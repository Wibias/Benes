package bootstrap

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/sessions"
)

func TestBuildDataPlanePersistsRequestTimelineForDataPlaneLookup(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()
	home := t.TempDir()
	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw: json.RawMessage(`{"hostname":"127.0.0.1","port":23100}`),
		Providers: map[string]json.RawMessage{
			"local": localResponsesProvider(upstream),
		},
	}, DataPlaneOptions{CodexPool: CodexPoolOptions{BenesHome: home}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plane.Close() })

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"local/gpt-4o","store":false,"stream":true,"input":"hi"}`))
	req.Host = "127.0.0.1:23100"
	req.Header.Set("X-Request-ID", "req-live-timeline")
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	lookup := httptest.NewRequest(http.MethodGet, "/v1/request-timeline?id=req-live-timeline", nil)
	lookup.Host = "127.0.0.1:23100"
	got := httptest.NewRecorder()
	plane.Handler.ServeHTTP(got, lookup)
	if got.Code != http.StatusOK {
		t.Fatalf("lookup status=%d body=%s", got.Code, got.Body.String())
	}
	var body struct {
		ID     string           `json:"id"`
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ID != "req-live-timeline" || len(body.Events) == 0 {
		t.Fatalf("body=%#v", body)
	}
}

func TestBuildDataPlanePersistsSessionsAcrossRestart(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()
	home := t.TempDir()
	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw: json.RawMessage(`{"hostname":"127.0.0.1","port":23100}`),
		Providers: map[string]json.RawMessage{
			"local": localResponsesProvider(upstream),
		},
	}, DataPlaneOptions{CodexPool: CodexPoolOptions{BenesHome: home}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plane.Close() })
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"local/gpt-4o","store":false,"stream":true,"input":"hi"}`))
	req.Host = "127.0.0.1:23100"
	req.Header.Set("thread-id", "thread-bootstrap")
	req.Header.Set("X-Request-ID", "req-session-1")
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if err := plane.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := sessions.Open(sessions.FilePath(home))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	listed, err := store.List(sessions.ListOptions{})
	if err != nil || len(listed.Sessions) != 1 {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
	if listed.Sessions[0].Namespace != sessions.NamespaceCodexThread || listed.Sessions[0].ExternalID != "thread-bootstrap" || listed.Sessions[0].RequestCount != 1 {
		t.Fatalf("summary=%#v", listed.Sessions[0])
	}
	detail, err := store.Get(listed.Sessions[0].ID, sessions.DetailOptions{})
	if err != nil || len(detail.Requests) != 1 || detail.Requests[0].CorrelationID != "req-session-1" || !strings.HasPrefix(detail.Requests[0].ID, "req_") || detail.Requests[0].ID == "req-session-1" {
		t.Fatalf("detail=%#v err=%v", detail, err)
	}
}

func TestBuildDataPlaneContinuesWhenSessionStoreUnavailable(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "sessions.sqlite"), 0o700); err != nil {
		t.Fatal(err)
	}
	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw: json.RawMessage(`{"hostname":"127.0.0.1","port":23100}`),
		Providers: map[string]json.RawMessage{
			"local": localResponsesProvider(upstream),
		},
	}, DataPlaneOptions{CodexPool: CodexPoolOptions{BenesHome: home}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plane.Close() })
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"local/gpt-4o","store":false,"stream":true,"input":"hi"}`))
	req.Host = "127.0.0.1:23100"
	req.Header.Set("X-Request-ID", "req-store-unavailable")
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("model status=%d body=%s", rr.Code, rr.Body.String())
	}
	list := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	list.Host = "127.0.0.1:23100"
	listRR := httptest.NewRecorder()
	plane.Handler.ServeHTTP(listRR, list)
	if listRR.Code != http.StatusServiceUnavailable {
		t.Fatalf("list status=%d body=%s", listRR.Code, listRR.Body.String())
	}
	if strings.Contains(listRR.Body.String(), home) || strings.Contains(strings.ToLower(listRR.Body.String()), "sqlite") {
		t.Fatalf("leaked store detail %s", listRR.Body.String())
	}
	detail := httptest.NewRequest(http.MethodGet, "/api/sessions/ses_missing", nil)
	detail.Host = "127.0.0.1:23100"
	detailRR := httptest.NewRecorder()
	plane.Handler.ServeHTTP(detailRR, detail)
	if detailRR.Code != http.StatusServiceUnavailable {
		t.Fatalf("detail status=%d body=%s", detailRR.Code, detailRR.Body.String())
	}
}

func TestBuildDataPlaneTimelineLookupFailsClosedWithoutHome(t *testing.T) {
	upstream := newResponsesUpstream(t)
	defer upstream.Close()
	plane, err := BuildDataPlane(t.Context(), config.DiskConfig{
		Raw: json.RawMessage(`{"hostname":"127.0.0.1","port":23100}`),
		Providers: map[string]json.RawMessage{
			"local": localResponsesProvider(upstream),
		},
	}, DataPlaneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plane.Close() })
	req := httptest.NewRequest(http.MethodGet, "/v1/request-timeline?id=req-missing", nil)
	req.Host = "127.0.0.1:23100"
	rr := httptest.NewRecorder()
	plane.Handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
