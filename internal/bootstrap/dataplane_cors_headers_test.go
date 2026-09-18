package bootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestBuildDataPlaneUsesProjectedListenerPortForCORSFallback(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()

	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw:       json.RawMessage(`{"hostname":"127.0.0.1","port":20200}`),
		Providers: map[string]json.RawMessage{"local": localResponsesProvider(upstream)},
	}, DataPlaneOptions{})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodOptions, "/v1/responses", nil)
	req.Host = "127.0.0.1:20200"
	req.Header.Set("Origin", "https://attacker.example")
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:20200" {
		t.Fatalf("fallback origin=%q", got)
	}
}
