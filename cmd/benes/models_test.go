package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunModelsListsCatalogProjectedIDs(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{
		"providers":{
			"openai-apikey":{
				"adapter":"openai-responses",
				"baseUrl":"https://api.openai.com/v1",
				"apiKey":"k",
				"defaultModel":"gpt-5.6"
			}
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
	code := runModels(nil, &stdout, &stderr, deps)

	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "openai-apikey/gpt-5.6") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunModelsListJSON(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{"providers":{"openai-apikey":{"adapter":"openai-responses","baseUrl":"https://api.openai.com/v1","apiKey":"k","defaultModel":"gpt-5.6"}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runModels([]string{"list", "--json"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"openai-apikey/gpt-5.6"`) {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunModelsRejectsUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runModels([]string{"foo"}, &stdout, &stderr, defaultCommandDependencies()); code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunModelsAddAndRemoveCustom(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{"providers":{"deepseek":{"adapter":"openai","baseUrl":"https://api.deepseek.com","apiKey":"k","defaultModel":"deepseek-chat"}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runModels([]string{"add", "deepseek", "deepseek-v4", "--display-name", "V4"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("add code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "deepseek/deepseek-v4") {
		t.Fatalf("add stdout=%q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runModels([]string{"list-custom"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "deepseek/deepseek-v4") {
		t.Fatalf("list-custom code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runModels([]string{"remove", "deepseek/deepseek-v4", "--yes"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("remove code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runModels([]string{"list-custom"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "no custom models") {
		t.Fatalf("after remove stdout=%q", stdout.String())
	}
}

func TestRunModelsAddUnknownProviderFailsClosed(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runModels([]string{"add", "missing", "m"}, &stdout, &stderr, deps); code == 0 || !strings.Contains(stderr.String(), "not configured") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunModelsDisableEnableAndSelected(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{"providers":{"deepseek":{"adapter":"openai","baseUrl":"https://api.deepseek.com","apiKey":"k","defaultModel":"deepseek-chat","models":["deepseek-chat","deepseek-v4"]}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runModels([]string{"disable", "deepseek/deepseek-v4"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("disable code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runModels([]string{"enable", "deepseek/deepseek-v4"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("enable code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runModels([]string{"selected", "deepseek", "--set", "deepseek-chat,deepseek-v4"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("selected code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runModels([]string{"selected", "deepseek"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "deepseek-chat") {
		t.Fatalf("selected list stdout=%q", stdout.String())
	}
}

func TestRunModelsEditContextAndShadow(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{
		"providers":{"deepseek":{"adapter":"openai","baseUrl":"https://api.deepseek.com","apiKey":"k","defaultModel":"deepseek-chat"}},
		"customModels":[{"id":"abc","provider":"deepseek","modelId":"old","addedAt":"2026-01-01T00:00:00Z"}]
	}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runModels([]string{"edit", "abc", "--model-id", "deepseek-v4"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("edit code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runModels([]string{"context", "provider", "deepseek", "on", "--value", "128000"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("context code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runModels([]string{"shadow", "set", "gpt-5.5", "--enabled", "on"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("shadow code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runModels([]string{"shadow"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "gpt-5.5") {
		t.Fatalf("shadow status stdout=%q", stdout.String())
	}
}
func TestRunModelsProbePostsExplicitCatalogProbe(t *testing.T) {
	prev := accessDo
	var gotURL string
	var gotBody []byte
	accessDo = func(method, url string, body []byte) (int, []byte, error) {
		if method != "POST" {
			t.Fatalf("method=%s", method)
		}
		gotURL = url
		gotBody = append([]byte(nil), body...)
		return 200, []byte(`{"provider":"lab","model":"m","state":"available","kind":"catalog","generationIncurred":false,"timing":{"totalMs":12}}`), nil
	}
	t.Cleanup(func() { accessDo = prev })
	home := t.TempDir()
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":9,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, RuntimePort: portPath}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runModels([]string{"probe", "lab", "m"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("code=%d err=%s", code, stderr.String())
	}
	if !strings.Contains(gotURL, "/api/models/probe") {
		t.Fatalf("url=%s", gotURL)
	}
	if strings.Contains(string(gotBody), `"generate":true`) {
		t.Fatalf("must not default to generation: %s", gotBody)
	}
	if !strings.Contains(stdout.String(), "available") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}
