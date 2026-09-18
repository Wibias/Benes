package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/nativemain"
)

func TestNativeMainProfilesListIsLoopbackOnly(t *testing.T) {
	home := t.TempDir()
	codex := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
		CodexHome:      codex,
		NativeMainKeys: nativemain.NewMemoryKeyProvider(),
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/native-main-profiles", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("catalog token should not list native profiles: %d", blockedRR.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/native-main-profiles", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"profiles"`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var listed map[string]any
	if json.Unmarshal(rr.Body.Bytes(), &listed) != nil {
		t.Fatal(rr.Body.String())
	}
}
