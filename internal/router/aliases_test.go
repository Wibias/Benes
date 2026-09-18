package router

import (
	"errors"
	"strings"
	"testing"
)

func TestDisplaySelectorProjectsUnambiguousAliases(t *testing.T) {
	table := AliasTable{
		Providers:       map[string]struct{}{"google-antigravity": {}},
		ProviderByAlias: map[string]string{"ga": "google-antigravity"},
		ModelByProvider: map[string]map[string]string{"google-antigravity": {"gemini": "gemini-3.7-flash"}},
	}
	if got := table.DisplaySelector("google-antigravity/gemini-3.7-flash"); got != "ga/gemini" {
		t.Fatalf("both=%q", got)
	}
	if got := table.DisplaySelector("google-antigravity/other"); got != "ga/other" {
		t.Fatalf("provider-only=%q", got)
	}
	noProvider := AliasTable{
		Providers:       map[string]struct{}{"google-antigravity": {}},
		ModelByProvider: map[string]map[string]string{"google-antigravity": {"gemini": "gemini-3.7-flash"}},
	}
	if got := noProvider.DisplaySelector("google-antigravity/gemini-3.7-flash"); got != "google-antigravity/gemini" {
		t.Fatalf("model-only=%q", got)
	}
	if got := table.DisplaySelector("missing/id"); got != "missing/id" {
		t.Fatalf("missing=%q", got)
	}
}

func TestResolveQualifiedProviderAndModelAliases(t *testing.T) {
	table := AliasTable{
		Providers: map[string]struct{}{"google-antigravity": {}, "openrouter": {}},
		ProviderByAlias: map[string]string{
			"agy": "google-antigravity",
		},
		ModelByProvider: map[string]map[string]string{
			"google-antigravity": {"opus": "claude-opus-5"},
			"openrouter":         {"opus": "anthropic/claude-opus-5"},
		},
	}
	route, err := Resolve("agy/opus", nil, table)
	if err != nil {
		t.Fatal(err)
	}
	if route.Provider != "google-antigravity" || route.Model != "claude-opus-5" {
		t.Fatalf("route=%#v", route)
	}
	canonical, err := Resolve("openrouter/anthropic/claude-opus-5", nil, table)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.Provider != "openrouter" || canonical.Model != "anthropic/claude-opus-5" {
		t.Fatalf("canonical=%#v", canonical)
	}
}

func TestResolveBareModelAliasWhenUnique(t *testing.T) {
	table := AliasTable{
		Providers: map[string]struct{}{"google-antigravity": {}},
		ModelByProvider: map[string]map[string]string{
			"google-antigravity": {"opus": "claude-opus-5"},
		},
	}
	route, err := Resolve("opus", nil, table)
	if err != nil {
		t.Fatal(err)
	}
	if route.Provider != "google-antigravity" || route.Model != "claude-opus-5" {
		t.Fatalf("route=%#v", route)
	}
}

func TestResolveBareModelAliasAmbiguous(t *testing.T) {
	table := AliasTable{
		Providers: map[string]struct{}{"google-antigravity": {}, "openrouter": {}},
		ModelByProvider: map[string]map[string]string{
			"google-antigravity": {"opus": "claude-opus-5"},
			"openrouter":         {"opus": "anthropic/claude-opus-5"},
		},
	}
	_, err := Resolve("opus", nil, table)
	var ambiguous *AmbiguousAliasError
	if !errors.As(err, &ambiguous) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "google-antigravity/claude-opus-5") || !strings.Contains(err.Error(), "openrouter/anthropic/claude-opus-5") {
		t.Fatalf("candidates=%v", err)
	}
}

func TestResolveCanonicalProviderIdWinsOverAlias(t *testing.T) {
	table := AliasTable{
		Providers:       map[string]struct{}{"agy": {}, "google-antigravity": {}},
		ProviderByAlias: map[string]string{"agy": "google-antigravity"},
		ModelByProvider: map[string]map[string]string{
			"agy": {"m": "from-canonical"},
		},
	}
	route, err := Resolve("agy/m", nil, table)
	if err != nil {
		t.Fatal(err)
	}
	if route.Provider != "agy" || route.Model != "from-canonical" {
		t.Fatalf("route=%#v", route)
	}
}

func TestResolveKeepsCodexNamespaceBeforeProviderAlias(t *testing.T) {
	table := AliasTable{
		Providers:       map[string]struct{}{"google-antigravity": {}},
		ProviderByAlias: map[string]string{"side": "google-antigravity"},
		ModelByProvider: map[string]map[string]string{
			"openai": {"spark": "gpt-5.3-codex-spark"},
		},
	}
	route, err := Resolve("side/spark", map[string]string{"side": "acct-b"}, table)
	if err != nil {
		t.Fatal(err)
	}
	if route.Provider != "openai" || route.Model != "gpt-5.3-codex-spark" || route.CodexAccountID != "acct-b" {
		t.Fatalf("route=%#v", route)
	}
}
