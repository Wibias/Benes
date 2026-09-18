package kiro

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestNativeSessionPathUsesPlatformStore(t *testing.T) {
	win, path := NativeSessionEntry(Host{
		Platform: "windows",
		Home:     `C:\Users\dev`,
		Env:      map[string]string{"LOCALAPPDATA": `D:\Local`},
	})
	if win != "kiro-cli-windows-data" || path != `D:\Local\Kiro-Cli\data.sqlite3` {
		t.Fatalf("windows=%s %s", win, path)
	}
	mac, path := NativeSessionEntry(Host{Platform: "darwin", Home: "/Users/dev"})
	if mac != "kiro-cli-data" || path != "/Users/dev/Library/Application Support/kiro-cli/data.sqlite3" {
		t.Fatalf("darwin=%s %s", mac, path)
	}
	lin, path := NativeSessionEntry(Host{Platform: "linux", Home: "/home/dev"})
	if lin != "kiro-cli-linux-data" || path != "/home/dev/.local/share/kiro-cli/data.sqlite3" {
		t.Fatalf("linux=%s %s", lin, path)
	}
}

func TestNativeWindowsSessionIgnoresPOSIXHome(t *testing.T) {
	_, path := NativeSessionEntry(Host{
		Platform: "windows",
		Home:     "/home/from-git-bash",
		Env:      map[string]string{"USERPROFILE": `C:\Users\real`, "LOCALAPPDATA": ""},
	})
	if path != `C:\Users\real\AppData\Local\Kiro-Cli\data.sqlite3` {
		t.Fatalf("path=%s", path)
	}
}

func TestResolveCLIExecutableSkipsDirectoriesAndPrefersPATH(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(filepath.Join(bin, "kiro-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "real", "kiro-cli")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := ResolveCLIExecutable(Host{
		Platform: runtime.GOOS,
		Home:     root,
		Env:      map[string]string{"PATH": bin + string(os.PathListSeparator) + filepath.Dir(real)},
	})
	if got != real {
		t.Fatalf("got=%s", got)
	}
}

func TestImportLocalSessionReadsJSONBeforeSQLite(t *testing.T) {
	root := t.TempDir()
	jsonPath := filepath.Join(root, "creds.json")
	writeJSON(t, jsonPath, map[string]any{
		"accessToken":  "aoa-json",
		"refreshToken": "rt-json",
		"profileArn":   "arn:aws:codewhisperer:us-east-1:123456789012:profile/json",
		"apiRegion":    "us-east-1",
	})
	dbPath := filepath.Join(root, "data.sqlite3")
	seedAuthDB(t, dbPath, [][2]string{{
		"kirocli:social:token",
		mustJSON(map[string]any{"access_token": "aoa-sqlite", "refresh_token": "rt-sqlite"}),
	}}, nil)
	got, err := ImportLocalSession(Host{
		Platform: runtime.GOOS,
		Home:     root,
		Env: map[string]string{
			"KIRO_CREDS_FILE": jsonPath,
			"KIROCLI_DB_PATH": dbPath,
		},
	})
	if err != nil || got.Source != "json" || got.Credential.AccessToken != "aoa-json" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestImportLocalSessionReadsPreferredSQLiteToken(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "data.sqlite3")
	seedAuthDB(t, dbPath, [][2]string{
		{"kirocli:social:token", mustJSON(map[string]any{"access_token": "aoa-social", "refresh_token": "rt-social", "expires_at": "2099-01-01T00:00:00Z"})},
		{"kirocli:odic:token", mustJSON(map[string]any{"access_token": "aoa-oidc", "refresh_token": "rt-oidc"})},
	}, map[string]string{
		"api.codewhisperer.profile": mustJSON(map[string]any{"arn": "arn:aws:codewhisperer:eu-west-1:123456789012:profile/abc"}),
	})
	got, err := ImportLocalSession(Host{
		Platform: runtime.GOOS,
		Home:     root,
		Env:      map[string]string{"KIROCLI_DB_PATH": dbPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "sqlite" || got.Credential.AccessToken != "aoa-oidc" || got.Credential.ProfileARN == "" || got.Credential.APIRegion != "eu-west-1" {
		t.Fatalf("got=%#v", got)
	}
	if got.Credential.ExpiresUnix == time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("preferred row must not inherit another token's expiry")
	}
}

func TestImportLocalSessionTokenKeyOverridesPreferredRow(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "sel.sqlite3")
	seedAuthDB(t, dbPath, [][2]string{
		{"kirocli:social:token", mustJSON(map[string]any{"access_token": "aoa-social", "refresh_token": "rt-social"})},
		{"kirocli:odic:token", mustJSON(map[string]any{"access_token": "aoa-oidc", "refresh_token": "rt-oidc"})},
	}, nil)
	got, err := ImportLocalSession(Host{
		Platform: runtime.GOOS,
		Home:     root,
		Env: map[string]string{
			"KIROCLI_DB_PATH":   dbPath,
			"KIROCLI_TOKEN_KEY": "kirocli:social:token",
		},
	})
	if err != nil || got.Credential.AccessToken != "aoa-social" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if _, err := ImportLocalSession(Host{
		Platform: runtime.GOOS,
		Home:     root,
		Env: map[string]string{
			"KIROCLI_DB_PATH":   dbPath,
			"KIROCLI_TOKEN_KEY": "missing:token",
		},
	}); !errors.Is(err, ErrTokenKeyMissing) {
		t.Fatalf("missing key=%v", err)
	}
}

func TestImportLocalSessionRejectsAmbiguousTokensWithoutLeakingSecrets(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "amb.sqlite3")
	seedAuthDB(t, dbPath, [][2]string{
		{"custom:a:token", mustJSON(map[string]any{"access_token": "aoa-secret-a", "refresh_token": "rt-a"})},
		{"custom:b:token", mustJSON(map[string]any{"access_token": "aoa-secret-b", "refresh_token": "rt-b"})},
	}, nil)
	_, err := ImportLocalSession(Host{
		Platform: runtime.GOOS,
		Home:     root,
		Env:      map[string]string{"KIROCLI_DB_PATH": dbPath},
	})
	if !errors.Is(err, ErrTokenAmbiguous) {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), "aoa-secret") {
		t.Fatalf("leaked=%v", err)
	}
}

