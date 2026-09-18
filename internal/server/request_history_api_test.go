package server

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/requesthistory"
	"github.com/Wibias/Benes/internal/timeline"

	_ "modernc.org/sqlite"
)

func TestRequestHistoryAPIListsPersistedIDs(t *testing.T) {
	dir := t.TempDir()
	store := timeline.NewStore(dir, 8)
	tr := timeline.New("req_1", 8)
	tr.Mark(timeline.StagePreDispatch, timeline.SideLocal, timeline.MilestoneDispatch, true, "")
	if err := store.Save(tr); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		Timeline:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/request-history", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/request-history", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "req_1") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRequestHistoryAPILooksUpTimeline(t *testing.T) {
	dir := t.TempDir()
	store := timeline.NewStore(dir, 8)
	tr := timeline.New("req_1", 8)
	tr.Mark(timeline.StagePreDispatch, timeline.SideLocal, timeline.MilestoneDispatch, true, "")
	if err := store.Save(tr); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		Timeline:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/request-history?id=req_1", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"stage":"pre_dispatch"`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRequestHistoryRouteDecisionLooksUpUsageIndex(t *testing.T) {
	home := t.TempDir()
	line := `{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"direct"}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := requesthistory.CatchUp(home); err != nil {
		t.Fatal(err)
	}
	h := testHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     filepath.Join(home, "config.json"),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/request-history/req_1/route-decision", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"routeKind":"direct"`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRequestHistoryIndexFailureDoesNotFailRequest(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	h := testHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": exactGPT56Provider()},
		UsageLogPath:   usagePath,
		ConfigPath:     filepath.Join(home, "config.json"),
	})
	requesthistory.SetBeforeCatchUpTestHook(func() error {
		return errors.New("index boom")
	})
	defer requesthistory.SetBeforeCatchUpTestHook(nil)
	postUsage(t, h, "thread-ok", "admitted-ok", "")
	raw := readUsageLog(t, usagePath)
	if !strings.Contains(raw, `"requestId":`) {
		t.Fatalf("ledger row missing after index failure: %s", raw)
	}
	requesthistory.SetBeforeCatchUpTestHook(nil)
	if _, err := requesthistory.CatchUp(home); err != nil {
		t.Fatal(err)
	}
	rows := parseUsageRows(t, usagePath)
	if len(rows) != 1 {
		t.Fatalf("rows=%d body=%s", len(rows), raw)
	}
	id := requireBenesRequestID(t, rows[0])
	req := httptest.NewRequest(http.MethodGet, "/api/request-history/"+id+"/route-decision", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRequestHistoryRebuildRequiredIsUnavailable(t *testing.T) {
	home := t.TempDir()
	line := `{"requestId":"req_old","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"direct"}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := requesthistory.CatchUp(home); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, "routing-history.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO schema_meta(key, value) VALUES ('indexed_offset', '999'), ('rebuild_required', '1')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	h := testHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     filepath.Join(home, "config.json"),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/request-history/req_old/route-decision", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "index_unavailable") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "routing-history.sqlite") {
		t.Fatalf("leaked path: %s", rr.Body.String())
	}
}

func TestRequestHistoryHealthyMissIsNotFound(t *testing.T) {
	home := t.TempDir()
	if _, err := requesthistory.CatchUp(home); err != nil {
		t.Fatal(err)
	}
	h := testHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     filepath.Join(home, "config.json"),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/request-history/req_missing/route-decision", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRequestHistorySyncFailureDoesNotProveAbsence(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_hist","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"direct"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	requesthistory.SetBeforeCatchUpTestHook(func() error {
		return errors.New("index boom")
	})
	defer requesthistory.SetBeforeCatchUpTestHook(nil)
	h := testHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": exactGPT56Provider()},
		UsageLogPath:   filepath.Join(home, "usage.jsonl"),
		ConfigPath:     filepath.Join(home, "config.json"),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/request-history/req_missing/route-decision", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "index_unavailable") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestRequestHistoryLookupWhileCatchUpPendingIsUnavailable(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_hist","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"direct"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	requesthistory.SetHoldCatchUpTestHook(func() {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
	})
	defer func() {
		requesthistory.SetHoldCatchUpTestHook(nil)
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	h := testHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     filepath.Join(home, "config.json"),
	})
	pending := httptest.NewRequest(http.MethodGet, "/api/request-history/req_missing/route-decision", nil)
	pending.Host = "127.0.0.1"
	pendingRR := httptest.NewRecorder()
	h.ServeHTTP(pendingRR, pending)
	if pendingRR.Code != http.StatusServiceUnavailable || !strings.Contains(pendingRR.Body.String(), "index_unavailable") {
		t.Fatalf("pending status=%d body=%s", pendingRR.Code, pendingRR.Body.String())
	}
	<-started
	close(release)
	requesthistory.SetHoldCatchUpTestHook(nil)
	if _, err := requesthistory.CatchUp(home); err != nil {
		t.Fatal(err)
	}
	hit := httptest.NewRequest(http.MethodGet, "/api/request-history/req_hist/route-decision", nil)
	hit.Host = "127.0.0.1"
	hitRR := httptest.NewRecorder()
	h.ServeHTTP(hitRR, hit)
	if hitRR.Code != http.StatusOK || !strings.Contains(hitRR.Body.String(), `"routeKind":"direct"`) {
		t.Fatalf("hit status=%d body=%s", hitRR.Code, hitRR.Body.String())
	}
	miss := httptest.NewRequest(http.MethodGet, "/api/request-history/req_missing/route-decision", nil)
	miss.Host = "127.0.0.1"
	missRR := httptest.NewRecorder()
	h.ServeHTTP(missRR, miss)
	if missRR.Code != http.StatusNotFound {
		t.Fatalf("miss status=%d body=%s", missRR.Code, missRR.Body.String())
	}
}

func TestAdmittedRequestReturnsWhileCatchUpBlocked(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	started := make(chan struct{})
	release := make(chan struct{})
	requesthistory.SetHoldCatchUpTestHook(func() {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
	})
	defer func() {
		requesthistory.SetHoldCatchUpTestHook(nil)
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	h := testHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": exactGPT56Provider()},
		UsageLogPath:   usagePath,
		ConfigPath:     filepath.Join(home, "config.json"),
	})
	<-started
	postUsage(t, h, "thread-async", "admitted-async", "")
	raw := readUsageLog(t, usagePath)
	if !strings.Contains(raw, `"requestId":`) {
		t.Fatalf("ledger row missing while catch-up blocked: %s", raw)
	}
	rows := parseUsageRows(t, usagePath)
	if len(rows) != 1 {
		t.Fatalf("rows=%d body=%s", len(rows), raw)
	}
	id := requireBenesRequestID(t, rows[0])
	pending := httptest.NewRequest(http.MethodGet, "/api/request-history/"+id+"/route-decision", nil)
	pending.Host = "127.0.0.1"
	pendingRR := httptest.NewRecorder()
	h.ServeHTTP(pendingRR, pending)
	if pendingRR.Code != http.StatusServiceUnavailable || !strings.Contains(pendingRR.Body.String(), "index_unavailable") {
		t.Fatalf("pending status=%d body=%s", pendingRR.Code, pendingRR.Body.String())
	}
	close(release)
	requesthistory.SetHoldCatchUpTestHook(nil)
	if _, err := requesthistory.CatchUp(home); err != nil {
		t.Fatal(err)
	}
	hit := httptest.NewRequest(http.MethodGet, "/api/request-history/"+id+"/route-decision", nil)
	hit.Host = "127.0.0.1"
	hitRR := httptest.NewRecorder()
	h.ServeHTTP(hitRR, hit)
	if hitRR.Code != http.StatusOK {
		t.Fatalf("hit status=%d body=%s", hitRR.Code, hitRR.Body.String())
	}
}
