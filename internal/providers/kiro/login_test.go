package kiro

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func loginHost(t *testing.T) (Host, string) {
	t.Helper()
	root := t.TempDir()
	env := map[string]string{}
	if runtime.GOOS == "windows" {
		env["LOCALAPPDATA"] = filepath.Join(root, "AppData", "Local")
		env["USERPROFILE"] = root
	}
	h := Host{Platform: runtime.GOOS, Home: root, Env: env}
	_, path := NativeSessionEntry(h)
	return h, path
}

func TestForceLoginRestoresPriorSessionOnFailure(t *testing.T) {
	h, path := loginHost(t)
	seedAuthDB(t, path, [][2]string{{
		"kirocli:social:token",
		mustJSON(map[string]any{
			"access_token":  "aoa-prior",
			"refresh_token": "rt-prior",
			"profile_arn":   "arn:aws:codewhisperer:us-east-1:123456789012:profile/prior",
		}),
	}}, nil)
	_, err := ForceLogin(context.Background(), h, func(ctx context.Context, args []string) (CLIResult, error) {
		if args[0] == "login" {
			return CLIResult{ExitCode: 1}, nil
		}
		return CLIResult{ExitCode: 0}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "did not complete successfully") {
		t.Fatalf("err=%v", err)
	}
	got, err := ImportLocalSession(h)
	if err != nil || got.Credential.AccessToken != "aoa-prior" {
		t.Fatalf("restored=%#v err=%v", got, err)
	}
	if _, statErr := os.Stat(path + recoverySuffix); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("recovery leftover")
	}
}

func TestForceLoginImportsFreshSessionAndKeepsRecoveryUntilSettle(t *testing.T) {
	h, path := loginHost(t)
	seedAuthDB(t, path, [][2]string{{
		"kirocli:social:token",
		mustJSON(map[string]any{
			"access_token":  "aoa-prior",
			"refresh_token": "rt-prior",
			"profile_arn":   "arn:aws:codewhisperer:us-east-1:123456789012:profile/prior",
		}),
	}}, nil)
	pending, err := ForceLogin(context.Background(), h, func(ctx context.Context, args []string) (CLIResult, error) {
		if args[0] == "login" {
			seedAuthDB(t, path, [][2]string{{
				"kirocli:social:token",
				mustJSON(map[string]any{
					"access_token":  "aoa-new",
					"refresh_token": "rt-new",
					"profile_arn":   "arn:aws:codewhisperer:us-west-2:123456789012:profile/new",
				}),
			}}, nil)
		}
		return CLIResult{ExitCode: 0}, nil
	})
	if err != nil || pending.Import.Credential.AccessToken != "aoa-new" {
		t.Fatalf("pending=%#v err=%v", pending, err)
	}
	if _, statErr := os.Stat(path + recoverySuffix); statErr != nil {
		t.Fatalf("recovery missing: %v", statErr)
	}
	Settle(pending, false)
	got, err := ImportLocalSession(h)
	if err != nil || got.Credential.AccessToken != "aoa-prior" {
		t.Fatalf("rolled back=%#v err=%v", got, err)
	}
}

func TestForceLoginBlocksUnreadableSession(t *testing.T) {
	h, path := loginHost(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not-sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ForceLogin(context.Background(), h, func(context.Context, []string) (CLIResult, error) {
		t.Fatal("must not mutate")
		return CLIResult{}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "could not be backed up") {
		t.Fatalf("err=%v", err)
	}
}

func TestForceLoginBlocksEnvOverride(t *testing.T) {
	h, path := loginHost(t)
	seedAuthDB(t, path, [][2]string{{
		"kirocli:social:token",
		mustJSON(map[string]any{"access_token": "aoa-native", "refresh_token": "rt-native", "profile_arn": "arn:aws:codewhisperer:us-east-1:123456789012:profile/n"}),
	}}, nil)
	h.Env["KIROCLI_DB_PATH"] = filepath.Join(t.TempDir(), "other.sqlite3")
	_, err := ForceLogin(context.Background(), h, func(context.Context, []string) (CLIResult, error) {
		t.Fatal("must not mutate")
		return CLIResult{}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "could not be backed up") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoginImportsStableLocalSession(t *testing.T) {
	h, path := loginHost(t)
	seedAuthDB(t, path, [][2]string{{
		"kirocli:social:token",
		mustJSON(map[string]any{
			"access_token":  "aoa-ok",
			"refresh_token": "rt-ok",
			"profile_arn":   "arn:aws:codewhisperer:us-east-1:123456789012:profile/ok",
		}),
	}}, nil)
	pending, err := Login(context.Background(), h, nil)
	if err != nil || pending.Import.Credential.AccessToken != "aoa-ok" {
		t.Fatalf("got=%#v err=%v", pending, err)
	}
}

func TestLoginFailsClosedWithoutProfile(t *testing.T) {
	h, path := loginHost(t)
	seedAuthDB(t, path, [][2]string{{
		"kirocli:social:token",
		mustJSON(map[string]any{"access_token": "aoa-ok", "refresh_token": "rt-ok"}),
	}}, nil)
	if _, err := Login(context.Background(), h, nil); !errors.Is(err, ErrMissingProfile) {
		t.Fatalf("err=%v", err)
	}
}

func TestLoginImportsBuilderIDWithoutProfile(t *testing.T) {
	h, path := loginHost(t)
	seedAuthDB(t, path, [][2]string{
		{
			"kirocli:odic:token",
			mustJSON(map[string]any{"access_token": "aoa-builder", "refresh_token": "rt-builder", "region": "us-east-1"}),
		},
		{
			"kirocli:odic:device-registration",
			mustJSON(map[string]any{"clientId": "builder-client", "clientSecret": "builder-secret"}),
		},
	}, nil)
	pending, err := Login(context.Background(), h, nil)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	cred := pending.Import.Credential
	if cred.AccessToken != "aoa-builder" || cred.AuthType != "aws_sso_oidc" || cred.ProfileARN != "" {
		t.Fatalf("cred=%#v", cred)
	}
}

func TestForceLoginCancelDoesNotLeaveSwitchedSession(t *testing.T) {
	h, path := loginHost(t)
	seedAuthDB(t, path, [][2]string{{
		"kirocli:social:token",
		mustJSON(map[string]any{
			"access_token":  "aoa-prior",
			"refresh_token": "rt-prior",
			"profile_arn":   "arn:aws:codewhisperer:us-east-1:123456789012:profile/prior",
		}),
	}}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := ForceLogin(ctx, h, func(ctx context.Context, args []string) (CLIResult, error) {
		if args[0] == "login" {
			cancel()
			return CLIResult{}, ctx.Err()
		}
		return CLIResult{ExitCode: 0}, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	got, err := ImportLocalSession(h)
	if err != nil || got.Credential.AccessToken != "aoa-prior" {
		t.Fatalf("restored=%#v err=%v", got, err)
	}
}

func TestInstallGuidanceIsPlatformSpecific(t *testing.T) {
	if !strings.Contains(InstallGuidance("windows"), "install.ps1") {
		t.Fatal("windows")
	}
	if !strings.Contains(InstallGuidance("linux"), "cli.kiro.dev/install") {
		t.Fatal("unix")
	}
}
