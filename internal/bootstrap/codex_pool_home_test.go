package bootstrap

import (
	"context"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/providerregistry"
)

func TestBuildCodexPoolAuthoritiesResolvesCodexHomeLazilyForPool(t *testing.T) {
	calls := 0
	codexHome := t.TempDir()
	authorities, err := buildCodexPoolAuthorities(context.Background(), poolDiskForTest(t, `{}`), []providerregistry.Spec{poolSpecForTest()}, CodexPoolOptions{
		BenesHome: t.TempDir(),
		ResolveCodexHome: func() (string, error) {
			calls++
			return codexHome, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || authorities["openai"] == nil {
		t.Fatalf("calls=%d authorities=%#v", calls, authorities)
	}
}

func TestBuildCodexPoolAuthoritiesDirectOnlyDoesNotRequireValidDiskRoot(t *testing.T) {
	authorities, err := buildCodexPoolAuthorities(context.Background(), config.DiskConfig{Raw: []byte(`{not-json`)}, []providerregistry.Spec{{
		ID: "openai", Protocol: providerregistry.ProtocolOpenAIResponses,
		AuthMode: providerregistry.AuthModeForward, CodexAccountMode: providerregistry.CodexAccountModeDirect,
	}}, CodexPoolOptions{})
	if err != nil || authorities != nil {
		t.Fatalf("authorities=%#v err=%v", authorities, err)
	}
}
