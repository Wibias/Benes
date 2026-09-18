package bootstrap

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/providerregistry"
)

func TestBuildCodexPoolAuthoritiesDirectModeInitialisesStoredRuntimeOnlyForExactNamespaces(t *testing.T) {
	direct := providerregistry.Spec{
		ID: "openai", Protocol: providerregistry.ProtocolOpenAIResponses,
		AuthMode: providerregistry.AuthModeForward, CodexAccountMode: providerregistry.CodexAccountModeDirect,
	}

	t.Run("no namespaces skips stored runtime", func(t *testing.T) {
		called := 0
		authorities, err := buildCodexPoolAuthorities(context.Background(), config.DiskConfig{Raw: json.RawMessage(`{not-json`)}, []providerregistry.Spec{direct}, CodexPoolOptions{
			BenesHome: "relative-must-not-be-read",
			ResolveCodexHome: func() (string, error) {
				called++
				return "", nil
			},
		})
		if err != nil || authorities != nil || called != 0 {
			t.Fatalf("authorities=%#v err=%v resolveCalls=%d", authorities, err, called)
		}
	})

	t.Run("namespace requires stored runtime", func(t *testing.T) {
		disk := config.DiskConfig{
			Raw:                    json.RawMessage(`{}`),
			CodexAccountNamespaces: map[string]string{"side": "acct-b"},
		}
		authorities, err := buildCodexPoolAuthorities(context.Background(), disk, []providerregistry.Spec{direct}, CodexPoolOptions{
			BenesHome: t.TempDir(), CodexHome: t.TempDir(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if authorities == nil || authorities["openai"] == nil {
			t.Fatalf("authorities=%#v", authorities)
		}
	})
}

func TestBuildCodexPoolAuthoritiesRejectsExactNamespacesWithoutCodexAuthority(t *testing.T) {
	disk := config.DiskConfig{
		Raw:                    json.RawMessage(`{}`),
		CodexAccountNamespaces: map[string]string{"side": "acct-b"},
	}
	keyAuth := providerregistry.Spec{
		ID: "openai", Protocol: providerregistry.ProtocolOpenAIResponses,
		AuthMode: providerregistry.AuthModeKey,
	}
	if err := validateCodexExactAccountAuthority(disk, []providerregistry.Spec{keyAuth}); err == nil {
		t.Fatal("expected exact-account namespace configuration to fail without a Codex authority")
	}
}
