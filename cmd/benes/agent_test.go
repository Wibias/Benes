package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func agentTestDeps(t *testing.T) commandDependencies {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai":{"adapter":"openai","baseUrl":"https://api.openai.com/v1","apiKey":"k"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	return deps
}

func TestRunAgentInjectionAndSubagents(t *testing.T) {
	deps := agentTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runAgent([]string{"injection", "set", "--model", "openai/gpt-4o", "--guidance", "on"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("injection code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runAgent([]string{"subagents", "set", "openai/gpt-4o,openai/gpt-4.1"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("subagents code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runAgent([]string{"status"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "openai/gpt-4o") {
		t.Fatalf("status stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAgent([]string{"subagents", "set", "a,b,c,d,e,f"}, &stdout, &stderr, deps); code == 0 || !strings.Contains(stderr.String(), "at most 5") {
		t.Fatalf("cap code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunAgentSidecarAndProviderAccountMode(t *testing.T) {
	deps := agentTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runAgent([]string{"sidecar", "web", "--backend", "anthropic", "--model", "gpt-5.4-mini"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("sidecar code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runProvider([]string{"account-mode", "pool"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("account-mode code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "pool") {
		t.Fatalf("account-mode stdout=%q", stdout.String())
	}
}
