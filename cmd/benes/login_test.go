package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/providers/antigravity"
	"github.com/Wibias/Benes/internal/providers/kiro"

	_ "modernc.org/sqlite"
)

func TestRunLoginPrintsAuthURLWithoutSecrets(t *testing.T) {
	home := t.TempDir()
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: filepath.Join(home, "config.json")}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runLogin(context.Background(), []string{"google-antigravity"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "accounts.google.com") || strings.Contains(stdout.String(), "verifier") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(home, "oauth-pending-google-antigravity.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRunLoginKiroImportsLocalSessionWithoutLeakingToken(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{}
	if runtime.GOOS == "windows" {
		env["LOCALAPPDATA"] = filepath.Join(home, "AppData", "Local")
		env["USERPROFILE"] = home
	}
	host := kiro.Host{Platform: runtime.GOOS, Home: home, Env: env}
	_, dbPath := kiro.NativeSessionEntry(host)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeKiroCLISession(t, dbPath, "aoa-secret-token", "arn:aws:codewhisperer:us-east-1:123456789012:profile/ok")
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: filepath.Join(home, "config.json")}, nil
	}
	deps.kiroHost = func() kiro.Host { return host }
	deps.kiroRunner = func(context.Context, []string) (kiro.CLIResult, error) {
		return kiro.CLIResult{ExitCode: 1}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runLogin(context.Background(), []string{"kiro"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "profile/ok") || strings.Contains(stdout.String(), "aoa-secret-token") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	raw, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "aoa-secret-token") || !strings.Contains(string(raw), "profile/ok") {
		t.Fatalf("auth.json missing stored kiro snapshot: %s", raw)
	}
}

func TestRunLoginRejectsUnknownProvider(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runLogin(context.Background(), []string{"not-a-provider"}, &stdout, &stderr, defaultCommandDependencies())
	if code != 2 || !strings.Contains(stderr.String(), "unsupported") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestRunLoginWritesKeyProviderWithoutPrintingSecret(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"port":23100}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runLogin(context.Background(), []string{"openai-apikey", "--api-key", "sk-secret-login"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-secret-login") || strings.Contains(stderr.String(), "sk-secret-login") {
		t.Fatalf("leaked key stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, `"openai-apikey"`) || !strings.Contains(text, "sk-secret-login") {
		t.Fatalf("config=%s", text)
	}
	if !strings.Contains(text, `"defaultProvider": "openai-apikey"`) {
		t.Fatalf("defaultProvider missing: %s", text)
	}
}

func TestRunLoginPreservesExistingModelCosts(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-responses","apiKey":"old","modelCosts":{"gpt-5.5":{"input":1}}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runLogin(context.Background(), []string{"openai-apikey", "--api-key", "sk-new"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	body, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "sk-new") || !strings.Contains(text, `"modelCosts"`) || strings.Contains(text, "sk-new") && strings.Contains(stdout.String(), "sk-new") {
		t.Fatalf("stdout=%s config=%s", stdout.String(), text)
	}
}

func TestRunLoginRequiresAPIKeyForKeyProviders(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runLogin(context.Background(), []string{"openai-apikey"}, &stdout, &stderr, defaultCommandDependencies())
	if code != 2 || !strings.Contains(stderr.String(), "--api-key") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestAppendedPendingFileRoundTrip(t *testing.T) {
	home := t.TempDir()
	pending := antigravity.PendingFile{Path: filepath.Join(home, "pending.json")}
	login, err := antigravity.NewPendingLogin()
	if err != nil {
		t.Fatal(err)
	}
	if err := pending.Save(login); err != nil {
		t.Fatal(err)
	}
	got, err := pending.Load()
	if err != nil || got.State != login.State {
		t.Fatalf("load=%#v err=%v", got, err)
	}
}

func writeKiroCLISession(t *testing.T, path, access, profile string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE auth_kv (key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	value := `{"access_token":"` + access + `","refresh_token":"rt","profile_arn":"` + profile + `"}`
	if _, err := db.Exec(`INSERT INTO auth_kv (key, value) VALUES (?, ?)`, "kirocli:social:token", value); err != nil {
		t.Fatal(err)
	}
}
