package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRunAliasSetListRemove(t *testing.T) {
	deps := comboTestDeps(t, `{
		"providers":{
			"google-antigravity":{"adapter":"google-antigravity","baseUrl":"https://example"},
			"openrouter":{"adapter":"openai-chat","baseUrl":"https://openrouter.ai/api/v1","apiKey":"k"}
		}
	}`)
	var stdout, stderr bytes.Buffer
	if code := runAlias([]string{"set", "google-antigravity", "agy"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("provider set code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAlias([]string{"set", "openrouter/anthropic/claude-opus-5", "opus"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("model set code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runAlias([]string{"list"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("list code=%d stderr=%s", code, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "google-antigravity  agy") || !strings.Contains(got, "openrouter/anthropic/claude-opus-5  opus") {
		t.Fatalf("list stdout=%q", got)
	}
	raw, err := os.ReadFile(mustAliasConfigPath(t, deps))
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Providers map[string]struct {
			Alias        string            `json:"alias"`
			ModelAliases map[string]string `json:"modelAliases"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	if root.Providers["google-antigravity"].Alias != "agy" {
		t.Fatalf("disk alias=%#v", root.Providers["google-antigravity"])
	}
	if root.Providers["openrouter"].ModelAliases["anthropic/claude-opus-5"] != "opus" {
		t.Fatalf("disk model aliases=%#v", root.Providers["openrouter"].ModelAliases)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runAlias([]string{"remove", "google-antigravity"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("remove provider code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runAlias([]string{"remove", "openrouter/anthropic/claude-opus-5"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("remove model code=%d stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	if code := runAlias([]string{"list"}, &stdout, &stderr, deps); code != 0 || !strings.Contains(stdout.String(), "No aliases configured.") {
		t.Fatalf("after remove stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunAliasSetRejectsCollisionsAndSlashTokens(t *testing.T) {
	deps := comboTestDeps(t, `{
		"providers":{
			"google-antigravity":{"adapter":"google-antigravity"},
			"openrouter":{"adapter":"openai-chat","alias":"or"}
		}
	}`)
	var stdout, stderr bytes.Buffer
	if code := runAlias([]string{"set", "google-antigravity", "or"}, &stdout, &stderr, deps); code != 1 || !strings.Contains(stderr.String(), "already used") {
		t.Fatalf("collision code=%d stderr=%q", code, stderr.String())
	}
	stderr.Reset()
	if code := runAlias([]string{"set", "google-antigravity", "openrouter"}, &stdout, &stderr, deps); code != 1 || !strings.Contains(stderr.String(), "collides with provider id") {
		t.Fatalf("canonical collision code=%d stderr=%q", code, stderr.String())
	}
	stderr.Reset()
	if code := runAlias([]string{"set", "google-antigravity", "agy/x"}, &stdout, &stderr, deps); code != 2 || !strings.Contains(stderr.String(), "single token") {
		t.Fatalf("slash token code=%d stderr=%q", code, stderr.String())
	}
	stderr.Reset()
	if code := runAlias([]string{"set", "missing", "agy"}, &stdout, &stderr, deps); code != 1 || !strings.Contains(stderr.String(), "unknown provider") {
		t.Fatalf("missing provider code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunAliasSetUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runAlias([]string{"wat"}, &stdout, &stderr, defaultCommandDependencies()); code != 2 || !strings.Contains(stderr.String(), "alias list|set|remove") {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
}

func mustAliasConfigPath(t *testing.T, deps commandDependencies) string {
	t.Helper()
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return paths.Config
}
