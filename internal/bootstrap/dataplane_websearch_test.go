package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/providerregistry"
	"github.com/Wibias/Benes/internal/transport"
)

func TestWebSearchConfigsCopiesCredentialRefWithoutPlaintext(t *testing.T) {
	ref := credentials.Ref{ID: "openai-apikey", Source: credentials.SourceSecureStore}
	got := webSearchConfigs([]providerregistry.Spec{{
		ID:            "openai-apikey",
		Endpoint:      "https://api.openai.com/v1/responses",
		AuthMode:      providerregistry.AuthModeKey,
		Protocol:      providerregistry.ProtocolOpenAIResponses,
		CredentialRef: ref,
		Capability: providerregistry.Capability{
			HostedWebSearch: true,
			WebSearchModels: []string{"gpt-5.6"},
		},
	}}, 0, false)
	cfg := got["openai-apikey"]
	if cfg.APIKey != "" {
		t.Fatalf("plaintext leaked: %#v", cfg)
	}
	if cfg.CredentialRef != ref || !cfg.Enabled || cfg.Endpoint != "https://api.openai.com/v1/responses" {
		t.Fatalf("cfg=%#v", cfg)
	}
	if cfg.MaxSearches != 3 {
		t.Fatalf("maxSearches=%d", cfg.MaxSearches)
	}
}

func TestWebSearchConfigsPinsHTTPClientToResolvedDestination(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(upstream.Close)
	got := webSearchConfigs([]providerregistry.Spec{{
		ID:       "openai-apikey",
		Endpoint: upstream.URL,
		AuthMode: providerregistry.AuthModeKey,
		Protocol: providerregistry.ProtocolOpenAIResponses,
		Capability: providerregistry.Capability{
			HostedWebSearch: true,
			WebSearchModels: []string{"gpt-5.6"},
		},
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	}}, 0, false)
	cfg := got["openai-apikey"]
	if cfg.HTTPClient == nil {
		t.Fatal("sidecar HTTP client was not pinned")
	}
}

func TestWebSearchConfigsOmitsUnresolvableDestinations(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(upstream.Close)
	got := webSearchConfigs([]providerregistry.Spec{{
		ID:       "openai-apikey",
		Endpoint: upstream.URL,
		AuthMode: providerregistry.AuthModeKey,
		Protocol: providerregistry.ProtocolOpenAIResponses,
		Capability: providerregistry.Capability{
			HostedWebSearch: true,
			WebSearchModels: []string{"gpt-5.6"},
		},
	}}, 0, false)
	if _, exists := got["openai-apikey"]; exists {
		t.Fatalf("unresolvable private destination was enabled: %#v", got["openai-apikey"])
	}
}

func TestWebSearchConfigsHonorsConfiguredMaxSearches(t *testing.T) {
	got := webSearchConfigs([]providerregistry.Spec{{
		ID:       "openai-apikey",
		Endpoint: "https://api.openai.com/v1/responses",
		AuthMode: providerregistry.AuthModeKey,
		Protocol: providerregistry.ProtocolOpenAIResponses,
		Capability: providerregistry.Capability{
			HostedWebSearch: true,
			WebSearchModels: []string{"gpt-5.6"},
		},
	}}, 5, false)
	if got["openai-apikey"].MaxSearches != 5 {
		t.Fatalf("maxSearches=%d", got["openai-apikey"].MaxSearches)
	}
}
