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

	"github.com/Wibias/Benes/internal/transport"
)

func TestRequestPacingAPIIncludesRuntimeTelemetry(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-responses","baseUrl":"https://api.openai.com/v1","authMode":"key","requestPacing":{"enabled":true,"minIntervalMs":1000}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	clock := newFakePacingClock(time.Unix(1_700_000_000, 0))
	pacer := transport.NewPacer(time.Second, clock)
	if err := pacer.AcquireWith(context.Background(), "openai-apikey", time.Second, "gpt-5"); err != nil {
		t.Fatal(err)
	}
	base, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	base = attachHandlerClose(t, base)
	h := WithProviderPacingRuntime(base, map[string]*transport.Pacer{"openai-apikey": pacer}, configPath)
	req := httptest.NewRequest(http.MethodGet, "/api/provider-request-pacing?name=openai-apikey", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Runtime map[string]struct {
			CurrentQueue    int    `json:"currentQueue"`
			UntilNextSlotMs int64  `json:"untilNextSlotMs"`
			LastModel       string `json:"lastModel"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body.Runtime["openai-apikey"]
	if got.CurrentQueue != 0 || got.UntilNextSlotMs != 1000 || got.LastModel != "gpt-5" {
		t.Fatalf("runtime=%#v", got)
	}
}

type fakePacingClock struct{ now time.Time }

func newFakePacingClock(now time.Time) *fakePacingClock { return &fakePacingClock{now: now} }
func (c *fakePacingClock) Now() time.Time               { return c.now }
func (c *fakePacingClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	c.now = c.now.Add(d)
	ch <- c.now
	return ch
}
