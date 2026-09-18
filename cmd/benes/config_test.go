package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunConfigSetContextWindowUsesTransactionStore(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"local":{"adapter":"openai-responses"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runConfig([]string{"set-context-window", "local", "gpt-5.6", "200000"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	var windows map[string]map[string]struct {
		Tokens int `json:"tokens"`
	}
	if err := json.Unmarshal(root["modelContextWindows"], &windows); err != nil {
		t.Fatal(err)
	}
	if windows["local"]["gpt-5.6"].Tokens != 200000 {
		t.Fatalf("config=%s", raw)
	}
}

func TestRunConfigShowRedactsSecrets(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"local":{"apiKey":"sk-secret","adapter":"openai-responses"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runConfig([]string{"show"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-secret") {
		t.Fatalf("secret leaked: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "********") {
		t.Fatalf("missing redaction: %s", stdout.String())
	}
}

func TestRunConfigShowDropsSecretShapedModelCostsKeys(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	body := `{"providers":{"blsc":{"adapter":"openai-chat","modelCosts":{"deepseek-v4-flash":{"input":0.14,"output":0.28,"cacheRead":0.0028,"cacheWrite":0},"sk-abcdef1234567890":{"input":1,"output":2,"cacheRead":0.1,"cacheWrite":0}}}}}`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runConfig([]string{"show"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-abcdef1234567890") {
		t.Fatalf("secret-shaped modelCosts key leaked: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "deepseek-v4-flash") {
		t.Fatalf("real model cost missing: %s", stdout.String())
	}
}

func TestRunConfigGetReturnsRedactedPath(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"port":18080,"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	code := runConfig([]string{"get", "port"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "18080") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRunConfigSetAndUnset(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{},"port":18080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runConfig([]string{"set", "port", "19000"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("set code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runConfig([]string{"get", "port"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("get code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "19000") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runConfig([]string{"unset", "port"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("unset code=%d stderr=%s", code, stderr.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"port"`) {
		t.Fatalf("port still present: %s", raw)
	}
}

func TestRunConfigMutationsListsNewestFirst(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{},"port":18080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runConfig([]string{"set", "port", "19000"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("set code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runConfig([]string{"mutations"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("mutations code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "sk-") {
		t.Fatalf("secret leaked: %s", stdout.String())
	}
	var records []config.MutationRecord
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatalf("decode=%v body=%s", err, stdout.String())
	}
	if len(records) != 1 || records[0].Source.Class != config.SourceCLI || records[0].Changes[0].Path != "/port" {
		t.Fatalf("records=%#v", records)
	}
}

func TestRunConfigValidateAndExport(t *testing.T) {
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
	if code := runConfig([]string{"validate"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("validate code=%d stderr=%s", code, stderr.String())
	}
	out := filepath.Join(home, "export.json")
	stdout.Reset()
	if code := runConfig([]string{"export", out}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("export code=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("export missing: %v", err)
	}
}

func TestRunConfigImportRequiresYes(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	from := filepath.Join(home, "incoming.json")
	if err := os.WriteFile(from, []byte(`{"providers":{},"port":23100}`), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runConfig([]string{"import", from}, &stdout, &stderr, deps); code != 2 {
		t.Fatalf("missing --yes code=%d stderr=%s", code, stderr.String())
	}
	if code := runConfig([]string{"import", from, "--yes"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("import code=%d stderr=%s", code, stderr.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "23100") {
		t.Fatalf("import missing port: %s", raw)
	}
}
