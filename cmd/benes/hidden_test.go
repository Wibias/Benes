package main

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunHiddenRefreshAndUpdateWorker(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runHidden(context.Background(), "__refresh-version", nil, &stdout, &stderr, defaultCommandDependencies()); code != 0 {
		t.Fatalf("refresh code=%d stderr=%s", code, stderr.String())
	}
	if code := runHidden(context.Background(), "__gui-update-worker", nil, &stdout, &stderr, defaultCommandDependencies()); code != 0 || !strings.Contains(stdout.String(), "does not self-update") {
		t.Fatalf("update worker stdout=%q", stdout.String())
	}
}

func TestRunHiddenStartupHealthJSON(t *testing.T) {
	home := t.TempDir()
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: filepath.Join(home, "config.json")}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runHidden(context.Background(), "__startup-health", nil, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status"`) || !strings.Contains(stdout.String(), `"routingKind"`) {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunHiddenTrayHostNonWindows(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runHidden(context.Background(), "__tray-host", nil, &stdout, &stderr, defaultCommandDependencies())
	if runtime.GOOS == "windows" {
		if code == 0 {
			t.Fatal("windows host without entry should fail")
		}
		return
	}
	if code != 2 || !strings.Contains(stderr.String(), "Windows-only") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}
