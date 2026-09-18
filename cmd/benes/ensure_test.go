package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/bootstrap"
	"github.com/Wibias/Benes/internal/config"
)

func TestRunEnsureReportsRunningProxyWithoutServing(t *testing.T) {
	home := t.TempDir()
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(portPath, []byte(`{"pid":7,"port":23100,"hostname":"127.0.0.1"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := withLiveRuntime(defaultCommandDependencies())
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, RuntimePort: portPath}, nil
	}
	served := false
	deps.serveDataPlane = func(context.Context, bootstrap.DataPlane, bootstrap.ServeOptions) error {
		served = true
		return nil
	}
	var stdout, stderr bytes.Buffer
	if code := runEnsure(context.Background(), nil, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if served {
		t.Fatal("ensure started the data plane while a proxy was already recorded")
	}
	if !strings.Contains(stdout.String(), "PID 7") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunEnsureRejectsArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runEnsure(context.Background(), []string{"--port"}, &stdout, &stderr, defaultCommandDependencies()); code != 2 {
		t.Fatalf("code=%d", code)
	}
}
