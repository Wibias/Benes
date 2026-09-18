package config

import (
	"encoding/json"
	"testing"

	"github.com/Wibias/Benes/internal/providerregistry"
)

func TestProjectProviderSpecsProjectsAnthropicMessagesNativeKeyAuth(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"anthropic-apikey": providerJSON(t, `{
			"adapter":"anthropic",
			"baseUrl":"https://api.anthropic.com",
			"apiKey":"test-key",
			"transientRetryOn5xx":{"enabled":true,"attempts":2}
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	spec := projection.Specs[0]
	if spec.Protocol != providerregistry.ProtocolAnthropicMessages {
		t.Fatalf("protocol=%q", spec.Protocol)
	}
	if spec.Endpoint != "https://api.anthropic.com/v1/messages" {
		t.Fatalf("endpoint=%q", spec.Endpoint)
	}
	if spec.APIKey != "test-key" || len(spec.APIKeyPool) != 0 {
		t.Fatalf("single Anthropic key was rewritten as a pool: %#v", spec)
	}
	if spec.APIKeyTransport != providerregistry.APIKeyTransportXAPIKey {
		t.Fatalf("apiKeyTransport=%q", spec.APIKeyTransport)
	}
	if !spec.Transient5xx.Enabled || spec.Transient5xx.Attempts != 2 || spec.Transient5xx.Sleep != nil {
		t.Fatalf("transient5xx=%#v", spec.Transient5xx)
	}
}

func TestProjectProviderSpecsProjectsAnthropicBearerTransport(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"gateway": providerJSON(t, `{
			"adapter":"anthropic",
			"baseUrl":"https://gateway.example/v1",
			"apiKey":"test-key",
			"apiKeyTransport":"bearer"
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	spec := projection.Specs[0]
	if spec.Endpoint != "https://gateway.example/v1/messages" || spec.APIKeyTransport != providerregistry.APIKeyTransportBearer {
		t.Fatalf("spec=%#v", spec)
	}
	if len(spec.APIKeyPool) != 0 {
		t.Fatalf("Anthropic bearer key was rewritten as a pool: %#v", spec.APIKeyPool)
	}
}

func TestProjectProviderSpecsAnthropicCredentialRefDoesNotRequirePlaintextKey(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"anthropic-apikey": providerJSON(t, `{
			"adapter":"anthropic",
			"baseUrl":"https://api.anthropic.com",
			"credentialRef":{"id":"anthropic-apikey","source":"secure-store"}
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if projection.Specs[0].APIKey != "" || projection.Specs[0].CredentialRef.ID != "anthropic-apikey" || len(projection.Specs[0].APIKeyPool) != 0 {
		t.Fatalf("spec=%#v", projection.Specs[0])
	}
}

func TestProjectProviderSpecsRejectsAnthropicAPIKeyPool(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"anthropic-apikey": providerJSON(t, `{
			"adapter":"anthropic",
			"baseUrl":"https://api.anthropic.com",
			"apiKeyPool":[{"id":"a","key":"key-a"},{"id":"b","key":"key-b"}]
		}`),
	}})
	if len(projection.Specs) != 0 || len(projection.Skipped) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if projection.Skipped[0].Code != "unsupported_field" || projection.Skipped[0].Field != "apiKeyPool" {
		t.Fatalf("skip=%#v", projection.Skipped[0])
	}
}

func TestProjectProviderSpecsRejectsInvalidAnthropicKeyTransport(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"anthropic-apikey": providerJSON(t, `{
			"adapter":"anthropic",
			"baseUrl":"https://api.anthropic.com",
			"apiKey":"test-key",
			"apiKeyTransport":"query-string"
		}`),
	}})
	if len(projection.Specs) != 0 || len(projection.Skipped) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if projection.Skipped[0].Code != "invalid_field" || projection.Skipped[0].Field != "apiKeyTransport" {
		t.Fatalf("skip=%#v", projection.Skipped[0])
	}
}

func TestProjectProviderSpecsDoesNotApplyAnthropicKeyTransportToOpenAI(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"gateway": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://gateway.example/v1",
			"apiKey":"test-key",
			"apiKeyTransport":"bearer"
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if projection.Specs[0].APIKeyTransport != "" {
		t.Fatalf("OpenAI inherited Anthropic apiKeyTransport=%q", projection.Specs[0].APIKeyTransport)
	}
}

func TestAnthropicMessagesEndpointCanonicalization(t *testing.T) {
	tests := map[string]string{
		"https://api.anthropic.com":                "https://api.anthropic.com/v1/messages",
		"https://api.anthropic.com/":               "https://api.anthropic.com/v1/messages",
		"https://gateway.example/v1":               "https://gateway.example/v1/messages",
		"https://gateway.example/v1/messages/":     "https://gateway.example/v1/messages",
		"https://opencode.ai/zen/go/v1":            "https://opencode.ai/zen/go/v1/messages",
		"https://gateway.example/custom/anthropic": "https://gateway.example/custom/anthropic/v1/messages",
	}
	for input, want := range tests {
		if got := anthropicMessagesEndpoint(input); got != want {
			t.Fatalf("anthropicMessagesEndpoint(%q)=%q want %q", input, got, want)
		}
	}
}
