package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogsAPIRequiresLoopbackAndCapturesDataPlane(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"p": providerFunc(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}

	models := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	models.Header.Set("Authorization", "Bearer local-secret")
	modelsRR := httptest.NewRecorder()
	h.ServeHTTP(modelsRR, models)

	logs := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	logs.Host = "127.0.0.1"
	logsRR := httptest.NewRecorder()
	h.ServeHTTP(logsRR, logs)
	if logsRR.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", logsRR.Code, logsRR.Body.String())
	}
	if strings.Contains(logsRR.Body.String(), "local-secret") {
		t.Fatalf("secret leaked: %s", logsRR.Body.String())
	}
	var entries []requestLogEntry
	if err := json.Unmarshal(logsRR.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Path == "/v1/models" && entry.Method == http.MethodGet {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing models log: %s", logsRR.Body.String())
	}
}

func TestSystemMemoryAPIRequiresLoopback(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"p": providerFunc(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/system/memory", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/system/memory", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "heapAlloc") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}
