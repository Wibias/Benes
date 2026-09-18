package providers

import "testing"

func TestImageURLMapsOpenAIFamilyEndpoints(t *testing.T) {
	cases := []struct {
		endpoint string
		kind     ImageRelayKind
		want     string
	}{
		{"https://api.openai.com/v1/responses", ImageRelayGenerations, "https://api.openai.com/v1/images/generations"},
		{"https://api.openai.com/v1/chat/completions", ImageRelayEdits, "https://api.openai.com/v1/images/edits"},
		{"https://chatgpt.com/backend-api/codex/responses", ImageRelayGenerations, "https://chatgpt.com/backend-api/codex/images/generations"},
		{"https://openrouter.ai/api/v1", ImageRelayGenerations, "https://openrouter.ai/api/v1/images/generations"},
	}
	for _, tc := range cases {
		got, err := ImageURL(tc.endpoint, tc.kind)
		if err != nil {
			t.Fatalf("endpoint=%q err=%v", tc.endpoint, err)
		}
		if got != tc.want {
			t.Fatalf("endpoint=%q got=%q want=%q", tc.endpoint, got, tc.want)
		}
	}
}

func TestImageCapabilityRejectsUnprovenTextOnlyHosts(t *testing.T) {
	if !ImageCapable("https://api.openai.com/v1/responses", "api-key") {
		t.Fatal("official OpenAI API-key endpoint must be image-capable")
	}
	if !ImageCapable("https://chatgpt.com/backend-api/codex/responses", "forward") {
		t.Fatal("native Codex ChatGPT forward must be image-capable")
	}
	if ImageCapable("https://api.x.ai/v1/chat/completions", "api-key") {
		t.Fatal("xAI chat endpoint must not be treated as an OpenAI image upstream")
	}
	if ImageCapable("https://cli-chat-proxy.grok.com/v1/chat/completions", "oauth") {
		t.Fatal("xAI OAuth proxy must not be treated as an OpenAI image upstream")
	}
	if ImageCapable("https://api.anthropic.com/v1/messages", "api-key") {
		t.Fatal("Anthropic must not be treated as an OpenAI image upstream")
	}
}
