package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunDoctorReportsConfigAndProxyWithoutBlocking(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"chat":{"adapter":"openai-chat","baseUrl":"https://api.openai.com/v1","apiKey":"sk-secret"}},"proxy":"http://proxy.example:3128"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runDoctor(&stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, configPath) || !strings.Contains(got, "proxy_mode\tfixed") {
		t.Fatalf("stdout=%q", got)
	}
	if !strings.Contains(got, "proxy\tnot-running") {
		t.Fatalf("missing proxy liveness: %q", got)
	}
	if !strings.Contains(got, "home\t") || !strings.Contains(got, "wsl\t") {
		t.Fatalf("missing home/wsl: %q", got)
	}
	if strings.Contains(got, "sk-secret") {
		t.Fatalf("doctor leaked api key: %q", got)
	}
	if !strings.Contains(got, "credential\tchat\tplaintext\ttrue") {
		t.Fatalf("stdout=%q", got)
	}

}
