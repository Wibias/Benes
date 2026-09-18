package server

import (
	"testing"

	"github.com/Wibias/Benes/internal/provideractivity"
)

func TestProvidersWorkspaceOpenAIOAuthDefaultIgnoresHiddenAPIHealthFailure(t *testing.T) {
	h, _ := newProvidersMutateHandler(t, `{
		"defaultProvider": "openai",
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward",
				"codexAccountMode": "direct",
				"defaultAccess": "oauth"
			},
			"openai-apikey": {
				"adapter": "openai-responses",
				"baseUrl": "https://api.openai.com/v1",
				"authMode": "key",
				"apiKey": "sk-test"
			}
		}
	}`, "openai", "openai-apikey")
	inner := h.(*handler)
	inner.activity.Record(provideractivity.Event{
		Provider:  "openai-apikey",
		Type:      "provider_health_failure",
		Severity:  "error",
		Timestamp: 1000,
	})

	lifecycle, codes := readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("hidden API-lane health must not poison OAuth-default OpenAI: lifecycle=%q issues=%v", lifecycle, codes)
	}
}
