package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/Wibias/Benes/internal/bootstrap"
	"github.com/Wibias/Benes/internal/config"
)

func TestRunRestartServesAfterMissingProxy(t *testing.T) {
	home := t.TempDir()
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home}, nil
	}
	served := false
	deps.serveDataPlane = func(context.Context, bootstrap.DataPlane, bootstrap.ServeOptions) error {
		served = true
		return nil
	}
	deps.buildDataPlane = func(context.Context, config.DiskConfig, bootstrap.DataPlaneOptions) (bootstrap.DataPlane, error) {
		return bootstrap.DataPlane{}, nil
	}
	deps.loadDiskConfig = func(string, int64) (config.DiskConfig, error) {
		return config.DiskConfig{}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runRestart(context.Background(), nil, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !served {
		t.Fatal("restart did not start the data plane")
	}
}

func TestRunRestartRejectsArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runRestart(context.Background(), []string{"now"}, &stdout, &stderr, defaultCommandDependencies()); code != 2 {
		t.Fatalf("code=%d", code)
	}
}
