package config

import (
	"encoding/json"
	"testing"
)

func TestProjectRouteAliases(t *testing.T) {
	table := ProjectRouteAliases(DiskConfig{Providers: map[string]json.RawMessage{
		"google-antigravity": providerJSON(t, `{
			"adapter":"google-antigravity",
			"alias":"agy",
			"modelAliases":{"claude-opus-5":"opus"}
		}`),
		"openrouter": providerJSON(t, `{
			"adapter":"openai-compatible",
			"alias":"or",
			"modelAliases":{"anthropic/claude-opus-5":"opus"}
		}`),
	}})
	if _, ok := table.Providers["google-antigravity"]; !ok {
		t.Fatalf("canonical provider missing: %#v", table.Providers)
	}
	if table.ProviderByAlias["agy"] != "google-antigravity" {
		t.Fatalf("agy=%q", table.ProviderByAlias["agy"])
	}
	if table.ProviderByAlias["or"] != "openrouter" {
		t.Fatalf("or=%q", table.ProviderByAlias["or"])
	}
	if table.ModelByProvider["google-antigravity"]["opus"] != "claude-opus-5" {
		t.Fatalf("agy opus=%#v", table.ModelByProvider["google-antigravity"])
	}
	if table.ModelByProvider["openrouter"]["opus"] != "anthropic/claude-opus-5" {
		t.Fatalf("or opus=%#v", table.ModelByProvider["openrouter"])
	}
}

func TestProjectProviderSpecsKeepsAliasFields(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"openrouter": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://openrouter.ai/api/v1",
			"apiKey":"k",
			"alias":"or",
			"modelAliases":{"anthropic/claude-opus-5":"opus"}
		}`),
	}})
	if len(projection.Specs) != 1 || len(projection.Skipped) != 0 {
		t.Fatalf("projection=%#v", projection)
	}
}

func TestProjectRouteAliasesSkipsBlankAndSlashTokens(t *testing.T) {
	table := ProjectRouteAliases(DiskConfig{Providers: map[string]json.RawMessage{
		"openrouter": providerJSON(t, `{
			"adapter":"openai-compatible",
			"alias":"or/x",
			"modelAliases":{"good":"ok","kept/id":"ok-slash-model","bad":"nope/x","":"empty"}
		}`),
	}})
	if _, ok := table.ProviderByAlias["or/x"]; ok {
		t.Fatalf("slash provider alias leaked: %#v", table.ProviderByAlias)
	}
	if table.ModelByProvider["openrouter"]["ok"] != "good" {
		t.Fatalf("valid model alias missing: %#v", table.ModelByProvider)
	}
	if table.ModelByProvider["openrouter"]["ok-slash-model"] != "kept/id" {
		t.Fatalf("canonical model slash dropped: %#v", table.ModelByProvider)
	}
	if _, ok := table.ModelByProvider["openrouter"]["nope/x"]; ok {
		t.Fatalf("slash model alias leaked")
	}
	if _, ok := table.ModelByProvider["openrouter"][""]; ok {
		t.Fatalf("empty alias leaked")
	}
}

func TestProjectRouteAliasesDropsCollidingProviderAliases(t *testing.T) {
	table := ProjectRouteAliases(DiskConfig{Providers: map[string]json.RawMessage{
		"left":       providerJSON(t, `{"adapter":"openai-chat","alias":"dup"}`),
		"right":      providerJSON(t, `{"adapter":"openai-chat","alias":"dup"}`),
		"openrouter": providerJSON(t, `{"adapter":"openai-chat","alias":"openrouter"}`),
	}})
	if _, ok := table.ProviderByAlias["dup"]; ok {
		t.Fatalf("colliding alias leaked: %#v", table.ProviderByAlias)
	}
	if _, ok := table.ProviderByAlias["openrouter"]; ok {
		t.Fatalf("canonical id alias leaked: %#v", table.ProviderByAlias)
	}
}

func TestProjectRouteAliasesSkipsDisabledProviderAliases(t *testing.T) {
	table := ProjectRouteAliases(DiskConfig{Providers: map[string]json.RawMessage{
		"google-antigravity": providerJSON(t, `{
			"adapter":"google-antigravity",
			"disabled":true,
			"alias":"agy",
			"modelAliases":{"claude-opus-5":"opus"}
		}`),
		"openrouter": providerJSON(t, `{
			"adapter":"openai-compatible",
			"modelAliases":{"anthropic/claude-opus-5":"opus"}
		}`),
	}})
	if _, ok := table.Providers["google-antigravity"]; !ok {
		t.Fatalf("disabled canonical id missing: %#v", table.Providers)
	}
	if _, ok := table.ProviderByAlias["agy"]; ok {
		t.Fatalf("disabled alias leaked")
	}
	if table.ModelByProvider["openrouter"]["opus"] != "anthropic/claude-opus-5" {
		t.Fatalf("enabled alias missing: %#v", table.ModelByProvider)
	}
	if _, ok := table.ModelByProvider["google-antigravity"]; ok {
		t.Fatalf("disabled model aliases leaked")
	}
}
