package providerregistry

import (
	"context"
	"net/netip"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers/openairesponses"
	"github.com/Wibias/Benes/internal/providers/xaicapability"
	"github.com/Wibias/Benes/internal/transport"
)

type authModeResolver struct {
	hosts []string
}

func (r *authModeResolver) LookupNetIP(_ context.Context, _ string, host string) ([]netip.Addr, error) {
	r.hosts = append(r.hosts, host)
	return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
}

func TestBuildConstructsForwardResponsesProviderWithPinnedResolver(t *testing.T) {
	resolver := &authModeResolver{}
	registry, err := Build(context.Background(), []Spec{{
		ID:                "openai",
		Protocol:          ProtocolOpenAIResponses,
		AuthMode:          AuthModeForward,
		Endpoint:          "https://chatgpt.com/backend-api/codex/responses",
		DestinationPolicy: transport.DestinationPolicy{Resolver: resolver},
	}}, Options{})
	if err != nil {
		t.Fatalf("Build(): %v", err)
	}
	if len(registry) != 1 {
		t.Fatalf("registry=%#v", registry)
	}
	if _, ok := registry["openai"].(*openairesponses.ForwardClient); !ok {
		t.Fatalf("provider type=%T", registry["openai"])
	}
	if len(resolver.hosts) != 0 {
		t.Fatalf("Build resolved forward destination too early: hosts=%#v", resolver.hosts)
	}
}

func TestBuildAcceptsXAIOAuthChatOnCLIProxy(t *testing.T) {
	resolver := &authModeResolver{}
	registry, err := Build(context.Background(), []Spec{{
		ID:                "xai",
		Protocol:          ProtocolOpenAIChat,
		AuthMode:          AuthModeOAuth,
		Endpoint:          xaicapability.OAuthChatEndpoint(),
		APIKey:            "oauth-tok",
		DestinationPolicy: transport.DestinationPolicy{Resolver: resolver},
	}}, Options{})
	if err != nil {
		t.Fatalf("Build(): %v", err)
	}
	if len(registry) != 1 || registry["xai"] == nil {
		t.Fatalf("registry=%#v", registry)
	}
	if len(resolver.hosts) != 1 || resolver.hosts[0] != "cli-chat-proxy.grok.com" {
		t.Fatalf("hosts=%#v", resolver.hosts)
	}
}

func TestBuildTreatsEmptyAuthModeAsKeyForCompatibility(t *testing.T) {
	_, err := Build(context.Background(), []Spec{{
		ID:                "native",
		Protocol:          ProtocolOpenAIResponses,
		Endpoint:          "https://127.0.0.1/v1/responses",
		APIKey:            "key",
		DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
	}}, Options{})
	if err != nil {
		t.Fatalf("Build(): %v", err)
	}
}

func TestBuildRejectsInvalidForwardAuthCombinationsBeforeResolution(t *testing.T) {
	tests := []struct {
		name string
		spec Spec
		want string
	}{
		{
			name: "forward chat wire",
			spec: Spec{ID: "chat", Protocol: ProtocolOpenAIChat, AuthMode: AuthModeForward, Endpoint: "https://chatgpt.com/backend-api/codex/responses"},
			want: "forward auth is only supported for openai-responses",
		},
		{
			name: "forward plus api key",
			spec: Spec{ID: "responses", Protocol: ProtocolOpenAIResponses, AuthMode: AuthModeForward, Endpoint: "https://chatgpt.com/backend-api/codex/responses", APIKey: "SECRET-KEY"},
			want: "forward auth cannot include a provider API key",
		},
		{
			name: "unknown auth mode",
			spec: Spec{ID: "responses", Protocol: ProtocolOpenAIResponses, AuthMode: AuthMode("future"), Endpoint: "https://chatgpt.com/backend-api/codex/responses"},
			want: "unsupported provider auth mode",
		},
		{
			name: "oauth outside xai",
			spec: Spec{ID: "openrouter", Protocol: ProtocolOpenAIChat, AuthMode: AuthModeOAuth, Endpoint: "https://openrouter.ai/api/v1/chat/completions", APIKey: "k"},
			want: "oauth auth is only supported for xai",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &authModeResolver{}
			tc.spec.DestinationPolicy.Resolver = resolver
			registry, err := Build(context.Background(), []Spec{tc.spec}, Options{})
			if err == nil || registry != nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("registry=%#v err=%v want=%q", registry, err, tc.want)
			}
			if len(resolver.hosts) != 0 {
				t.Fatalf("invalid auth mode reached resolver: %#v", resolver.hosts)
			}
			if strings.Contains(err.Error(), "SECRET-KEY") {
				t.Fatalf("credential leaked in error: %v", err)
			}
		})
	}
}

func TestBuildForwardRejectsNonCanonicalEndpointBeforeResolution(t *testing.T) {
	resolver := &authModeResolver{}
	registry, err := Build(context.Background(), []Spec{{
		ID:                "openai",
		Protocol:          ProtocolOpenAIResponses,
		AuthMode:          AuthModeForward,
		Endpoint:          "https://example.com/responses",
		DestinationPolicy: transport.DestinationPolicy{Resolver: resolver},
	}}, Options{})
	if err == nil || registry != nil {
		t.Fatalf("registry=%#v err=%v", registry, err)
	}
	if len(resolver.hosts) != 0 {
		t.Fatalf("noncanonical endpoint reached resolver: %#v", resolver.hosts)
	}
}
