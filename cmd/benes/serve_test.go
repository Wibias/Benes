package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/bootstrap"
	"github.com/Wibias/Benes/internal/config"
)

func TestRunServeComposesConfigEnvironmentBuildAndServe(t *testing.T) {
	ctx := context.Background()
	disk := config.DiskConfig{Raw: []byte(`{}`), Providers: map[string]json.RawMessage{}, Source: config.DiskConfigSourceFile}
	plane := bootstrap.DataPlane{Listener: config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100}}
	var calls []string
	deps := commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			calls = append(calls, "paths")
			return config.Paths{Config: "/tmp/benes/config.json"}, nil
		},
		loadDiskConfig: func(path string, maxBytes int64) (config.DiskConfig, error) {
			calls = append(calls, "load")
			if path != "/tmp/benes/config.json" || maxBytes != 0 {
				t.Fatalf("load path=%q max=%d", path, maxBytes)
			}
			return disk, nil
		},
		resolveEnvironmentToken: func(map[string]string) string {
			calls = append(calls, "env")
			return "runtime-token"
		},
		buildDataPlane: func(gotCtx context.Context, got config.DiskConfig, options bootstrap.DataPlaneOptions) (bootstrap.DataPlane, error) {
			calls = append(calls, "build")
			if gotCtx != ctx || !reflect.DeepEqual(got, disk) {
				t.Fatalf("build ctx/disk mismatch")
			}
			if !reflect.DeepEqual(options.RuntimeDataPlaneTokens, []string{"runtime-token"}) {
				t.Fatalf("runtime tokens=%#v", options.RuntimeDataPlaneTokens)
			}
			return plane, nil
		},
		serveDataPlane: func(gotCtx context.Context, got bootstrap.DataPlane, options bootstrap.ServeOptions) error {
			calls = append(calls, "serve")
			if gotCtx != ctx || !reflect.DeepEqual(got, plane) {
				t.Fatalf("serve ctx/plane mismatch")
			}
			if options.AfterListen != nil {
				t.Fatal("expected Codex inject to stay off unless --inject")
			}
			if !reflect.DeepEqual(options, bootstrap.ServeOptions{}) {
				t.Fatalf("serve options=%#v", options)
			}
			return nil
		},
	}

	var stdout, stderr bytes.Buffer
	if code := runWithDependencies(ctx, []string{"serve"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if !reflect.DeepEqual(calls, []string{"paths", "load", "env", "build", "serve"}) {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestRunServeDoesNotInventEmptyRuntimeToken(t *testing.T) {
	var got []string
	deps := commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) { return config.Paths{Config: "config.json"}, nil },
		loadDiskConfig: func(string, int64) (config.DiskConfig, error) {
			return config.DiskConfig{Raw: []byte(`{}`), Providers: map[string]json.RawMessage{}}, nil
		},
		resolveEnvironmentToken: func(map[string]string) string { return "" },
		buildDataPlane: func(_ context.Context, _ config.DiskConfig, options bootstrap.DataPlaneOptions) (bootstrap.DataPlane, error) {
			got = options.RuntimeDataPlaneTokens
			return bootstrap.DataPlane{}, nil
		},
		serveDataPlane: func(context.Context, bootstrap.DataPlane, bootstrap.ServeOptions) error { return nil },
	}
	if code := runWithDependencies(context.Background(), []string{"serve"}, &bytes.Buffer{}, &bytes.Buffer{}, deps); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if got != nil {
		t.Fatalf("runtime tokens=%#v", got)
	}
}

func TestRunServeRejectsArgumentsBeforeStartup(t *testing.T) {
	called := false
	deps := commandDependencies{resolvePaths: func(config.PathOptions) (config.Paths, error) { called = true; return config.Paths{}, nil }}
	var stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"serve", "extra"}, &bytes.Buffer{}, &stderr, deps)
	if code != 2 || called {
		t.Fatalf("code=%d called=%v stderr=%q", code, called, stderr.String())
	}
	if !strings.Contains(stderr.String(), "does not accept arguments") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunServeHonorsPortFlag(t *testing.T) {
	var served bootstrap.DataPlane
	deps := commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) { return config.Paths{Config: "config.json"}, nil },
		loadDiskConfig: func(string, int64) (config.DiskConfig, error) {
			return config.DiskConfig{Raw: []byte(`{}`), Providers: map[string]json.RawMessage{}}, nil
		},
		resolveEnvironmentToken: func(map[string]string) string { return "" },
		buildDataPlane: func(context.Context, config.DiskConfig, bootstrap.DataPlaneOptions) (bootstrap.DataPlane, error) {
			return bootstrap.DataPlane{Listener: config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100}}, nil
		},
		serveDataPlane: func(_ context.Context, plane bootstrap.DataPlane, _ bootstrap.ServeOptions) error {
			served = plane
			return nil
		},
	}
	var stderr bytes.Buffer
	if code := runWithDependencies(context.Background(), []string{"serve", "--port", "18080"}, &bytes.Buffer{}, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if served.Listener.Port != 18080 {
		t.Fatalf("port=%d", served.Listener.Port)
	}
}

