package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunSystemSettingsUpdatesAutoStart(t *testing.T) {
	deps := initTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runSystem([]string{"settings", "--auto-start", "off"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "updated") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"apiKey"`) {
		t.Fatalf("secret leaked: %s", raw)
	}
	var cfg struct {
		CodexAutoStart bool `json:"codexAutoStart"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.CodexAutoStart {
		t.Fatalf("auto-start still on: %s", raw)
	}
}

func TestRunSystemStatusJSONWithoutProxy(t *testing.T) {
	deps := initTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runSystem([]string{"status", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var body struct {
		Running  bool `json:"running"`
		Settings struct {
			StreamMode string `json:"streamMode"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Running {
		t.Fatalf("expected no proxy: %s", stdout.String())
	}
	if body.Settings.StreamMode != "auto" {
		t.Fatalf("stdout=%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "apiKey") {
		t.Fatalf("secret leaked: %s", stdout.String())
	}
}

func TestRunSystemUpdateDoesNotSelfUpdate(t *testing.T) {
	deps := initTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runSystem([]string{"update"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "does not self-update") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunSystemRejectsUnknown(t *testing.T) {
	deps := initTestDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runSystem([]string{"reboot"}, &stdout, &stderr, deps); code != 2 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}
