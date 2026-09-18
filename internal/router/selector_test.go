package router

import "testing"

func TestParseExplicitSelector(t *testing.T) {
	for _, tc := range []struct {
		selector string
		provider string
		model    string
	}{
		{selector: "openai-apikey/gpt-5.6", provider: "openai-apikey", model: "gpt-5.6"},
		{selector: "openrouter/anthropic/claude-sonnet-4", provider: "openrouter", model: "anthropic/claude-sonnet-4"},
		{selector: "gateway/org/team/model", provider: "gateway", model: "org/team/model"},
	} {
		route, err := ParseExplicit(tc.selector)
		if err != nil {
			t.Fatalf("ParseExplicit(%q): %v", tc.selector, err)
		}
		if route.Provider != tc.provider || route.Model != tc.model {
			t.Fatalf("ParseExplicit(%q)=%#v", tc.selector, route)
		}
	}
}

func TestParseExplicitRejectsImplicitAndEmptySegments(t *testing.T) {
	for _, value := range []string{
		"gpt-5.6",
		"/gpt-5.6",
		"openai-apikey/",
		"openai-apikey//gpt",
		"openrouter/anthropic/",
		"openrouter/anthropic//claude",
	} {
		if _, err := ParseExplicit(value); err == nil {
			t.Fatalf("ParseExplicit(%q) succeeded", value)
		}
	}
}