func TestRunServeRejectsInvalidPortFlag(t *testing.T) {
	called := false
	deps := commandDependencies{resolvePaths: func(config.PathOptions) (config.Paths, error) { called = true; return config.Paths{}, nil }}
	for _, args := range [][]string{{"serve", "--port"}, {"serve", "--port", "0"}, {"serve", "--port", "foo"}, {"serve", "--port", "18080", "extra"}} {
		called = false
		var stderr bytes.Buffer
		code := runWithDependencies(context.Background(), args, &bytes.Buffer{}, &stderr, deps)
		if code != 2 || called {
			t.Fatalf("args=%v code=%d called=%v stderr=%q", args, code, called, stderr.String())
		}
	}
}

func TestRunServeStopsAtEachStartupFailure(t *testing.T) {
	sentinel := errors.New("sentinel startup failure")
	tests := []struct {
		name string
		deps commandDependencies
	}{
		{"paths", commandDependencies{resolvePaths: func(config.PathOptions) (config.Paths, error) { return config.Paths{}, sentinel }}},
		{"load", commandDependencies{
			resolvePaths:   func(config.PathOptions) (config.Paths, error) { return config.Paths{Config: "config.json"}, nil },
			loadDiskConfig: func(string, int64) (config.DiskConfig, error) { return config.DiskConfig{}, sentinel },
		}},
		{"build", commandDependencies{
			resolvePaths:            func(config.PathOptions) (config.Paths, error) { return config.Paths{Config: "config.json"}, nil },
			loadDiskConfig:          func(string, int64) (config.DiskConfig, error) { return config.DiskConfig{}, nil },
			resolveEnvironmentToken: func(map[string]string) string { return "" },
			buildDataPlane: func(context.Context, config.DiskConfig, bootstrap.DataPlaneOptions) (bootstrap.DataPlane, error) {
				return bootstrap.DataPlane{}, sentinel
			},
		}},
		{"serve", commandDependencies{
			resolvePaths:            func(config.PathOptions) (config.Paths, error) { return config.Paths{Config: "config.json"}, nil },
			loadDiskConfig:          func(string, int64) (config.DiskConfig, error) { return config.DiskConfig{}, nil },
			resolveEnvironmentToken: func(map[string]string) string { return "" },
			buildDataPlane: func(context.Context, config.DiskConfig, bootstrap.DataPlaneOptions) (bootstrap.DataPlane, error) {
				return bootstrap.DataPlane{}, nil
			},
			serveDataPlane: func(context.Context, bootstrap.DataPlane, bootstrap.ServeOptions) error { return sentinel },
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			code := runWithDependencies(context.Background(), []string{"serve"}, &bytes.Buffer{}, &stderr, tt.deps)
			if code != 1 {
				t.Fatalf("code=%d stderr=%q", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), "benes: serve:") || !strings.Contains(stderr.String(), sentinel.Error()) {
				t.Fatalf("stderr=%q", stderr.String())
			}
		})
	}
}

