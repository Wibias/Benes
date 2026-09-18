package bootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestBuildDataPlanePassesPersistedCORSOriginsToAdmission(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()

	origins := []string{"https://dashboard.example", "chrome-extension://abc"}
	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw:              json.RawMessage(`{"hostname":"127.0.0.1","port":23100}`),
		CORSAllowOrigins: origins,
		Providers:        map[string]json.RawMessage{"local": localResponsesProvider(upstream)},
	}, DataPlaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !plane.bindReady {
		t.Fatal("plane not bind-ready")
	}

	body := `{"model":"local/gpt-4o","store":false,"stream":false,"input":"hi"}`
	allowed := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	allowed.Host = "127.0.0.1:23100"
	allowed.Header.Set("Origin", "https://dashboard.example/path")
	allowedRR := httptest.NewRecorder()
	plane.Handler.ServeHTTP(allowedRR, allowed)
	if allowedRR.Code != http.StatusOK {
		t.Fatalf("configured origin status=%d body=%s", allowedRR.Code, allowedRR.Body.String())
	}

	foreign := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	foreign.Host = "127.0.0.1:23100"
	foreign.Header.Set("Origin", "https://foreign.example")
	foreignRR := httptest.NewRecorder()
	plane.Handler.ServeHTTP(foreignRR, foreign)
	if foreignRR.Code != http.StatusForbidden {
		t.Fatalf("foreign origin status=%d body=%s", foreignRR.Code, foreignRR.Body.String())
	}

	origins[0] = "https://mutated.example"
	mutated := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	mutated.Host = "127.0.0.1:23100"
	mutated.Header.Set("Origin", "https://mutated.example")
	mutatedRR := httptest.NewRecorder()
	plane.Handler.ServeHTTP(mutatedRR, mutated)
	if mutatedRR.Code != http.StatusForbidden {
		t.Fatalf("post-build mutation changed admission: status=%d body=%s", mutatedRR.Code, mutatedRR.Body.String())
	}
}
