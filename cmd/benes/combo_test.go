package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func comboTestDeps(t *testing.T, body string) commandDependencies {
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
	return deps
}

func TestRunComboSetListRemove(t *testing.T) {
	deps := comboTestDeps(t, `{"providers":{"openai":{"adapter":"openai","baseUrl":"https://api.openai.com/v1","apiKey":"k"}}}`)
	var stdout, stderr bytes.Buffer
	if code := runCombo([]string{"set", "flash", "--targets", "openai/gpt-4,openai/gpt-4o", "--strategy", "failover"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("set code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runCombo([]string{"list"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "flash") {
		t.Fatalf("list stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := runRoute([]string{"combo", "show", "flash"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "openai/gpt-4") {
		t.Fatalf("show stdout=%q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runCombo([]string{"remove", "flash", "--yes"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("remove code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runCombo([]string{"list"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "No combos configured.") {
		t.Fatalf("after remove stdout=%q", stdout.String())
	}
}

func TestRunComboSetRejectsRoundRobin(t *testing.T) {
	deps := comboTestDeps(t, `{"providers":{"openai":{"adapter":"openai","baseUrl":"https://api.openai.com/v1","apiKey":"k"}}}`)
	var stdout, stderr bytes.Buffer
	if code := runCombo([]string{"set", "flash", "--targets", "openai/gpt-4", "--strategy", "round-robin"}, &stdout, &stderr, deps); code != 2 || !strings.Contains(stderr.String(), "failover") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunComboSetRequiresTargets(t *testing.T) {
	deps := comboTestDeps(t, `{"providers":{}}`)
	var stdout, stderr bytes.Buffer
	if code := runCombo([]string{"set", "x"}, &stdout, &stderr, deps); code != 2 || !strings.Contains(stderr.String(), "--targets") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunComboNativeAliasAndImageInput(t *testing.T) {
	deps := comboTestDeps(t, `{
		"providers":{"Nova1":{"adapter":"openai","baseUrl":"https://example","apiKey":"k"}},
		"combos":{"text-only":{"imageInput":"disabled","targets":[{"provider":"ark","model":"old-model"}]}}
	}`)
	var stdout, stderr bytes.Buffer
	if code := runCombo([]string{"set", "nova-sol", "--targets", "Nova1/codex/gpt-5.6-sol", "--alias", "gpt-5.6-sol", "--native-alias", "--display-name", "Nova1 - codex-gpt-5.6-sol"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("native-alias code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runCombo([]string{"set", "text-only", "--targets", "ark/new-model"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("image-input code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runCombo([]string{"show", "text-only", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("show code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"imageInput":"disabled"`) && !strings.Contains(stdout.String(), `"imageInput": "disabled"`) {
		t.Fatalf("imageInput not preserved: %q", stdout.String())
	}
}

func TestRunComboSetOmitsDefaultStickyLimit(t *testing.T) {
	deps := comboTestDeps(t, `{"providers":{"openai":{"adapter":"openai","baseUrl":"https://api.openai.com/v1","apiKey":"k"}}}`)
	var stdout, stderr bytes.Buffer
	if code := runCombo([]string{"set", "flash", "--targets", "openai/gpt-4", "--strategy", "failover"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("set code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runCombo([]string{"show", "flash", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("show code=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if strings.Contains(out, `"stickyLimit"`) {
		t.Fatalf("unexpected stickyLimit pollution: %s", out)
	}
}

func TestRunComboSetPreservesExplicitStickyLimit(t *testing.T) {
	deps := comboTestDeps(t, `{
		"providers":{"openai":{"adapter":"openai","baseUrl":"https://api.openai.com/v1","apiKey":"k"}},
		"combos":{"sticky":{"strategy":"failover","stickyLimit":7,"targets":[{"provider":"openai","model":"gpt-4"}]}}
	}`)
	var stdout, stderr bytes.Buffer
	if code := runCombo([]string{"set", "sticky", "--targets", "openai/gpt-4o"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("set code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runCombo([]string{"show", "sticky", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("show code=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"stickyLimit":7`) && !strings.Contains(out, `"stickyLimit": 7`) {
		t.Fatalf("stickyLimit not preserved: %s", out)
	}
}

func TestRunComboSetExplicitStickyLimit(t *testing.T) {
	deps := comboTestDeps(t, `{"providers":{"openai":{"adapter":"openai","baseUrl":"https://api.openai.com/v1","apiKey":"k"}}}`)
	var stdout, stderr bytes.Buffer
	if code := runCombo([]string{"set", "flash", "--targets", "openai/gpt-4", "--sticky", "3"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("set code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runCombo([]string{"show", "flash", "--json"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("show code=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, `"stickyLimit":3`) && !strings.Contains(out, `"stickyLimit": 3`) {
		t.Fatalf("explicit stickyLimit missing: %s", out)
	}
}

