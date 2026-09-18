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

func TestParseDevArgs(t *testing.T) {
	got, err := parseDevArgs([]string{"--port", "5173", "--dir", "./src", "--update", "--branch", "preview", "--proxy", "on", "--open"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 5173 || got.Dir != "./src" || !got.Update || got.Branch != "preview" || !got.ProxyOn || !got.Open || got.NoGUI {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseDevArgsEqualsFormAndDefaults(t *testing.T) {
	got, err := parseDevArgs([]string{"--port=8080", "--proxy=off"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 8080 || got.Dir != "." || got.Branch != "dev" || got.ProxyOn || got.Update {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseDevArgsDefaultsPort(t *testing.T) {
	got, err := parseDevArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != config.DefaultDevPort || got.Dir != "." || got.Branch != "dev" || got.ProxyOn {
		t.Fatalf("got=%+v", got)
	}
}

func TestParseDevArgsRejectsInvalidProxy(t *testing.T) {
	_, err := parseDevArgs([]string{"--port", "5173", "--proxy", "maybe"})
	if err == nil || !strings.Contains(err.Error(), "--proxy must be on or off") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseDevArgsNoGUIRequiresProxyOn(t *testing.T) {
	_, err := parseDevArgs([]string{"--port", "5173", "--no-gui"})
	if err == nil || !strings.Contains(err.Error(), "--no-gui requires --proxy on") {
		t.Fatalf("err=%v", err)
	}
	got, err := parseDevArgs([]string{"--port", "5173", "--no-gui", "--proxy", "on"})
	if err != nil || !got.NoGUI || !got.ProxyOn {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestRunDevUnknownFlagUsesUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"dev", "--port", "5173", "--wat"}, &stdout, &stderr, commandDependencies{})
	if code != 2 || !strings.Contains(stderr.String(), "unexpected argument") || !strings.Contains(stderr.String(), "usage: dev") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunDevRejectsMissingCheckout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWithDependencies(context.Background(), []string{"dev", "--port", "5173", "--dir", t.TempDir()}, &stdout, &stderr, commandDependencies{})
	if code != 1 || !strings.Contains(stderr.String(), "not a Benes checkout") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunDevProxyOffNeedsMainListener(t *testing.T) {
	dir := fakeBenesCheckout(t)
	var stdout, stderr bytes.Buffer
	deps := commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: filepath.Join(t.TempDir(), "missing-runtime-port.json")}, nil
		},
	}
	code := runWithDependencies(context.Background(), []string{"dev", "--port", "5173", "--dir", dir}, &stdout, &stderr, deps)
	if code != 1 || !strings.Contains(stderr.String(), "--proxy off needs a running main listener") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestValidateDevCheckoutRequiresBenesModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "benes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "gui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gui", "package.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateDevCheckout(dir); err == nil || !strings.Contains(err.Error(), "not the Benes module") {
		t.Fatalf("err=%v", err)
	}
}

func TestOverlayEnvReplacesExistingKeys(t *testing.T) {
	got := overlayEnv([]string{"PATH=/bin", "BENES_DEV_MODE=0", "HOME=/tmp"}, []string{"BENES_DEV_MODE=1"})
	joined := strings.Join(got, "\n")
	if strings.Count(joined, "BENES_DEV_MODE=") != 1 || !strings.Contains(joined, "BENES_DEV_MODE=1") {
		t.Fatalf("env=%q", got)
	}
	if !strings.Contains(joined, "PATH=/bin") || !strings.Contains(joined, "HOME=/tmp") {
		t.Fatalf("env=%q", got)
	}
}

func TestLoadServeRuntimeOverridesSkipInjectWithoutDevMode(t *testing.T) {
	env := map[string]string{
		"BENES_PID_PATH":          "/tmp/dev.pid",
		"BENES_RUNTIME_PORT_PATH": "/tmp/dev-port.json",
		"BENES_SKIP_CODEX_INJECT": "1",
	}
	got := loadServeRuntimeOverridesFrom(func(key string) string { return env[key] })
	if got.PIDPath != "" || got.RuntimePortPath != "" || !got.SkipCodexInject {
		t.Fatalf("got=%+v", got)
	}
}

func TestLoadServeRuntimeOverridesFromRequiresDevMode(t *testing.T) {
	env := map[string]string{
		"BENES_PID_PATH":          "/tmp/dev.pid",
		"BENES_RUNTIME_PORT_PATH": "/tmp/dev-port.json",
		"BENES_SKIP_CODEX_INJECT": "1",
	}
	getenv := func(key string) string { return env[key] }
	got := loadServeRuntimeOverridesFrom(getenv)
	if got.PIDPath != "" || got.RuntimePortPath != "" || !got.SkipCodexInject {
		t.Fatalf("got=%+v", got)
	}
	env["BENES_DEV_MODE"] = "1"
	got = loadServeRuntimeOverridesFrom(getenv)
	if got.PIDPath != "/tmp/dev.pid" || got.RuntimePortPath != "/tmp/dev-port.json" || !got.SkipCodexInject {
		t.Fatalf("got=%+v", got)
	}
}

func TestRunServeHonorsDevRuntimePaths(t *testing.T) {
	t.Setenv("BENES_DEV_MODE", "1")
	t.Setenv("BENES_PID_PATH", "/tmp/dev.pid")
	t.Setenv("BENES_RUNTIME_PORT_PATH", "/tmp/dev-port.json")
	t.Setenv("BENES_SKIP_CODEX_INJECT", "1")
	var got bootstrap.ServeOptions
	deps := commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Config: "config.json", PID: "/tmp/main.pid", RuntimePort: "/tmp/main-port.json"}, nil
		},
		loadDiskConfig: func(string, int64) (config.DiskConfig, error) {
			return config.DiskConfig{Raw: []byte(`{}`)}, nil
		},
		resolveEnvironmentToken: func(map[string]string) string { return "" },
		buildDataPlane: func(context.Context, config.DiskConfig, bootstrap.DataPlaneOptions) (bootstrap.DataPlane, error) {
			return bootstrap.DataPlane{Listener: config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100}}, nil
		},
		serveDataPlane: func(_ context.Context, _ bootstrap.DataPlane, options bootstrap.ServeOptions) error {
			got = options
			return nil
		},
	}
	var stderr bytes.Buffer
	if code := runWithDependencies(context.Background(), []string{"serve"}, &bytes.Buffer{}, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if got.PIDPath != "/tmp/dev.pid" || got.RuntimePortPath != "/tmp/dev-port.json" {
		t.Fatalf("options=%+v", got)
	}
	if got.AfterListen != nil {
		t.Fatal("expected Codex inject to be skipped")
	}
}

func TestRunServeIgnoresDevRuntimePathsWithoutDevMode(t *testing.T) {
	t.Setenv("BENES_PID_PATH", "/tmp/dev.pid")
	var got bootstrap.ServeOptions
	deps := commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Config: "config.json", PID: "/tmp/main.pid", RuntimePort: "/tmp/main-port.json"}, nil
		},
		loadDiskConfig: func(string, int64) (config.DiskConfig, error) {
			return config.DiskConfig{Raw: []byte(`{}`)}, nil
		},
		resolveEnvironmentToken: func(map[string]string) string { return "" },
		buildDataPlane: func(context.Context, config.DiskConfig, bootstrap.DataPlaneOptions) (bootstrap.DataPlane, error) {
			return bootstrap.DataPlane{Listener: config.ListenerConfig{Hostname: "127.0.0.1", Port: 23100}}, nil
		},
		serveDataPlane: func(_ context.Context, _ bootstrap.DataPlane, options bootstrap.ServeOptions) error {
			got = options
			return nil
		},
	}
	if code := runWithDependencies(context.Background(), []string{"serve", "--inject"}, &bytes.Buffer{}, &bytes.Buffer{}, deps); code != 0 {
		t.Fatalf("code=%d", code)
	}
	if got.PIDPath != "/tmp/main.pid" || got.RuntimePortPath != "/tmp/main-port.json" || got.AfterListen == nil {
		t.Fatalf("options=%+v afterListen=%v", got, got.AfterListen != nil)
	}
}

func fakeBenesCheckout(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "benes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "gui"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/Wibias/Benes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gui", "package.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
