package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

// grokDeps is initTestDeps plus a live listener record: both grok subcommands drive the
// running data plane, because the projection belongs to the process that owns the catalogue.
func grokDeps(t *testing.T) commandDependencies {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":23100,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	return deps
}

func stubGrokAccess(t *testing.T, wantMethod, wantPath string, status int, payload string) {
	t.Helper()
	prev := accessDo
	accessDo = func(method, u string, _ []byte) (int, []byte, error) {
		if method != wantMethod || !strings.HasSuffix(u, wantPath) {
			t.Fatalf("method=%s url=%s", method, u)
		}
		return status, []byte(payload), nil
	}
	t.Cleanup(func() { accessDo = prev })
}

func TestRunGrokStatusReportsTheProjection(t *testing.T) {
	deps := grokDeps(t)
	stubGrokAccess(t, "GET", "/api/grok", 200, `{"configPath":"/tmp/config.toml","present":true,"baseUrl":"http://127.0.0.1:23100/v1","catalogue":19,"registered":19,"current":true}`)
	var stdout, stderr bytes.Buffer
	if code := runGrok([]string{"status"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"Grok config: /tmp/config.toml", "Registered models: 19 of 19", "Configuration: up to date"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout=%q missing %q", stdout.String(), want)
		}
	}
}

func TestRunGrokStatusDistinguishesAbsentAndStale(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    string
	}{
		{"absent", `{"configPath":"/tmp/config.toml","present":false,"catalogue":19,"registered":0,"current":false}`, "no managed block written yet"},
		{"stale", `{"configPath":"/tmp/config.toml","present":true,"catalogue":20,"registered":19,"current":false}`, "does not match the Benes catalogue"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubGrokAccess(t, "GET", "/api/grok", 200, tc.payload)
			var stdout, stderr bytes.Buffer
			if code := runGrok([]string{"status"}, &stdout, &stderr, grokDeps(t)); code != 0 {
				t.Fatalf("code=%d stderr=%s", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("stdout=%q missing %q", stdout.String(), tc.want)
			}
		})
	}
}

func TestRunGrokStatusJSONPassesTheListenerSummaryThrough(t *testing.T) {
	stubGrokAccess(t, "GET", "/api/grok", 200, `{"configPath":"/tmp/config.toml","present":true,"catalogue":3,"registered":3,"current":true}`)
	var stdout, stderr bytes.Buffer
	if code := runGrok([]string{"status", "--json"}, &stdout, &stderr, grokDeps(t)); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"catalogue":3`) || !strings.Contains(stdout.String(), `"current":true`) {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunGrokApplyAsksTheListenerToWriteTheProjection(t *testing.T) {
	stubGrokAccess(t, "POST", "/api/grok/apply", 200, `{"ok":true,"changed":true,"message":"Added the benes managed block to Grok config."}`)
	var stdout, stderr bytes.Buffer
	if code := runGrok([]string{"apply"}, &stdout, &stderr, grokDeps(t)); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Added the benes managed block") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunGrokApplySurfacesTheNativeRefusal(t *testing.T) {
	stubGrokAccess(t, "POST", "/api/grok/apply", 200, `{"ok":false,"message":"Grok config has an orphaned benes marker; injection skipped."}`)
	var stdout, stderr bytes.Buffer
	if code := runGrok([]string{"apply"}, &stdout, &stderr, grokDeps(t)); code != 1 {
		t.Fatalf("code=%d stdout=%s", code, stdout.String())
	}
	if !strings.Contains(stderr.String(), "orphaned benes marker") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

// The per-Grok model list is gone: only a status read and an apply remain, and neither
// accepts a model argument.
func TestRunGrokRejectsTheRetiredSelectionActions(t *testing.T) {
	for _, action := range []string{"exclude", "include", "set", "clear"} {
		t.Run(action, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runGrok([]string{action, "gpt-4o"}, &stdout, &stderr, grokDeps(t)); code != 2 {
				t.Fatalf("code=%d stdout=%q", code, stdout.String())
			}
			if !strings.Contains(stderr.String(), "grok status") || !strings.Contains(stderr.String(), "grok apply") {
				t.Fatalf("usage missing: %q", stderr.String())
			}
		})
	}
}

func TestRunGrokUnknownAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runGrok([]string{"delete"}, &stdout, &stderr, grokDeps(t)); code != 2 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunIntegrationGrokDelegatesToStatus(t *testing.T) {
	stubGrokAccess(t, "GET", "/api/grok", 200, `{"configPath":"/tmp/config.toml","present":true,"catalogue":2,"registered":2,"current":true}`)
	var stdout, stderr bytes.Buffer
	if code := runIntegration([]string{"grok"}, &stdout, &stderr, grokDeps(t)); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Grok config:") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunIntegrationRejectsUnknownFamily(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runIntegration([]string{"other"}, &stdout, &stderr, defaultCommandDependencies()); code != 2 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

