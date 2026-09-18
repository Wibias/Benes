package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/usageledger"
)

func TestUsageRetentionAPIPreviewRunLoopback(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, "config.json")
	if err := os.WriteFile(cfg, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := usageledger.OpenWithTarget(home, 64)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		row := map[string]any{
			"timestamp": int64(1000 + i),
			"requestId": "req_" + strconv.Itoa(i),
			"provider":  "openai",
			"model":     "gpt-5",
			"status":    200,
			"pad":       strings.Repeat("x", 40),
		}
		raw, _ := json.Marshal(row)
		if err := l.Append(raw); err != nil {
			t.Fatal(err)
		}
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     cfg,
		UsageLogPath:   filepath.Join(home, "usage.jsonl"),
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	blocked := httptest.NewRequest(http.MethodGet, "/api/usage/retention", nil)
	blocked.Host = "example.com"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, blocked)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("non-loopback status=%d body=%s", rr.Code, rr.Body.String())
	}

	getRR := storageLoopbackJSON(t, h, http.MethodGet, "/api/usage/retention", nil)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get %d %s", getRR.Code, getRR.Body.String())
	}
	putRR := storageLoopbackJSON(t, h, http.MethodPut, "/api/usage/retention", map[string]any{"maxBytes": 120, "maxAgeMs": 0})
	if putRR.Code != http.StatusOK {
		t.Fatalf("put %d %s", putRR.Code, putRR.Body.String())
	}
	prevRR := storageLoopbackJSON(t, h, http.MethodPost, "/api/usage/retention/preview", map[string]any{})
	if prevRR.Code != http.StatusOK {
		t.Fatalf("preview %d %s", prevRR.Code, prevRR.Body.String())
	}
	var prev usageledger.RetentionPreview
	if err := json.Unmarshal(prevRR.Body.Bytes(), &prev); err != nil {
		t.Fatal(err)
	}
	runRR := storageLoopbackJSON(t, h, http.MethodPost, "/api/usage/retention/run", map[string]any{"digest": prev.Digest})
	if runRR.Code != http.StatusOK {
		t.Fatalf("run %d %s", runRR.Code, runRR.Body.String())
	}
}
