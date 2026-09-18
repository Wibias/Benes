package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/providerregistry"
	"github.com/Wibias/Benes/internal/providers/xaicapability"
)

func TestAttachXAIAccountBindsOAuthTokenToProxySpecOnly(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "auth.json"), []byte(`{"xai":{"accounts":[{"id":"acct-a","credential":{"access":"oauth-tok"}}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	specs := []providerregistry.Spec{
		{ID: "xai", Endpoint: xaicapability.OAuthChatEndpoint(), APIKey: "sk-should-not-survive"},
		{ID: "openrouter", Endpoint: "https://openrouter.ai/api/v1/chat/completions"},
	}
	attachXAIAccount(home, specs)
	if specs[0].APIKey != "oauth-tok" || specs[0].CredentialRef.ID != "" {
		t.Fatalf("proxy spec=%#v", specs[0])
	}
	if specs[1].APIKey != "" {
		t.Fatalf("unrelated spec received xAI oauth token: %#v", specs[1])
	}
}

func TestDropUnresolvedXAIOAuthDoesNotBlockOtherProviders(t *testing.T) {
	specs, skipped := dropUnresolvedXAIOAuth([]providerregistry.Spec{
		{ID: "xai", Endpoint: xaicapability.OAuthChatEndpoint()},
		{ID: "openrouter", Endpoint: "https://openrouter.ai/api/v1/chat/completions", APIKey: "k"},
	}, nil)
	if len(specs) != 1 || specs[0].ID != "openrouter" {
		t.Fatalf("specs=%#v", specs)
	}
	if len(skipped) != 1 || skipped[0] != (config.ProviderProjectionSkip{ID: "xai", Code: "missing_credential", Field: "apiKey"}) {
		t.Fatalf("skipped=%#v", skipped)
	}
}

func TestCatalogCandidatesPairOAuthAuthClassWithProxyDestination(t *testing.T) {
	oauth := catalogCandidates(providerregistry.Spec{Endpoint: xaicapability.OAuthChatEndpoint(), APIKey: "oauth-tok"})
	if len(oauth) != 1 || oauth[0].AuthClass != xaicapability.AuthClassOAuth || oauth[0].Destination != xaicapability.OAuthChatEndpoint() {
		t.Fatalf("oauth=%#v", oauth)
	}
	key := catalogCandidates(providerregistry.Spec{Endpoint: xaicapability.CanonicalAPI + "/chat/completions", APIKey: "sk-xai"})
	if len(key) != 1 || key[0].AuthClass != xaicapability.AuthClassAPIKey || key[0].Destination != xaicapability.CanonicalAPI+"/chat/completions" {
		t.Fatalf("key=%#v", key)
	}
}