func TestRunServeCancelledContextHasNoStartupSideEffects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	deps := commandDependencies{resolvePaths: func(config.PathOptions) (config.Paths, error) { called = true; return config.Paths{}, nil }}
	var stderr bytes.Buffer
	code := runWithDependencies(ctx, []string{"serve"}, &bytes.Buffer{}, &stderr, deps)
	if code != 1 || called {
		t.Fatalf("code=%d called=%v stderr=%q", code, called, stderr.String())
	}
	if !strings.Contains(stderr.String(), context.Canceled.Error()) {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunServeSkipsCodexInjectByDefault(t *testing.T) {
	var got bootstrap.ServeOptions
	if code := runWithDependencies(context.Background(), []string{"serve"}, &bytes.Buffer{}, &bytes.Buffer{}, serveInjectDeps(&got)); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if got.AfterListen != nil {
		t.Fatal("expected Codex inject to be skipped")
	}
}

func TestRunServeInjectsCodexWhenInjectFlag(t *testing.T) {
	var got bootstrap.ServeOptions
	var stderr bytes.Buffer
	if code := runWithDependencies(context.Background(), []string{"serve", "--inject"}, &bytes.Buffer{}, &stderr, serveInjectDeps(&got)); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if got.AfterListen == nil {
		t.Fatal("missing AfterListen inject hook")
	}
}

func TestRunServeHonorsNoInjectFlagWithPort(t *testing.T) {
	var got bootstrap.ServeOptions
	var served bootstrap.DataPlane
	deps := serveInjectDeps(&got)
	origServe := deps.serveDataPlane
	deps.serveDataPlane = func(ctx context.Context, plane bootstrap.DataPlane, options bootstrap.ServeOptions) error {
		served = plane
		return origServe(ctx, plane, options)
	}
	var stderr bytes.Buffer
	if code := runWithDependencies(context.Background(), []string{"serve", "--port", "18080", "--no-inject"}, &bytes.Buffer{}, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if served.Listener.Port != 18080 {
		t.Fatalf("port=%d", served.Listener.Port)
	}
	if got.AfterListen != nil {
		t.Fatal("expected Codex inject to be skipped")
	}
}

func TestRunServeSkipEnvWinsOverInjectFlagWithoutDevMode(t *testing.T) {
	t.Setenv("BENES_SKIP_CODEX_INJECT", "1")
	var got bootstrap.ServeOptions
	if code := runWithDependencies(context.Background(), []string{"serve", "--inject"}, &bytes.Buffer{}, &bytes.Buffer{}, serveInjectDeps(&got)); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if got.AfterListen != nil {
		t.Fatal("expected Codex inject to stay skipped")
	}
}

func TestRunServeRejectsInjectAndNoInjectTogether(t *testing.T) {
	called := false
	deps := commandDependencies{resolvePaths: func(config.PathOptions) (config.Paths, error) { called = true; return config.Paths{}, nil }}
	var stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"serve", "--inject", "--no-inject"}, &bytes.Buffer{}, &stderr, deps)
	if code != 2 || called {
		t.Fatalf("code=%d called=%v stderr=%q", code, called, stderr.String())
	}
	if !strings.Contains(stderr.String(), "cannot combine --inject and --no-inject") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func serveInjectDeps(got *bootstrap.ServeOptions) commandDependencies {
	return commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) { return config.Paths{Config: "config.json"}, nil },
		loadDiskConfig: func(string, int64) (config.DiskConfig, error) {
			return config.DiskConfig{Raw: []byte(`{}`), Providers: map[string]json.RawMessage{}}, nil
		},
		resolveEnvironmentToken: func(map[string]string) string { return "" },
		buildDataPlane: func(context.Context, config.DiskConfig, bootstrap.DataPlaneOptions) (bootstrap.DataPlane, error) {
			return bootstrap.DataPlane{Listener: config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100}}, nil
		},
		serveDataPlane: func(_ context.Context, _ bootstrap.DataPlane, options bootstrap.ServeOptions) error {
			*got = options
			return nil
		},
	}
}
