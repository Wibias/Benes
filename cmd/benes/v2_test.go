package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/codexfeatures"
	"github.com/Wibias/Benes/internal/config"
)

func TestRunV2StatusReadsCodexAndBenesConfig(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("[features]\nmulti_agent_v2 = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"multiAgentMode":"v2","keepNativeChatGptOnV1":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runV2(nil, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "multi_agent_v2: ON") || !strings.Contains(out, "multi_agent_mode: v2") || !strings.Contains(out, "keep_native_chatgpt_on_v1: ON") {
		t.Fatalf("stdout=%q", out)
	}
}

func TestRunV2OnOffAndMode(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("[features]\nmulti_agent_v2 = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	called := []string{}
	orig := codexfeatures.Run
	codexfeatures.Run = func(action string) error {
		called = append(called, action)
		enabled := "false"
		if action == "enable" {
			enabled = "true"
		}
		return os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("[features]\nmulti_agent_v2 = "+enabled+"\n"), 0o600)
	}
	t.Cleanup(func() { codexfeatures.Run = orig })

	var stdout, stderr bytes.Buffer
	if code := runV2([]string{"on"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("on code=%d stderr=%s", code, stderr.String())
	}
	if len(called) != 1 || called[0] != "enable" {
		t.Fatalf("called=%v", called)
	}
	stdout.Reset()
	if code := runV2([]string{"mode", "v1"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("mode code=%d stderr=%s", code, stderr.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"multiAgentMode": "v1"`) && !strings.Contains(string(raw), `"multiAgentMode":"v1"`) {
		t.Fatalf("config=%s", raw)
	}
}
