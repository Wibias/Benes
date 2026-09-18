package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
)

func TestProvidersWorkspaceOpenAIPoolInvalidManagedStoreNeedsAttention(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(home, "codex-accounts.json"), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}

	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"main-at","account_id":"main-chat"}}`), 0o600); err != nil {
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
	if lifecycle != "attention" || !containsWorkspaceIssue(codes, "credential_store_unavailable") {
		t.Fatalf("invalid managed OAuth store must need attention even when Main is valid: lifecycle=%q issues=%v", lifecycle, codes)
	}
	if containsWorkspaceIssue(codes, "credential_missing") || containsWorkspaceIssue(codes, "credential_reauth_required") {
		t.Fatalf("store corruption must not be mislabeled as missing credential or reauth: issues=%v", codes)
	}

	if err := os.WriteFile(filepath.Join(home, "codex-accounts.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle, codes = readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("repairing the managed store must let the valid Main credential restore healthy lifecycle: lifecycle=%q issues=%v", lifecycle, codes)
	}
}
