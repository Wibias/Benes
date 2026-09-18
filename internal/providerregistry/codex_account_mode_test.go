package providerregistry

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
	"github.com/Wibias/Benes/internal/transport"
)

type registryForwardAuthority struct{}

func (registryForwardAuthority) Resolve(context.Context, providers.DispatchRequest) (openairesponses.ForwardCredential, error) {
	return openairesponses.ForwardCredential{Authorization: "Bearer selected"}, nil
}

type codexModeResolver struct{ calls int }

func (r *codexModeResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	r.calls++
	return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
}

func forwardModeSpec(mode CodexAccountMode, resolver transport.Resolver) Spec {
	return Spec{
		ID: "openai", Protocol: ProtocolOpenAIResponses, AuthMode: AuthModeForward,
		CodexAccountMode:  mode,
		Endpoint:          "https://chatgpt.com/backend-api/codex/responses",
		DestinationPolicy: transport.DestinationPolicy{Resolver: resolver},
	}
}

func TestBuildPoolModeRequiresExplicitForwardAuthorityBeforeResolution(t *testing.T) {
	resolver := &codexModeResolver{}
	registry, err := Build(context.Background(), []Spec{forwardModeSpec(CodexAccountModePool, resolver)}, Options{})
	if err == nil || registry != nil || !errors.Is(err, ErrCodexPoolAuthorityRequired) {
		t.Fatalf("registry=%#v err=%v", registry, err)
	}
	if resolver.calls != 0 {
		t.Fatalf("pool dependency failure reached DNS: %d", resolver.calls)
	}
}

func TestBuildPoolModeInjectsAuthority(t *testing.T) {
	resolver := &codexModeResolver{}
	registry, err := Build(context.Background(), []Spec{forwardModeSpec(CodexAccountModePool, resolver)}, Options{
		ForwardAuthorities: map[string]openairesponses.ForwardCredentialAuthority{"openai": registryForwardAuthority{}},
	})
	if err != nil || len(registry) != 1 {
		t.Fatalf("registry=%#v err=%v", registry, err)
	}
	if resolver.calls != 0 {
		t.Fatalf("Build resolved forward destination too early: calls=%d", resolver.calls)
	}
}

func TestBuildDirectModeDoesNotRequireInjectedPoolAuthority(t *testing.T) {
	resolver := &codexModeResolver{}
	registry, err := Build(context.Background(), []Spec{forwardModeSpec(CodexAccountModeDirect, resolver)}, Options{})
	if err != nil || len(registry) != 1 {
		t.Fatalf("registry=%#v err=%v", registry, err)
	}
	if resolver.calls != 0 {
		t.Fatalf("Build resolved forward destination too early: calls=%d", resolver.calls)
	}
}

func TestBuildModeEmptyForwardSpecPreservesCallerForwardCompatibility(t *testing.T) {
	resolver := &codexModeResolver{}
	spec := forwardModeSpec("", resolver)
	spec.ID = "custom"
	registry, err := Build(context.Background(), []Spec{spec}, Options{})
	if err != nil || len(registry) != 1 {
		t.Fatalf("registry=%#v err=%v", registry, err)
	}
}

func TestBuildRejectsCodexModeOnNonForwardShapeBeforeResolution(t *testing.T) {
	resolver := &codexModeResolver{}
	tests := []Spec{
		{ID: "openai", Protocol: ProtocolOpenAIResponses, AuthMode: AuthModeKey, CodexAccountMode: CodexAccountModePool, Endpoint: "https://api.openai.com/v1/responses", APIKey: "k", DestinationPolicy: transport.DestinationPolicy{Resolver: resolver}},
		{ID: "openai", Protocol: ProtocolOpenAIChat, AuthMode: AuthModeKey, CodexAccountMode: CodexAccountModeDirect, Endpoint: "https://api.openai.com/v1/chat/completions", APIKey: "k", DestinationPolicy: transport.DestinationPolicy{Resolver: resolver}},
		{ID: "openai", Protocol: ProtocolOpenAIResponses, AuthMode: AuthModeForward, CodexAccountMode: CodexAccountMode("future"), Endpoint: "https://chatgpt.com/backend-api/codex/responses", DestinationPolicy: transport.DestinationPolicy{Resolver: resolver}},
	}
	for _, spec := range tests {
		registry, err := Build(context.Background(), []Spec{spec}, Options{})
		if err == nil || registry != nil {
			t.Fatalf("spec=%#v registry=%#v err=%v", spec, registry, err)
		}
	}
	if resolver.calls != 0 {
		t.Fatalf("invalid mode specs reached DNS: %d", resolver.calls)
	}
}
