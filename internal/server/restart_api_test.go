package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/codexappserver"
)

func TestCodexAppServerAPIsAreLoopbackAndOmitSecrets(t *testing.T) {
	t.Cleanup(codexappserver.ResetRestartInFlightForTests)
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	secretLine := `codex app-server --api-key sk-secret-marker --home /Users/private-marker`
	started := int64(1)
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.5"}},
		CodexHome:      codexHome,
		CodexAppServer: codexappserver.ServiceIO{
			Process: codexappserver.IO{
				ListSnapshots: func() []codexappserver.Snapshot {
					return []codexappserver.Snapshot{{PID: 4242, CommandLine: secretLine}}
				},
				ReadStartMs: func(int) *int64 { return &started },
				CatalogMtimeMs: func() *int64 {
					mtime := int64(10)
					return &mtime
				},
				Kill:     func(int) error { return nil },
				IsAlive:  func(int) bool { return false },
				WaitExit: func(int, int) bool { return true },
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	blockedGet := httptest.NewRequest(http.MethodGet, "/api/system/codex-app-server", nil)
	blockedGet.Header.Set("Authorization", "Bearer local-secret")
	blockedGetRR := httptest.NewRecorder()
	h.ServeHTTP(blockedGetRR, blockedGet)
	if blockedGetRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane GET status=%d body=%s", blockedGetRR.Code, blockedGetRR.Body.String())
	}

	blockedPost := httptest.NewRequest(http.MethodPost, "/api/system/codex-restart", nil)
	blockedPost.Header.Set("Authorization", "Bearer local-secret")
	blockedPostRR := httptest.NewRecorder()
	h.ServeHTTP(blockedPostRR, blockedPost)
	if blockedPostRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane POST status=%d body=%s", blockedPostRR.Code, blockedPostRR.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/api/system/codex-app-server", nil)
	get.Host = "127.0.0.1:18080"
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, get)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	var state codexappserver.StateResponse
	if err := json.Unmarshal(getRR.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state.State != codexappserver.StateStale || state.RunningCount != 1 {
		t.Fatalf("state=%+v body=%s", state, getRR.Body.String())
	}
	assertNoAppServerSecrets(t, getRR.Body.String())

	post := httptest.NewRequest(http.MethodPost, "/api/system/codex-restart", nil)
	post.Host = "127.0.0.1:18080"
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, post)
	if postRR.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", postRR.Code, postRR.Body.String())
	}
	var result codexappserver.RestartResponse
	if err := json.Unmarshal(postRR.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.Code != codexappserver.CodeStopped || len(result.Stopped) != 1 || result.Stopped[0] != 4242 {
		t.Fatalf("restart=%+v body=%s", result, postRR.Body.String())
	}
	assertNoAppServerSecrets(t, postRR.Body.String())
	raw, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "http://127.0.0.1:18080/v1") {
		t.Fatalf("config=%s", raw)
	}
}

func TestCodexRestartReportsNothingRunningWithoutSignalling(t *testing.T) {
	t.Cleanup(codexappserver.ResetRestartInFlightForTests)
	codexHome := t.TempDir()
	killed := false
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CodexHome:      codexHome,
		CodexAppServer: codexappserver.ServiceIO{
			Process: codexappserver.IO{
				ListSnapshots: func() []codexappserver.Snapshot { return nil },
				Kill: func(int) error {
					killed = true
					return nil
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/system/codex-restart", nil)
	req.Host = "127.0.0.1:18080"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if killed {
		t.Fatal("signalled a process when none were listed")
	}
	if !strings.Contains(rr.Body.String(), `"code":"nothing_running"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestCodexRestartWritesSanitizedRoutedCatalog(t *testing.T) {
	t.Cleanup(codexappserver.ResetRestartInFlightForTests)
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "models_cache.json"), []byte(`{"models":[{"slug":"gpt-5.6-sol","base_instructions":"You are a helpful coding assistant.","available_in_plans":["plus"]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "google/gemini-2.5-pro"}},
		CodexHome:      codexHome,
		CodexAppServer: codexappserver.ServiceIO{
			Process: codexappserver.IO{
				ListSnapshots: func() []codexappserver.Snapshot { return nil },
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/system/codex-restart", nil)
	req.Host = "127.0.0.1:18080"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw, err := os.ReadFile(filepath.Join(codexHome, "benes-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Models []map[string]any `json:"models"`
	}
	if json.Unmarshal(raw, &file) != nil {
		t.Fatalf("catalog=%s", raw)
	}
	var routed map[string]any
	for _, model := range file.Models {
		if slug, _ := model["slug"].(string); slug == "google/gemini-2.5-pro" {
			routed = model
			break
		}
	}
	if routed == nil {
		t.Fatalf("routed missing: %s", raw)
	}
	if routed["supported_in_api"] != true {
		t.Fatalf("supported_in_api=%v", routed["supported_in_api"])
	}
	if _, ok := routed["available_in_plans"]; ok {
		t.Fatal("restart catalog leaked native plan gating onto a routed row")
	}
}

func assertNoAppServerSecrets(t *testing.T, body string) {
	t.Helper()
	for _, token := range []string{"sk-secret-marker", "private-marker", "app-server", "EPERM", "/Users/"} {
		if strings.Contains(body, token) {
			t.Fatalf("response leaked %q: %s", token, body)
		}
	}
}
