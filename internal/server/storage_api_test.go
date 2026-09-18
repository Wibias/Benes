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

func TestStorageAPIIsLoopbackAndScansHome(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "sessions", "a.jsonl"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CodexHome:      home,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/storage", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/storage", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"apiKey"`) || strings.Contains(rr.Body.String(), "sk-") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var report struct {
		CodexHome string `json:"codexHome"`
		Total     struct {
			Bytes     int64 `json:"bytes"`
			FileCount int   `json:"fileCount"`
		} `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.CodexHome != home || report.Total.FileCount != 1 || report.Total.Bytes != 5 {
		t.Fatalf("report=%+v body=%s", report, rr.Body.String())
	}
}
