package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunClaudeConfigSetPUTsWithoutPrintingSecrets(t *testing.T) {
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
		if method != "PUT" || !strings.Contains(u, "/api/claude-code") {
			t.Fatalf("method=%s url=%s", method, u)
		}
		if !strings.Contains(string(body), `"enabled":false`) {
			t.Fatalf("body=%s", body)
		}
		return 200, []byte(`{"ok":true}`), nil
	}
	defer func() { accessDo = prev }()
	var stdout, stderr bytes.Buffer
	if code := runClaude([]string{"config", "set", "--enabled", "off"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "updated") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunClaudeLaunchWiresEnvWithoutPrintingSecrets(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"apiKeys":[{"key":"client-secret"}],"claudeCode":{"enabled":true,"authMode":"proxy","model":"gpt-5.5"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude"))
	t.Setenv("ANTHROPIC_API_KEY", "client-secret")
	prevDo := accessDo
	t.Cleanup(func() { accessDo = prevDo })
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		if strings.Contains(u, "/api/claude-code") {
			return 200, []byte(`{"enabled":true,"authMode":"proxy","model":"gpt-5.5","injectAgents":true}`), nil
		}
		if strings.Contains(u, "/v1/models") {
			return 200, []byte(`{"data":[{"id":"claude-benes-native--gpt-5.5","display_name":"GPT"}]}`), nil
		}
		t.Fatalf("url=%s", u)
		return 0, nil, nil
	}
	prevLaunch := launchClaude
	t.Cleanup(func() { launchClaude = prevLaunch })
	var gotEnv []string
	launchClaude = func(args []string, env []string) error {
		gotEnv = append([]string{}, env...)
		return nil
	}
	var stdout, stderr bytes.Buffer
	code := runClaude([]string{"--help"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
		},
		spawnStart: func() error {
			t.Fatal("should not start")
			return nil
		},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "client-secret") || strings.Contains(stderr.String(), "client-secret") {
		t.Fatalf("secret leaked stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	joined := strings.Join(gotEnv, "\n")
	if !strings.Contains(joined, "ANTHROPIC_BASE_URL=http://127.0.0.1:18080") {
		t.Fatalf("env=%s", joined)
	}
	if strings.Contains(joined, "ANTHROPIC_API_KEY=client-secret") {
		t.Fatal("admission key forwarded as API key")
	}
	if !strings.Contains(joined, "ANTHROPIC_AUTH_TOKEN=client-secret") && !strings.Contains(joined, "ANTHROPIC_AUTH_TOKEN=benes-proxy") {
		t.Fatalf("token missing: %s", joined)
	}
	cache := filepath.Join(home, "claude", "cache", "gateway-models.json")
	raw, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "client-secret") {
		t.Fatal("secret in cache")
	}
}

func TestRunClaudeDisabledDoesNotLaunch(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"claudeCode":{"enabled":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	prevLaunch := launchClaude
	t.Cleanup(func() { launchClaude = prevLaunch })
	launchClaude = func([]string, []string) error {
		t.Fatal("launched")
		return nil
	}
	var stdout, stderr bytes.Buffer
	code := runClaude(nil, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, Config: configPath}, nil
		},
	})
	if code != 1 || !strings.Contains(stderr.String(), "disabled") {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}
