package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func providerTestDeps(t *testing.T, body string) (commandDependencies, string) {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	return deps, configPath
}

func TestRunProviderListAndShowMasksKey(t *testing.T) {
	deps, _ := providerTestDeps(t, `{
		"defaultProvider":"openai",
		"providers":{"openai":{"adapter":"openai","baseUrl":"https://api.openai.com/v1","apiKey":"sk-secret-key-value","defaultModel":"gpt-4"}}
	}`)
	var stdout, stderr bytes.Buffer
	if code := runProvider([]string{"list"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "openai (default)") {
		t.Fatalf("list code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-secret") {
		t.Fatalf("list leaked key: %q", stdout.String())
	}
	stdout.Reset()
	if code := runProvider([]string{"show", "openai"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("show code=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-secret-key-value") || !strings.Contains(stdout.String(), "****") {
		t.Fatalf("show leaked key: %q", stdout.String())
	}
}

func TestRunProviderAddRemoveAndSetDefault(t *testing.T) {
	deps, _ := providerTestDeps(t, `{
		"defaultProvider":"openai",
		"providers":{"openai":{"adapter":"openai","baseUrl":"https://api.openai.com/v1","apiKey":"k"}}
	}`)
	var stdout, stderr bytes.Buffer
	if code := runProvider([]string{"add", "deepseek", "--adapter", "openai", "--base-url", "https://api.deepseek.com", "--api-key", "ds-key"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("add code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runProvider([]string{"set-default", "deepseek"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("set-default code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runProvider([]string{"remove", "openai"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("remove code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runProvider([]string{"list"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "deepseek (default)") || strings.Contains(stdout.String(), "  openai") {
		t.Fatalf("after remove stdout=%q", stdout.String())
	}
}

func TestRunProviderRemoveDefaultFailsClosed(t *testing.T) {
	deps, _ := providerTestDeps(t, `{
		"defaultProvider":"openai",
		"providers":{
			"openai":{"adapter":"openai","baseUrl":"https://api.openai.com/v1","apiKey":"k"},
			"other":{"adapter":"openai","baseUrl":"https://example","apiKey":"k"}
		}
	}`)
	var stdout, stderr bytes.Buffer
	if code := runProvider([]string{"remove", "openai"}, &stdout, &stderr, deps); code == 0 || !strings.Contains(stderr.String(), "default provider") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunProviderAddRequiresAdapterAndBaseURL(t *testing.T) {
	deps, _ := providerTestDeps(t, `{"providers":{}}`)
	var stdout, stderr bytes.Buffer
	if code := runProvider([]string{"add", "custom"}, &stdout, &stderr, deps); code != 2 || !strings.Contains(stderr.String(), "--adapter") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunProviderEditUpdatesAdapter(t *testing.T) {
	deps, _ := providerTestDeps(t, `{
		"defaultProvider":"openai",
		"providers":{"openai":{"adapter":"openai","baseUrl":"https://api.openai.com/v1","apiKey":"sk-secret-key-value"}}
	}`)
	var stdout, stderr bytes.Buffer
	if code := runProvider([]string{"edit", "openai", "--adapter", "openai-responses", "--note", "primary"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("edit code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runProvider([]string{"show", "openai"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("show code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-secret-key-value") {
		t.Fatalf("show leaked key: %q", stdout.String())
	}
}

func TestRunProviderTestRequiresLiveProxy(t *testing.T) {
	deps, _ := providerTestDeps(t, `{"providers":{"openai":{"adapter":"openai","baseUrl":"https://api.openai.com/v1"}}}`)
	var stdout, stderr bytes.Buffer
	if code := runProvider([]string{"test", "openai"}, &stdout, &stderr, deps); code == 0 || !strings.Contains(stderr.String(), "not running") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunProviderPresetsRequiresLiveProxy(t *testing.T) {
	deps, _ := providerTestDeps(t, `{"providers":{}}`)
	var stdout, stderr bytes.Buffer
	if code := runProvider([]string{"presets"}, &stdout, &stderr, deps); code == 0 || !strings.Contains(stderr.String(), "not running") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunProviderQuotaRequiresLiveProxy(t *testing.T) {
	deps, _ := providerTestDeps(t, `{"providers":{}}`)
	var stdout, stderr bytes.Buffer
	if code := runProvider([]string{"quota"}, &stdout, &stderr, deps); code == 0 || !strings.Contains(stderr.String(), "not running") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunProviderQuotaPrintsReportsWithoutSecrets(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":23100,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
	}
	prev := accessDo
	accessDo = func(method, url string, body []byte) (int, []byte, error) {
		if !strings.Contains(url, "/api/provider-quotas?refresh=1") {
			t.Fatalf("url %s", url)
		}
		return 200, []byte(`{"generatedAt":1,"reports":[{"provider":"openrouter","source":"openrouter:key-info","quota":{"customWindows":[{"label":"API credits ($7.50 of $10.00 remaining)","percent":25}]}}]}`), nil
	}
	t.Cleanup(func() { accessDo = prev })
	var stdout, stderr bytes.Buffer
	if code := runProvider([]string{"quota", "--refresh"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "openrouter") || strings.Contains(stdout.String(), "sk-") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}
