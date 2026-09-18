package openairesponses

import "testing"

func TestSupportsNativeHostedWebSearchDestinationExactAllowlist(t *testing.T) {
	for _, destination := range []string{
		"https://api.openai.com/v1/responses",
		"https://api.openai.com/v1/responses/",
		"https://chatgpt.com/backend-api/codex/responses",
		"https://opencode.ai/zen/go/v1/responses",
	} {
		if !SupportsNativeHostedWebSearchDestination(destination) {
			t.Fatalf("expected native hosted search for %q", destination)
		}
	}
}

func TestSupportsNativeHostedWebSearchDestinationFailsClosed(t *testing.T) {
	for _, destination := range []string{
		"http://api.openai.com/v1/responses",
		"https://api.openai.com:443/v1/responses",
		"https://api.openai.com/v1/chat/completions",
		"https://opencode.ai/zen/go/v1/responses?x=1",
		"https://opencode.ai/zen/go/v1/responses#fragment",
		"https://user@opencode.ai/zen/go/v1/responses",
		"https://example.com/v1/responses",
	} {
		if SupportsNativeHostedWebSearchDestination(destination) {
			t.Fatalf("unexpected native hosted search for %q", destination)
		}
	}
}
