package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunRoutePolicyListAndShow(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{
		"routingProfiles":{
			"fast":{"candidates":[{"provider":"openai","model":"gpt-4o"}]}
		}
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runRoute([]string{"policy", "list"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "fast") {
		t.Fatalf("list code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := runRoute([]string{"policy", "show", "fast"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "openai/gpt-4o") {
		t.Fatalf("show stdout=%q", stdout.String())
	}
	stdout.Reset()
	if code := runRoute([]string{"policy", "dry-run", "fast"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "openai/gpt-4o") {
		t.Fatalf("dry-run stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

}

func TestRunAccessEndpointsFromConfig(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"hostname":"127.0.0.1","port":18080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runAccess([]string{"endpoints"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), ":18080") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
