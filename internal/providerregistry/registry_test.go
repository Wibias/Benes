package providerregistry

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/capability"
	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/providers/antigravity"
	"github.com/Wibias/Benes/internal/providers/google"
	"github.com/Wibias/Benes/internal/providers/kiro"
	"github.com/Wibias/Benes/internal/providers/openaichat"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
	"github.com/Wibias/Benes/internal/transport"
)

func TestBuildCreatesBothMigratedProviderProtocols(t *testing.T) {
	upstream := httptest.NewServer(nil)
	defer upstream.Close()

	registry, err := Build(context.Background(), []Spec{
		{
			ID:                "native",
			Protocol:          ProtocolOpenAIResponses,
			Endpoint:          upstream.URL + "/v1/responses",
			APIKey:            "responses-key",
			DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
		},
		{
			ID:                "openrouter",
			Protocol:          ProtocolOpenAIChat,
			Endpoint:          upstream.URL + "/v1/chat/completions",
			APIKey:            "chat-key",
			DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
			Chat:              ChatOptions{NativeOpenAI: false, PreserveReasoningContent: true},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Build(): %v", err)
	}
	if len(registry) != 2 {
		t.Fatalf("registry=%#v", registry)
	}
	if _, ok := registry["native"].(*openairesponses.Client); !ok {
		t.Fatalf("native provider type=%T", registry["native"])
	}
	if _, ok := registry["openrouter"].(*openaichat.Client); !ok {
		t.Fatalf("openrouter provider type=%T", registry["openrouter"])
	}
}

func TestBuildConstructsKiroOAuthFromStoredSnapshot(t *testing.T) {
	registry, err := Build(context.Background(), []Spec{{
		ID:         "kiro",
		Protocol:   ProtocolKiro,
		AuthMode:   AuthModeOAuth,
		APIKey:     "tok",
		ProfileARN: "arn:aws:codewhisperer:us-east-1:123456789012:profile/abc",
		APIRegion:  "us-east-1",
		Endpoint:   "https://runtime.us-east-1.kiro.dev",
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry["kiro"].(*kiro.Client); !ok {
		t.Fatalf("type=%T", registry["kiro"])
	}
}

func TestBuildConstructsKiroProvider(t *testing.T) {
	registry, err := Build(context.Background(), []Spec{{
		ID:         "kiro",
		Protocol:   ProtocolKiro,
		APIKey:     "tok",
		ProfileARN: "arn:aws:codewhisperer:us-east-1:123456789012:profile/abc",
		APIRegion:  "us-east-1",
		Endpoint:   "https://runtime.us-east-1.kiro.dev",
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry["kiro"].(*kiro.Client); !ok {
		t.Fatalf("type=%T", registry["kiro"])
	}
}

func TestBuildConstructsKiroBuilderIDWithoutProfileARN(t *testing.T) {
	registry, err := Build(context.Background(), []Spec{{
		ID:        "kiro",
		Protocol:  ProtocolKiro,
		APIKey:    "tok",
		APIRegion: "us-east-1",
		AuthType:  "aws_sso_oidc",
		Endpoint:  "https://runtime.us-east-1.kiro.dev",
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry["kiro"].(*kiro.Client); !ok {
		t.Fatalf("type=%T", registry["kiro"])
	}
}

func TestBuildConstructsGoogleAndVertexProviders(t *testing.T) {
	registry, err := Build(context.Background(), []Spec{
		{ID: "google", Protocol: ProtocolGoogle, APIKey: "gk"},
		{ID: "vertex", Protocol: ProtocolGoogleVertex, APIKey: "vk"},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry["google"].(*google.Client); !ok {
		t.Fatalf("google=%T", registry["google"])
	}
	if _, ok := registry["vertex"].(*google.Client); !ok {
		t.Fatalf("vertex=%T", registry["vertex"])
	}
}

func TestBuildConstructsVertexWithoutAPIKeyUsingProjectAndLocation(t *testing.T) {
	registry, err := Build(context.Background(), []Spec{{
		ID:       "vertex",
		Protocol: ProtocolGoogleVertex,
		Project:  "proj",
		Location: "us-central1",
	}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry["vertex"].(*google.Client); !ok {
		t.Fatalf("vertex=%T", registry["vertex"])
	}
}

func TestBuildRejectsInvalidProviderIDs(t *testing.T) {
	for _, id := range []string{"", " ", "/bad", "bad/name", " bad", "bad ", "bad\tname"} {
		_, err := Build(context.Background(), []Spec{{ID: id, Protocol: ProtocolOpenAIChat, APIKey: "k"}}, Options{})
		if err == nil {
			t.Fatalf("Build() accepted id %q", id)
		}
	}
}

func TestBuildRejectsDuplicateProviderIDs(t *testing.T) {
	_, err := Build(context.Background(), []Spec{
		{ID: "same", Protocol: ProtocolOpenAIChat, APIKey: "a"},
		{ID: "same", Protocol: ProtocolOpenAIResponses, APIKey: "b"},
	}, Options{})
	if err == nil || !strings.Contains(err.Error(), "duplicate provider id") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildRejectsUnknownProtocolBeforeCredentialOrNetworkWork(t *testing.T) {
	_, err := Build(context.Background(), []Spec{{
		ID: "future", Protocol: Protocol("future-protocol"), APIKey: "SECRET-KEY",
	}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "unsupported provider protocol") {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), "SECRET-KEY") {
		t.Fatalf("credential leaked in error: %v", err)
	}
}

func TestBuildDoesNotLeakProviderCredentialInConstructionErrors(t *testing.T) {
	secret := "VERY-SECRET-PROVIDER-KEY"
	_, err := Build(context.Background(), []Spec{{
		ID:       "broken",
		Protocol: ProtocolOpenAIChat,
		Endpoint: "ftp://example.com/v1/chat/completions",
		APIKey:   secret,
	}}, Options{})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("credential leaked in error: %v", err)
	}
}

func TestBuildFailsClosedIfAnyProviderCannotBeConstructed(t *testing.T) {
	upstream := httptest.NewServer(nil)
	defer upstream.Close()
	registry, err := Build(context.Background(), []Spec{
		{
			ID: "good", Protocol: ProtocolOpenAIChat, Endpoint: upstream.URL,
			APIKey: "k", DestinationPolicy: transport.DestinationPolicy{AllowPrivateNetwork: true},
		},
		{ID: "bad", Protocol: ProtocolOpenAIChat, Endpoint: "ftp://example.com/v1/chat/completions", APIKey: "k"},
	}, Options{})
	if err == nil {
		t.Fatal("expected construction error")
	}
	if registry != nil {
		t.Fatalf("partial registry escaped: %#v", registry)
	}
}

func TestBuildRejectsEmptySpecSet(t *testing.T) {
	registry, err := Build(context.Background(), nil, Options{})
	if err == nil || registry != nil {
		t.Fatalf("registry=%#v err=%v", registry, err)
	}
}

func TestAdoptSpecSecretsPutsPlaintextIntoStore(t *testing.T) {
	store, err := credentials.NewFileStore(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := adoptSpecSecrets(store, Spec{
		ID:     "openai-apikey",
		APIKey: "sk-live",
		APIKeyPool: []APIKeySlot{
			{ID: "primary", Key: "sk-live"},
			{ID: "backup", Key: "sk-backup"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if spec.CredentialRef.ID != "openai-apikey" || spec.CredentialRef.Source != credentials.SourceSecureStore {
		t.Fatalf("ref=%#v", spec.CredentialRef)
	}
	if spec.APIKey != "" || spec.APIKeyPool[0].Key != "" || spec.APIKeyPool[1].Key != "" {
		t.Fatalf("plaintext remained after adopt: %#v", spec)
	}
	if spec.APIKeyPool[0].Ref.ID == "" || spec.APIKeyPool[1].Ref.ID == "" {
		t.Fatalf("pool refs missing: %#v", spec.APIKeyPool)
	}
	got, err := store.Get(spec.CredentialRef)
	if err != nil || string(got) != "sk-live" {
		t.Fatalf("primary=%q err=%v", got, err)
	}
	backup, err := store.Get(credentials.Ref{ID: "openai-apikey-pool-1", Source: credentials.SourceSecureStore})
	if err != nil || string(backup) != "sk-backup" {
		t.Fatalf("backup=%q err=%v", backup, err)
	}
	resolved, err := resolveSpecSecrets(store, spec)
	if err != nil || resolved.APIKey != "sk-live" || resolved.APIKeyPool[1].Key != "sk-backup" {
		t.Fatalf("resolve=%#v err=%v", resolved, err)
	}
}

func TestLiveCredentialRefsCollectsProviderAndPoolSlots(t *testing.T) {
	refs := LiveCredentialRefs([]Spec{
		{CredentialRef: credentials.Ref{ID: "primary", Source: credentials.SourceSecureStore}},
		{APIKeyPool: []APIKeySlot{{Ref: credentials.Ref{ID: "pool-1", Source: credentials.SourceSecureStore}}}},
	})
	if len(refs) != 2 || refs[0].ID != "primary" || refs[1].ID != "pool-1" {
		t.Fatalf("refs=%#v", refs)
	}
}

func TestAdoptSpecSecretsLeavesForwardAuthAndNilStoreUnchanged(t *testing.T) {
	forward := Spec{ID: "openai", AuthMode: AuthModeForward, APIKey: ""}
	got, err := adoptSpecSecrets(nil, forward)
	if err != nil || got.CredentialRef.ID != "" {
		t.Fatalf("nil store forward=%#v err=%v", got, err)
	}
	store, err := credentials.NewFileStore(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	got, err = adoptSpecSecrets(store, forward)
	if err != nil || got.CredentialRef.ID != "" {
		t.Fatalf("forward adopted=%#v err=%v", got, err)
	}
}

func TestAntigravityAccountsBindProjectNotProviderID(t *testing.T) {
	got := antigravityAccounts(Spec{ID: "google-antigravity", APIKey: "tok", Project: "cca-proj"})
	if len(got) != 1 || got[0].ID != "google-antigravity" || got[0].Token != "tok" || got[0].ProjectID != "cca-proj" {
		t.Fatalf("single=%#v", got)
	}
	if got[0].ProjectID == got[0].ID {
		t.Fatal("provider id must not be reused as the CCA project")
	}
	pool := antigravityAccounts(Spec{
		ID:      "google-antigravity",
		Project: "shared",
		APIKeyPool: []APIKeySlot{
			{ID: "a", Key: "ta"},
			{ID: "b", Key: "tb"},
		},
	})
	if len(pool) != 2 || pool[0].ProjectID != "shared" || pool[1].ProjectID != "shared" || pool[1].Token != "tb" {
		t.Fatalf("pool=%#v", pool)
	}
	explicit := antigravityAccounts(Spec{
		ID:       "google-antigravity",
		APIKey:   "ignored",
		Project:  "wrong",
		Accounts: []antigravity.Account{{ID: "live", Token: "live-tok", ProjectID: "live-proj"}},
	})
	if len(explicit) != 1 || explicit[0].ProjectID != "live-proj" || explicit[0].Token != "live-tok" {
		t.Fatalf("explicit=%#v", explicit)
	}
}

func TestCapabilityPolicyPreservesStructuredOutputEvidence(t *testing.T) {
	providerDefault := false
	policy := capabilityPolicy(Spec{
		ID:       "google",
		Protocol: ProtocolGoogle,
		Capability: Capability{
			SupportsStructuredOutput: &providerDefault,
			ModelSupportsStructuredOutput: map[string]bool{
				"gemini-3.7-flash": true,
			},
		},
	}, "api-key")
	if supported, known := capability.StructuredOutputSupport(policy, "gemini-3.7-flash"); !known || !supported {
		t.Fatalf("exact support=%v known=%v", supported, known)
	}
	if supported, known := capability.StructuredOutputSupport(policy, "unknown-model"); !known || supported {
		t.Fatalf("provider default support=%v known=%v", supported, known)
	}
}

func TestBuildOpenAICompatibleConfiguredUserAgent(t *testing.T) {
	for _, protocol := range []Protocol{ProtocolOpenAIChat, ProtocolOpenAIResponses} {
		t.Run(string(protocol), func(t *testing.T) {
			registry, err := Build(context.Background(), []Spec{{
				ID: "p", Protocol: protocol, Endpoint: "https://example.com/v1",
				APIKey: "k", UserAgent: "provider-agent/1",
			}}, Options{TransportOptions: transport.ClientOptions{}})
			if err != nil {
				t.Fatal(err)
			}
			if registry["p"] == nil {
				t.Fatal("provider missing")
			}
		})
	}
	if _, err := Build(context.Background(), []Spec{{
		ID: "google", Protocol: ProtocolGoogle, Endpoint: "https://generativelanguage.googleapis.com",
		APIKey: "k", UserAgent: "provider-agent/1",
	}}, Options{TransportOptions: transport.ClientOptions{}}); err == nil {
		t.Fatal("non OpenAI-compatible provider accepted configured user agent")
	}
}

