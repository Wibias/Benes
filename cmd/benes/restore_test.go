package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunRestoreStripsManagedRouting(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("model_provider = \"benes\"\n[model_providers.benes]\nbase_url = \"http://127.0.0.1:23100/v1\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	var stdout, stderr bytes.Buffer
	if code := runRestore(nil, &stdout, &stderr, commandDependencies{}); code != 0 {

		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Removed benes routing") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	raw, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	if strings.Contains(string(raw), "benes") {
		t.Fatalf("config=%s", raw)
	}
}

func TestRunRestoreJSONReportsStrip(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("model_provider = \"benes\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	var stdout, stderr bytes.Buffer
	if code := runRestore([]string{"--json"}, &stdout, &stderr, commandDependencies{}); code != 0 {

		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"success":true`) || !strings.Contains(stdout.String(), `"stripped":true`) {
		t.Fatalf("stdout=%q", stdout.String())
	}

}

func TestRunRestoreBackInjectsLiveProxyURL(t *testing.T) {
	codexHome := t.TempDir()
	benesHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(benesHome, "runtime-port.json")
	if err := os.WriteFile(runtime, []byte(`{"pid":7,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runRestore([]string{"back"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	raw, _ := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if !strings.Contains(string(raw), "http://127.0.0.1:18080/v1") {
		t.Fatalf("config=%s stdout=%s", raw, stdout.String())
	}
}
