package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDebugAPIRequiresLoopback(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"p": providerFunc(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/debug", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
}

func TestDebugAPIGetPutAndLogs(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"p": providerFunc(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	put := httptest.NewRequest(http.MethodPut, "/api/debug", strings.NewReader(`{"debug":true}`))
	put.Host = "127.0.0.1"
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	var view struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(putRR.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Enabled {
		t.Fatalf("view=%s", putRR.Body.String())
	}

	models := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	models.Header.Set("Authorization", "Bearer local-secret")
	modelsRR := httptest.NewRecorder()
	h.ServeHTTP(modelsRR, models)

	logs := httptest.NewRequest(http.MethodGet, "/api/debug/logs", nil)
	logs.Host = "127.0.0.1"
	logsRR := httptest.NewRecorder()
	h.ServeHTTP(logsRR, logs)
	if logsRR.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", logsRR.Code, logsRR.Body.String())
	}
	if !strings.Contains(logsRR.Body.String(), "/v1/models") {
		t.Fatalf("missing captured line: %s", logsRR.Body.String())
	}
	if strings.Contains(logsRR.Body.String(), "local-secret") {
		t.Fatalf("secret leaked: %s", logsRR.Body.String())
	}
}
