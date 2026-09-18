package compat

import (
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
)

func TestClassifyHarnessAdapters(t *testing.T) {
	cases := []struct {
		harness string
		adapter string
		want    string
	}{
		{"codex", "openai-responses", VerdictVerified},
		{"codex", "openai-chat", VerdictDegraded},
		{"claude", "anthropic", VerdictVerified},
		{"claude", "openai-chat", VerdictDegraded},
		{"grok", "openai-responses", VerdictVerified},
		{"opencode", "openai-chat", VerdictVerified},
		{"opencode", "google", VerdictUnsupported},
		{"codex", "", VerdictUnknown},
	}
	for _, tc := range cases {
		got := Classify(tc.harness, tc.adapter, catalog.CapabilityUnknown)
		if got != tc.want {
			t.Fatalf("%s/%s: got %s want %s", tc.harness, tc.adapter, got, tc.want)
		}
	}
}

func TestSplitModelID(t *testing.T) {
	provider, model := SplitModelID("openai-apikey/gpt-5.4")
	if provider != "openai-apikey" || model != "gpt-5.4" {
		t.Fatalf("got %s %s", provider, model)
	}
}
