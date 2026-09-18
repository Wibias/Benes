package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/Wibias/Benes/internal/bootstrap"
	"github.com/Wibias/Benes/internal/config"
)

func TestRunServePassesResolvedBenesHomeToCodexPoolBootstrap(t *testing.T) {
	deps := commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: "/tmp/benes-home", Config: "/tmp/benes-home/config.json"}, nil
		},
		loadDiskConfig: func(string, int64) (config.DiskConfig, error) {
			return config.DiskConfig{Raw: json.RawMessage(`{}`), Providers: map[string]json.RawMessage{}}, nil
		},
		resolveEnvironmentToken: func(map[string]string) string { return "" },
		buildDataPlane: func(_ context.Context, _ config.DiskConfig, options bootstrap.DataPlaneOptions) (bootstrap.DataPlane, error) {
			if options.CodexPool.BenesHome != "/tmp/benes-home" {
				t.Fatalf("Benes home=%q", options.CodexPool.BenesHome)
			}
			return bootstrap.DataPlane{}, nil
		},
		serveDataPlane: func(context.Context, bootstrap.DataPlane, bootstrap.ServeOptions) error { return nil },
	}
	if code := runWithDependencies(context.Background(), []string{"serve"}, &bytes.Buffer{}, &bytes.Buffer{}, deps); code != 0 {
		t.Fatalf("code=%d", code)
	}
}