func TestImportLocalSessionMissingOverrideDoesNotCreatePath(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "missing", "credentials.sqlite3")
	native := filepath.Join(root, ".local", "share", "kiro-cli", "data.sqlite3")
	seedAuthDB(t, native, [][2]string{
		{"kirocli:social:token", mustJSON(map[string]any{"access_token": "aoa-default-must-not-win"})},
	}, nil)
	_, err := ImportLocalSession(Host{
		Platform: runtime.GOOS,
		Home:     root,
		Env:      map[string]string{"KIROCLI_DB_PATH": missing},
	})
	if !errors.Is(err, ErrNoSession) {
		t.Fatalf("err=%v", err)
	}
	if _, statErr := os.Stat(missing); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("created override path")
	}
}

func TestImportLocalSessionRejectsMidFlightSwitch(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "live.sqlite3")
	seedAuthDB(t, dbPath, [][2]string{
		{"kirocli:social:token", mustJSON(map[string]any{
			"access_token":  "aoa-one",
			"refresh_token": "rt-one",
			"profile_arn":   "arn:aws:codewhisperer:us-east-1:123456789012:profile/one",
		})},
	}, nil)
	reads := 0
	h := Host{
		Platform: runtime.GOOS,
		Home:     root,
		Env:      map[string]string{"KIROCLI_DB_PATH": dbPath},
		AfterRead: func() {
			reads++
			if reads == 1 {
				seedAuthDB(t, dbPath, [][2]string{
					{"kirocli:social:token", mustJSON(map[string]any{
						"access_token":  "aoa-two",
						"refresh_token": "rt-two",
						"profile_arn":   "arn:aws:codewhisperer:us-west-2:123456789012:profile/two",
					})},
				}, nil)
			}
		},
	}
	if _, err := ImportLocalSessionStable(h); !errors.Is(err, ErrSplitSnapshot) {
		t.Fatalf("switch=%v", err)
	}
}

func TestImportLocalSessionPromotesStableReadToSnapshot(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "ok.sqlite3")
	seedAuthDB(t, dbPath, [][2]string{
		{"kirocli:social:token", mustJSON(map[string]any{
			"access_token":  "aoa-ok",
			"refresh_token": "rt-ok",
			"profile_arn":   "arn:aws:codewhisperer:us-east-1:123456789012:profile/ok",
		})},
	}, nil)
	got, err := ImportLocalSessionStable(Host{
		Platform: runtime.GOOS,
		Home:     root,
		Env:      map[string]string{"KIROCLI_DB_PATH": dbPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := ImportSnapshot(got.Credential)
	if err != nil || snap.AccessToken != "aoa-ok" || snap.EffectiveRegion() != "us-east-1" {
		t.Fatalf("snap=%#v err=%v", snap, err)
	}
}

func TestResolveCLIExecutableFallsBackToInstallLayout(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "Kiro-Cli", "kiro-cli.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("mz"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ResolveCLIExecutable(Host{
		Platform: runtime.GOOS,
		Home:     root,
		Env:      map[string]string{"LOCALAPPDATA": root, "PATH": filepath.Join(root, "empty")},
	})
	if runtime.GOOS == "windows" {
		if got != exe {
			t.Fatalf("got=%s want=%s", got, exe)
		}
		return
	}
	if got != cliUnixName && !strings.HasSuffix(got, cliUnixName) {
		t.Fatalf("got=%s", got)
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func seedAuthDB(t *testing.T, path string, tokens [][2]string, state map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE auth_kv (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range tokens {
		if _, err := db.Exec(`INSERT INTO auth_kv (key, value) VALUES (?, ?)`, row[0], row[1]); err != nil {
			t.Fatal(err)
		}
	}
	if len(state) > 0 {
		if _, err := db.Exec(`CREATE TABLE state (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
			t.Fatal(err)
		}
		for key, value := range state {
			if _, err := db.Exec(`INSERT INTO state (key, value) VALUES (?, ?)`, key, value); err != nil {
				t.Fatal(err)
			}
		}
	}
}
