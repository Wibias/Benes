package bootstrap

import (
	"context"
	"testing"

	"github.com/Wibias/Benes/internal/providerregistry"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
)

type bootstrapAuthorityStub struct{ token string }

func (a bootstrapAuthorityStub) Resolve(context.Context, providers.DispatchRequest) (openairesponses.ForwardCredential, error) {
	return openairesponses.ForwardCredential{Authorization: "Bearer " + a.token}, nil
}

func TestCloneRegistryOptionsDoesNotMutateCallerAuthorityMap(t *testing.T) {
	originalAuthority := bootstrapAuthorityStub{token: "original"}
	options := providerregistry.Options{
		ForwardAuthorities: map[string]openairesponses.ForwardCredentialAuthority{
			"openai": originalAuthority,
		},
	}
	cloned := cloneRegistryOptions(options)
	cloned.ForwardAuthorities["openai"] = bootstrapAuthorityStub{token: "runtime"}

	credential, err := options.ForwardAuthorities["openai"].Resolve(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if credential.Authorization != "Bearer original" {
		t.Fatalf("original authority mutated: %#v", credential)
	}
}
