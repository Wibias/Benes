package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/credentials"
)

func TestProvidersWorkspaceOpenAIPoolRequiresWholePoolReauth(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward",
				"codexAccountMode": "pool"
			}
		},
		"codexAccounts": [
			{"id":"pool-1","email":"pool@example.com","plan":"plus"}
		],
		"activeCodexAccountId": "__main__",
		"activeCodexAccountPinned": "__main__"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := codexauth.NewManagedCredentialStore(home)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	validatedAt := now.UnixMilli()
	if err := store.Put(context.Background(), "pool-1", codexauth.ManagedCredential{
		AccessToken: "pool-at", RefreshToken: "pool-rt", ExpiresAtMS: validatedAt + 3600000, ChatGPTAccountID: "pool-chat",
	}, &validatedAt); err != nil {
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
	reauth := codexauth.NewReauthState()
	reauth.Mark(codexauth.MainAccountID, 1)

	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
		CodexAccounts: &CodexAccountRuntime{
			Store: store, Main: main, Reauth: reauth, Now: func() time.Time { return now },
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	lifecycle, codes := readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("one usable pool credential must absorb main reauth: lifecycle=%q issues=%v", lifecycle, codes)
	}

	reauth.Mark("pool-1", 1)
	lifecycle, codes = readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "attention" || !containsWorkspaceIssue(codes, "credential_reauth_required") {
		t.Fatalf("all pool credentials needing reauth must need attention: lifecycle=%q issues=%v", lifecycle, codes)
	}
}

func TestProvidersWorkspaceOpenAIPoolAllCredentialsPausedNeedAttention(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	pausedConfig := `{
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
		],
		"pausedCodexAccountIds": ["pool-1"]
	}`
	if err := os.WriteFile(configPath, []byte(pausedConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := codexauth.NewManagedCredentialStore(home)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	validatedAt := now.UnixMilli()
	if err := store.Put(context.Background(), "pool-1", codexauth.ManagedCredential{
		AccessToken: "pool-at", RefreshToken: "pool-rt", ExpiresAtMS: validatedAt + 3600000, ChatGPTAccountID: "pool-chat",
	}, &validatedAt); err != nil {
		t.Fatal(err)
	}
	main, err := codexauth.NewMainCredentialSource(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
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
	if lifecycle != "attention" || !containsWorkspaceIssue(codes, "credential_pool_paused") {
		t.Fatalf("fully paused OAuth pool must need attention: lifecycle=%q issues=%v", lifecycle, codes)
	}
	if containsWorkspaceIssue(codes, "credential_missing") {
		t.Fatalf("paused physical credentials must not be mislabeled missing: issues=%v", codes)
	}

	unpausedConfig := `{
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
		],
		"pausedCodexAccountIds": []
	}`
	if err := os.WriteFile(configPath, []byte(unpausedConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	lifecycle, codes = readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("unpausing a usable OAuth credential must restore healthy lifecycle: lifecycle=%q issues=%v", lifecycle, codes)
	}
}

func TestProvidersWorkspaceOpenAIDirectDoesNotRequireStoredOAuthCredential(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward",
				"codexAccountMode": "direct",
				"defaultAccess": "oauth"
			}
		},
		"codexAccounts": [
			{"id":"pool-1","email":"pool@example.com","plan":"plus"}
		],
		"codexAccountNamespaces": {"work":"pool-1"}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := codexauth.NewManagedCredentialStore(home)
	if err != nil {
		t.Fatal(err)
	}
	main, err := codexauth.NewMainCredentialSource(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reauth := codexauth.NewReauthState()
	reauth.Mark(codexauth.MainAccountID, 1)

	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
		CodexAccounts: &CodexAccountRuntime{
			Store: store, Main: main, Reauth: reauth,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	lifecycle, codes := readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("direct mode uses caller auth and must ignore stored OAuth availability/reauth: lifecycle=%q issues=%v", lifecycle, codes)
	}
}

func TestProvidersWorkspaceOpenAIDefaultOAuthRequiresPhysicalCredential(t *testing.T) {
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
	main, err := codexauth.NewMainCredentialSource(t.TempDir())
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
	if lifecycle != "attention" || !containsWorkspaceIssue(codes, "credential_missing") {
		t.Fatalf("OAuth default without a physical credential must need setup: lifecycle=%q issues=%v", lifecycle, codes)
	}

	validatedAt := now.UnixMilli()
	if err := store.Put(context.Background(), "pool-1", codexauth.ManagedCredential{
		AccessToken: "pool-at", RefreshToken: "pool-rt", ExpiresAtMS: validatedAt + 3600000, ChatGPTAccountID: "pool-chat",
	}, &validatedAt); err != nil {
		t.Fatal(err)
	}
	lifecycle, codes = readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("usable OAuth pool credential must clear missing-credential attention: lifecycle=%q issues=%v", lifecycle, codes)
	}
}

func TestProvidersWorkspaceOpenAIDefaultAPIRequiresResolvableCredentialRef(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward",
				"codexAccountMode": "pool",
				"defaultAccess": "api"
			},
			"openai-apikey": {
				"adapter": "openai-responses",
				"baseUrl": "https://api.openai.com/v1",
				"authMode": "key",
				"credentialRef": {"id":"api-active","source":"secure-store"}
			}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	keyStore, err := credentials.NewFileStore(filepath.Join(home, "credentials"), true)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{
			"openai":        providerFunc(nil),
			"openai-apikey": providerFunc(nil),
		},
		ConfigPath:  configPath,
		Credentials: keyStore,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	lifecycle, codes := readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "attention" || !containsWorkspaceIssue(codes, "credential_missing") {
		t.Fatalf("API default with dangling credential ref must need setup: lifecycle=%q issues=%v", lifecycle, codes)
	}

	if _, err := keyStore.Put("api-active", []byte("sk-test")); err != nil {
		t.Fatal(err)
	}
	lifecycle, codes = readOpenAIWorkspaceLifecycle(t, h)
	if lifecycle != "healthy" || len(codes) != 0 {
		t.Fatalf("resolvable API default credential must clear missing-credential attention: lifecycle=%q issues=%v", lifecycle, codes)
	}
}

func readOpenAIWorkspaceLifecycle(t *testing.T, h http.Handler) (string, []string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/providers/workspace", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Providers []struct {
			ID        string `json:"id"`
			Lifecycle string `json:"lifecycle"`
		} `json:"providers"`
		Attention []struct {
			Provider string `json:"provider"`
			Code     string `json:"code"`
		} `json:"attention"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	lifecycle := ""
	for _, provider := range body.Providers {
		if provider.ID == "openai" {
			lifecycle = provider.Lifecycle
			break
		}
	}
	codes := make([]string, 0, len(body.Attention))
	for _, issue := range body.Attention {
		if issue.Provider == "openai" {
			codes = append(codes, issue.Code)
		}
	}
	return lifecycle, codes
}
