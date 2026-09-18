package providerregistry

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers/anthropicmessages"
	"github.com/Wibias/Benes/internal/transport"
)

func TestBuildConstructsAnthropicMessagesProvider(t *testing.T) {
	upstream := httptest.NewServer(nil)
	defer upstream.Close()
	registry, err := Build(context.Background(), []Spec{{
		ID:                "anthropic-apikey",
		Protocol:          ProtocolAnthropicMessages,
		Endpoint:          upstream.URL + "/v1/messages",
		APIKey:            "test-key",
		APIKeyTransport:   APIKeyTransportXAPIKey,
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry["anthropic-apikey"].(*anthropicmessages.Client); !ok {
		t.Fatalf("provider type=%T", registry["anthropic-apikey"])
	}
}

func TestBuildRejectsAnthropicOAuthInKeyAuthAdapter(t *testing.T) {
	_, err := Build(context.Background(), []Spec{{
		ID:       "anthropic",
		Protocol: ProtocolAnthropicMessages,
		AuthMode: AuthModeOAuth,
		Endpoint: "https://api.anthropic.com/v1/messages",
		APIKey:   "test-key",
	}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "oauth") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildRejectsAnthropicAPIKeyPool(t *testing.T) {
	_, err := Build(context.Background(), []Spec{{
		ID:       "anthropic-apikey",
		Protocol: ProtocolAnthropicMessages,
		Endpoint: "https://api.anthropic.com/v1/messages",
		APIKey:   "test-key",
		APIKeyPool: []APIKeySlot{
			{ID: "a", Key: "key-a"},
			{ID: "b", Key: "key-b"},
		},
	}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "API key pool") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildRejectsInvalidAnthropicKeyTransport(t *testing.T) {
	_, err := Build(context.Background(), []Spec{{
		ID:              "anthropic-apikey",
		Protocol:        ProtocolAnthropicMessages,
		Endpoint:        "https://api.anthropic.com/v1/messages",
		APIKey:          "test-key",
		APIKeyTransport: APIKeyTransport("query-string"),
	}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "API key transport") {
		t.Fatalf("err=%v", err)
	}
}
