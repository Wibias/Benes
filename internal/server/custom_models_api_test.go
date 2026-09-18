package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomModelsAPICreatesWithoutLeakingSecrets(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"apiKeys":[{"key":"sk-secret"}],"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/custom-models", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	post := httptest.NewRequest(http.MethodPost, "/api/custom-models", strings.NewReader(`{"provider":"openai-apikey","modelId":"gpt-local","displayName":"Local"}`))
	post.Host = "127.0.0.1"
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, post)
	if postRR.Code != http.StatusCreated || strings.Contains(postRR.Body.String(), "sk-secret") {
		t.Fatalf("status=%d body=%s", postRR.Code, postRR.Body.String())
	}
	var created customModelRecord
	if err := json.Unmarshal(postRR.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.ModelID != "gpt-local" {
		t.Fatalf("%+v", created)
	}
	del := httptest.NewRequest(http.MethodDelete, "/api/custom-models/"+created.ID, nil)
	del.Host = "127.0.0.1"
	delRR := httptest.NewRecorder()
	h.ServeHTTP(delRR, del)
	if delRR.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", delRR.Code, delRR.Body.String())
	}
}
