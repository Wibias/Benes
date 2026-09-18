package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunAccountListPrintsMaskedKeyRows(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat","authMode":"key"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	prev := accessDo
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		if method != "GET" {
			t.Fatalf("method=%s", method)
		}
		if strings.Contains(u, "/api/providers/keys") {
			return 200, []byte(`{"activeId":"openai-apikey","keys":[{"id":"openai-apikey","masked":"stored","active":true}]}`), nil
		}
		if strings.Contains(u, "/api/codex-auth/accounts") {
			return 200, []byte(`{"accounts":[{"id":"__main__","email":"Codex App login","isMain":true}]}`), nil
		}
		if strings.Contains(u, "/api/codex-auth/active") {
			return 200, []byte(`{"activeCodexAccountId":"__main__","autoSwitchThreshold":80}`), nil
		}
		if strings.Contains(u, "/api/oauth/accounts") {
			return 200, []byte(`{"accounts":[]}`), nil
		}
		t.Fatalf("url=%s", u)
		return 0, nil, nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"list", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-") || strings.Contains(stdout.String(), "ya29") {
		t.Fatalf("secret leaked: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"openai-apikey"`) || !strings.Contains(stdout.String(), `"stored"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunAccountUnknownSubcommandIsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"not-a-verb"}, &stdout, &stderr, defaultCommandDependencies()); code != 2 || !strings.Contains(stderr.String(), "unknown account command") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestRunAccountAddKeyPostsStdinAndNeverPrintsSecret(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat","authMode":"key"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	prevDo := accessDo
	prevIn := accountKeyInput
	var posted []byte
	accessDo = func(method, u string, body []byte) (int, []byte, error) {
		if method != "POST" || !strings.Contains(u, "/api/providers/keys") {
			t.Fatalf("unexpected %s %s", method, u)
		}
		posted = append([]byte(nil), body...)
		return 201, []byte(`{"ok":true,"id":"openai-apikey"}`), nil
	}
	accountKeyInput = strings.NewReader("sk-secret-value\n")
	defer func() {
		accessDo = prevDo
		accountKeyInput = prevIn
	}()
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"add-key", "openai-apikey", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(string(posted), "sk-secret-value") {
		t.Fatalf("did not post key: %s", posted)
	}
	if strings.Contains(stdout.String(), "sk-secret-value") || strings.Contains(stderr.String(), "sk-secret-value") {
		t.Fatalf("secret leaked: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"openai-apikey"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunAccountAddKeyRequiresStdin(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"authMode":"key"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: filepath.Join(home, "runtime-port.json")}, nil
	}
	prevIn := accountKeyInput
	accountKeyInput = strings.NewReader("  \n")
	defer func() { accountKeyInput = prevIn }()
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"add-key", "openai-apikey"}, &stdout, &stderr, deps); code != 2 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestRunAccountRemoveRequiresYesAndDeletesKey(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"authMode":"key"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"remove", "openai-apikey", "openai-apikey"}, &stdout, &stderr, deps); code != 2 {
		t.Fatalf("missing --yes code=%d stderr=%s", code, stderr.String())
	}
	prevDo := accessDo
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		if method != "DELETE" || !strings.Contains(u, "/api/providers/keys") || !strings.Contains(u, "id=openai-apikey") {
			t.Fatalf("unexpected %s %s", method, u)
		}
		return 200, []byte(`{"ok":true}`), nil
	}
	defer func() { accessDo = prevDo }()
	stdout.Reset()
	stderr.Reset()
	if code := runAccount([]string{"remove", "openai-apikey", "openai-apikey", "--yes", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"ok":true`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunAccountUseAndAliasHitKeyPoolRoutes(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"authMode":"key"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	prev := accessDo
	accessDo = func(method, u string, body []byte) (int, []byte, error) {
		if method == "PUT" && strings.Contains(u, "/api/providers/keys/active") {
			if !strings.Contains(string(body), `"746b4ad1"`) {
				t.Fatalf("body=%s", body)
			}
			return 200, []byte(`{"ok":true,"activeId":"746b4ad1"}`), nil
		}
		if method == "PUT" && strings.Contains(u, "/api/providers/keys/alias") {
			if strings.Contains(string(body), "sk-") {
				t.Fatalf("secret in alias: %s", body)
			}
			return 200, []byte(`{"ok":true}`), nil
		}
		t.Fatalf("unexpected %s %s", method, u)
		return 0, nil, nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"use", "openai-apikey", "746b4ad1", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("use code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAccount([]string{"alias", "openai-apikey", "746b4ad1", "prod", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("alias code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-") {
		t.Fatalf("leaked: %s", stdout.String())
	}
}

func TestRunAccountUseAntigravityHitsOAuthActive(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	prev := accessDo
	accessDo = func(method, u string, body []byte) (int, []byte, error) {
		if method != "PUT" || !strings.Contains(u, "/api/oauth/accounts/active") {
			t.Fatalf("unexpected %s %s", method, u)
		}
		if strings.Contains(string(body), "secret") {
			t.Fatalf("secret: %s", body)
		}
		return 200, []byte(`{"ok":true,"activeAccountId":"b"}`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"use", "google-antigravity", "b", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"oauth"`) {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunAccountRemoveAntigravityHitsOAuthDelete(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	prev := accessDo
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		if method != "DELETE" || !strings.Contains(u, "/api/oauth/accounts") || !strings.Contains(u, "id=b") {
			t.Fatalf("unexpected %s %s", method, u)
		}
		return 200, []byte(`{"ok":true}`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"remove", "google-antigravity", "b", "--yes", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestRunAccountUseKiroHitsOAuthActive(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	prev := accessDo
	var saw string
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		saw = method + " " + u
		return 200, []byte(`{"ok":true}`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"use", "kiro", "acct"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(saw, "PUT") || !strings.Contains(saw, "/api/oauth/accounts/active") {
		t.Fatalf("saw=%s", saw)
	}
}

func TestRunAccountPriorityAndUseTalkToCodexAuthRoutes(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	prev := accessDo
	var saw []string
	accessDo = func(method, u string, body []byte) (int, []byte, error) {
		saw = append(saw, method+" "+u)
		switch {
		case strings.Contains(u, "/api/codex-auth/accounts/priority"):
			if method != "PUT" || !strings.Contains(string(body), `"id":"pool-1"`) {
				t.Fatalf("priority body=%s", body)
			}
			return 200, []byte(`{"ok":true,"id":"pool-1","priority":2}`), nil
		case strings.Contains(u, "/api/codex-auth/active") && method == "PUT":
			if !strings.Contains(string(body), `"accountId":"__main__"`) {
				t.Fatalf("use body=%s", body)
			}
			return 200, []byte(`{"ok":true,"activeCodexAccountId":"__main__"}`), nil
		case strings.Contains(u, "/api/codex-auth/accounts/clear-cooldown"):
			return 200, []byte(`{"ok":true,"id":"__main__","cleared":false}`), nil
		default:
			t.Fatalf("url=%s", u)
			return 0, nil, nil
		}
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runAccount([]string{"priority", "openai", "pool-1", "first", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("priority code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAccount([]string{"use", "openai", "main", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("use code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAccount([]string{"clear-cooldown", "openai", "main", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("clear code=%d stderr=%s", code, stderr.String())
	}
	joined := strings.Join(saw, "\n")
	if !strings.Contains(joined, "/api/codex-auth/accounts/priority") || !strings.Contains(joined, "/api/codex-auth/active") {
		t.Fatalf("saw=%v", saw)
	}
}
