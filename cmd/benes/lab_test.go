package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunLabRebuildAndStatus(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-responses","models":["gpt-5.4"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, Config: configPath}, nil
		},
		loadDiskConfig: config.LoadDiskConfig,
	}
	var stdout, stderr bytes.Buffer
	if code := runLab([]string{"rebuild"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("rebuild exit=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	if code := runLab([]string{"status"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("status exit=%d", code)
	}
	if !strings.Contains(stdout.String(), "verdicts") {
		t.Fatalf("status=%q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(home, "lab", "events.jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestRunLabHelpExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runLab([]string{"help"}, &stdout, &stderr, defaultCommandDependencies()); code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}
