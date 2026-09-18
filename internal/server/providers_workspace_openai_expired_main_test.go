package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
)

func TestProvidersWorkspaceOpenAIPoolExpiredMainRequiresReauthWithoutAlternate(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward",
				"codexAccountMode": "pool",
				"defaultAccess": "oauth"
			}
		},
		"codexAccounts": [
			{"id":"pool-1","email":"pool@example.com","plan":"plus"}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := codexauth.NewManagedCredentialStore(home)
	if err != nil {
		t.Fatal(err)
	}
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"x.eyJleHAiOjF9.y","account_id":"main-chat"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	main, err := codexauth.NewMainCredentialSource(codexHome)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
		CodexAccounts: &CodexAccountRuntime{
			Store: store, Main: main, Reauth: codexauth.NewReauthState(), Now: func() time.Time { return now },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	lifecycle, codes := readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "attention" || !containsWorkspaceIssue(codes, "credential_reauth_required") {
		t.Fatalf("expired sole OAuth credential must require reauth: lifecycle=%q issues=%v", lifecycle, codes)
	}
	if containsWorkspaceIssue(codes, "credential_missing") {
		t.Fatalf("expired credential exists physically and must not be mislabeled missing: issues=%v", codes)
	}

	validatedAt := now.UnixMilli()
	if err := store.Put(context.Background(), "pool-1", codexauth.ManagedCredential{
		AccessToken: "pool-at", RefreshToken: "pool-rt", ExpiresAtMS: validatedAt + 3600000, ChatGPTAccountID: "pool-chat",
	}, &validatedAt); err != nil {
		t.Fatal(err)
	}
	lifecycle, codes = readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("usable pooled alternate must absorb expired main credential: lifecycle=%q issues=%v", lifecycle, codes)
	}
}
