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

func TestContextProjectionAPIReadsAndWritesMode(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
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
	get := httptest.NewRequest(http.MethodGet, "/api/context-projection", nil)
	get.Host = "127.0.0.1"
	got := httptest.NewRecorder()
	h.ServeHTTP(got, get)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"mode":"off"`) {
		t.Fatalf("get status=%d body=%s", got.Code, got.Body.String())
	}
	put := httptest.NewRequest(http.MethodPut, "/api/context-projection", strings.NewReader(`{"mode":"recovery"}`))
	put.Host = "127.0.0.1"
	put.Header.Set("Content-Type", "application/json")
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	again := httptest.NewRequest(http.MethodGet, "/api/context-projection", nil)
	again.Host = "127.0.0.1"
	againRR := httptest.NewRecorder()
	h.ServeHTTP(againRR, again)
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(againRR.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Mode != "recovery" {
		t.Fatalf("mode=%q body=%s", body.Mode, againRR.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"mode": "recovery"`) && !strings.Contains(string(raw), `"mode":"recovery"`) {
		t.Fatalf("config=%s", raw)
	}
}

func TestContextProjectionAPIRejectsUnknownMode(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{}`), 0o600); err != nil {
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
	put := httptest.NewRequest(http.MethodPut, "/api/context-projection", strings.NewReader(`{"mode":"lab"}`))
	put.Host = "127.0.0.1"
	put.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, put)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
