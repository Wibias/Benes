package providerregistry

import (
	"context"
	"testing"

	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
)

type directOverrideAuthority struct{}

func (directOverrideAuthority) Resolve(context.Context, providers.DispatchRequest) (openairesponses.ForwardCredential, error) {
	return openairesponses.ForwardCredential{Authorization: "Bearer injected-direct"}, nil
}

func TestBuildDirectModeUsesInjectedAuthorityWhenProvided(t *testing.T) {
	resolver := &codexModeResolver{}
	registry, err := Build(context.Background(), []Spec{forwardModeSpec(CodexAccountModeDirect, resolver)}, Options{
		ForwardAuthorities: map[string]openairesponses.ForwardCredentialAuthority{"openai": directOverrideAuthority{}},
	})
	if err != nil || len(registry) != 1 {
		t.Fatalf("registry=%#v err=%v", registry, err)
	}
	if resolver.calls != 0 {
		t.Fatalf("Build resolved forward destination too early: calls=%d", resolver.calls)
	}
}
