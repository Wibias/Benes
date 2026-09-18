package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/sessions"
	"github.com/Wibias/Benes/internal/store/atomicfile"
	"github.com/Wibias/Benes/internal/usage"
)

func TestPanicBeforeWriteAlignsSessionsAndDiagnosticsStatus(t *testing.T) {
	h, _ := newGroupedHandler(t, providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		panic("boom-secret-should-not-leak")
	}))
	rr := postGrouped(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "panic-before-thread", "panic-before-corr")
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("http=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "boom-secret-should-not-leak") {
		t.Fatalf("panic payload leaked %s", rr.Body.String())
	}

	details := responsesDiagnostics(t, h)
	if len(details) != 1 {
		t.Fatalf("diagnostics records=%d", len(details))
	}
	detail := details[0]
	if detail.Status != http.StatusInternalServerError {
		t.Fatalf("diagnostics status=%d", detail.Status)
	}
	if detail.Failure == nil || detail.Failure.Cause != "internal_panic" || detail.ErrorCode != "internal_panic" {
		t.Fatalf("failure=%s", mustJSON(detail.Failure))
	}
	if !strings.HasPrefix(detail.RequestID, "req_") {
		t.Fatalf("requestId=%q", detail.RequestID)
	}

	req := onlySessionRequest(t, h)
	if status, _ := req["status"].(float64); status != http.StatusInternalServerError {
		t.Fatalf("sessions status=%s", mustJSON(req))
	}
	if req["id"] != detail.RequestID {
		t.Fatalf("id mismatch sessions=%s diagnostics=%s", req["id"], detail.RequestID)
	}
	if strings.Contains(mustJSON(req), "boom-secret-should-not-leak") {
		t.Fatalf("panic payload leaked into sessions %s", mustJSON(req))
	}
}

func TestPanicAfterCommittedStatusAlignsSessionsAndDiagnostics(t *testing.T) {
	h, _ := newGroupedHandler(t, providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		return &panicAfterEventStream{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}}, panicOn: 1}, nil
	}))
	rr := postGrouped(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "panic-after-thread", "panic-after-corr")
	if rr.Code != http.StatusOK {
		t.Fatalf("http=%d body=%s", rr.Code, rr.Body.String())
	}

	details := responsesDiagnostics(t, h)
	if len(details) != 1 {
		t.Fatalf("diagnostics records=%d", len(details))
	}
	detail := details[0]
	if detail.Status != http.StatusOK {
		t.Fatalf("diagnostics status=%d want committed 200", detail.Status)
	}
	if detail.Failure == nil || detail.Failure.Cause != "internal_panic" || detail.ErrorCode != "internal_panic" {
		t.Fatalf("failure=%s", mustJSON(detail.Failure))
	}

	req := onlySessionRequest(t, h)
	if status, _ := req["status"].(float64); status != http.StatusOK {
		t.Fatalf("sessions status=%s", mustJSON(req))
	}
	if req["id"] != detail.RequestID {
		t.Fatalf("id mismatch sessions=%s diagnostics=%s", req["id"], detail.RequestID)
	}
}

func TestGroupedNonPanicRequestKeepsSessionsAndDiagnosticsStatus200(t *testing.T) {
	h, _ := newGroupedHandler(t, &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	}})
	rr := postGrouped(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "ok-thread", "ok-corr")
	if rr.Code != http.StatusOK {
		t.Fatalf("http=%d body=%s", rr.Code, rr.Body.String())
	}

	details := responsesDiagnostics(t, h)
	if len(details) != 1 {
		t.Fatalf("diagnostics records=%d", len(details))
	}
	detail := details[0]
	if detail.Status != http.StatusOK {
		t.Fatalf("diagnostics status=%d", detail.Status)
	}
	if detail.Failure != nil || detail.ErrorCode == "internal_panic" {
		t.Fatalf("unexpected panic attribution %s", mustJSON(detail.Failure))
	}

	req := onlySessionRequest(t, h)
	if status, _ := req["status"].(float64); status != http.StatusOK {
		t.Fatalf("sessions status=%s", mustJSON(req))
	}
	if req["id"] != detail.RequestID {
		t.Fatalf("id mismatch sessions=%s diagnostics=%s", req["id"], detail.RequestID)
	}
}

func TestDiagnosticsSkipsPriceOverlayLoadWhenUsageMissing(t *testing.T) {
	h, _ := newGroupedHandler(t, &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	}})
	loads := installCountingOverlays(h, operatorGPT56Overlay(1, 2))
	postGroupedOK(t, h, "no-usage-thread", "no-usage-corr")
	if loads.Load() != 0 {
		t.Fatalf("overlay loads=%d want 0", loads.Load())
	}
	detail := latestResponsesDetail(t, h)
	if detail.Usage == nil || detail.Usage.Status != "unreported" {
		t.Fatalf("usage=%s", mustJSON(detail.Usage))
	}
	if detail.Cost != nil {
		t.Fatalf("cost=%s", mustJSON(detail.Cost))
	}
	req := onlySessionRequest(t, h)
	if _, ok := req["usage"]; ok {
		t.Fatalf("sessions stored usage %s", mustJSON(req))
	}
}

func TestGroupedExactUsageSharesOnePriceOverlayLoad(t *testing.T) {
	h, _ := newGroupedHandler(t, exactGPT56Provider())
	loads := installCountingOverlays(h, operatorGPT56Overlay(5, 10))
	postGroupedOK(t, h, "exact-thread", "exact-corr")
	if loads.Load() != 1 {
		t.Fatalf("overlay loads=%d want 1", loads.Load())
	}

	detail := latestResponsesDetail(t, h)
	if detail.Usage == nil || detail.Usage.Status != "reported" {
		t.Fatalf("usage=%s", mustJSON(detail.Usage))
	}
	if detail.Cost == nil || detail.Cost.Kind != "exact" || detail.Cost.Total == nil || detail.Cost.Currency != "USD" {
		t.Fatalf("diagnostics cost=%s", mustJSON(detail.Cost))
	}
	want := (1000.0*5 + 10.0*10) / 1_000_000
	if *detail.Cost.Total != want {
		t.Fatalf("diagnostics total=%v want %v", *detail.Cost.Total, want)
	}

	req := onlySessionRequest(t, h)
	usage, _ := req["usage"].(map[string]any)
	if usage["cost"] != want || usage["currency"] != "USD" {
		t.Fatalf("sessions usage=%s", mustJSON(req))
	}
}

func TestEstimatedDiagnosticsUsageDoesNotPersistSessionCost(t *testing.T) {
	h, _ := newGroupedHandler(t, &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1000, OutputTokens: 10, Estimated: true}},
	}})
	loads := installCountingOverlays(h, operatorGPT56Overlay(5, 10))
	postGroupedOK(t, h, "est-thread", "est-corr")
	if loads.Load() != 1 {
		t.Fatalf("overlay loads=%d want 1", loads.Load())
	}

	detail := latestResponsesDetail(t, h)
	if detail.Usage == nil || detail.Usage.Status != "estimated" {
		t.Fatalf("usage=%s", mustJSON(detail.Usage))
	}
	if detail.Cost == nil || detail.Cost.Kind != "estimated" || detail.Cost.Total == nil {
		t.Fatalf("diagnostics cost=%s", mustJSON(detail.Cost))
	}

	req := onlySessionRequest(t, h)
	if _, ok := req["usage"]; ok {
		t.Fatalf("sessions persisted estimated usage %s", mustJSON(req))
	}
}

func TestModelCostsUpdateIsObservedOnSubsequentRequest(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat","modelCosts":{"gpt-5.6":{"input":5,"output":10}}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": exactGPT56Provider()},
		Sessions:       store,
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	impl := h.(*handler)
	var loads atomic.Int32
	orig := impl.priceOverlayLoad
	if orig == nil {
		orig = func() []usage.PriceRecord { return loadPriceOverlaysFromDisk(impl.configPath) }
	}
	impl.priceOverlayLoad = func() []usage.PriceRecord {
		loads.Add(1)
		return orig()
	}

	postGroupedOK(t, h, "update-thread", "update-1")
	if loads.Load() != 1 {
		t.Fatalf("first request overlay loads=%d want 1", loads.Load())
	}
	first := latestResponsesDetail(t, h)
	wantFirst := (1000.0*5 + 10.0*10) / 1_000_000
	if first.Cost == nil || first.Cost.Kind != "exact" || first.Cost.Total == nil || *first.Cost.Total != wantFirst {
		t.Fatalf("first cost=%s", mustJSON(first.Cost))
	}

	// Another process publishes config.json (CLI/manual) without calling
	// this process's config.Transaction.Commit.
	if err := atomicfile.Write(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat","modelCosts":{"gpt-5.6":{"input":50,"output":100}}}}}`+"\n"), atomicfile.Options{Mode: 0o600}); err != nil {
		t.Fatal(err)
	}

	postGroupedOK(t, h, "update-thread", "update-2")
	if loads.Load() != 2 {
		t.Fatalf("after update overlay loads=%d want 2", loads.Load())
	}
	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	var second diagnosticsRequestDetail
	for _, row := range listed.Requests {
		if row.Path == "/v1/responses" && row.RequestID != first.RequestID {
			second = getDiagnosticsDetail(t, h, row.RequestID)
		}
	}
	wantSecond := (1000.0*50 + 10.0*100) / 1_000_000
	if second.Cost == nil || second.Cost.Kind != "exact" || second.Cost.Total == nil || *second.Cost.Total != wantSecond {
		t.Fatalf("second cost=%s", mustJSON(second.Cost))
	}

	detail := getJSON(t, h, "/api/sessions/"+first.SessionID)
	rows := detail["requests"].([]any)
	if len(rows) != 2 {
		t.Fatalf("session requests=%s", mustJSON(detail))
	}
	costs := []float64{}
	for _, raw := range rows {
		usage, _ := raw.(map[string]any)["usage"].(map[string]any)
		costs = append(costs, usage["cost"].(float64))
	}
	if (costs[0] != wantFirst || costs[1] != wantSecond) && (costs[0] != wantSecond || costs[1] != wantFirst) {
		t.Fatalf("session costs=%v", costs)
	}
}

func TestPriceOverlaySnapshotConcurrentRequests(t *testing.T) {
	h, _ := newGroupedHandler(t, providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "hello"},
			{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1000, OutputTokens: 10}},
		}}, nil
	}))
	loads := installCountingOverlays(h, operatorGPT56Overlay(5, 10))
	const n = 8
	var wg sync.WaitGroup
	errCh := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			thread := "conc-thread-" + string(rune('a'+i))
			corr := "conc-corr-" + string(rune('a'+i))
			rr := postGrouped(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, thread, corr)
			if rr.Code != http.StatusOK {
				errCh <- rr.Body.String()
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for msg := range errCh {
		t.Fatalf("concurrent request failed %s", msg)
	}
	if loads.Load() != n {
		t.Fatalf("overlay loads=%d want %d", loads.Load(), n)
	}
	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	want := (1000.0*5 + 10.0*10) / 1_000_000
	rows := 0
	for _, row := range listed.Requests {
		if row.Path != "/v1/responses" {
			continue
		}
		rows++
		detail := getDiagnosticsDetail(t, h, row.RequestID)
		if detail.Cost == nil || detail.Cost.Kind != "exact" || detail.Cost.Total == nil || *detail.Cost.Total != want {
			t.Fatalf("diagnostics cost=%s", mustJSON(detail.Cost))
		}
	}
	if rows != n {
		t.Fatalf("diagnostics rows=%d want %d", rows, n)
	}
}

func newGroupedHandler(t *testing.T, provider Provider) (http.Handler, *sessions.Store) {
	t.Helper()
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	return h, store
}

func postGrouped(t *testing.T, h http.Handler, body, threadID, correlation string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("thread-id", threadID)
	req.Header.Set("X-Request-ID", correlation)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func postGroupedOK(t *testing.T, h http.Handler, threadID, correlation string) {
	t.Helper()
	rr := postGrouped(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, threadID, correlation)
	if rr.Code != http.StatusOK {
		t.Fatalf("http=%d body=%s", rr.Code, rr.Body.String())
	}
}

func onlySessionRequest(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	listed := getJSON(t, h, "/api/sessions")
	rows, _ := listed["sessions"].([]any)
	if len(rows) != 1 {
		t.Fatalf("sessions=%s", mustJSON(listed))
	}
	id := rows[0].(map[string]any)["id"].(string)
	detail := getJSON(t, h, "/api/sessions/"+id)
	reqs, _ := detail["requests"].([]any)
	if len(reqs) != 1 {
		t.Fatalf("requests=%s", mustJSON(detail))
	}
	return reqs[0].(map[string]any)
}

func responsesDiagnostics(t *testing.T, h http.Handler) []diagnosticsRequestDetail {
	t.Helper()
	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	out := []diagnosticsRequestDetail{}
	for _, row := range listed.Requests {
		if row.Path == "/v1/responses" {
			out = append(out, getDiagnosticsDetail(t, h, row.RequestID))
		}
	}
	return out
}

func installCountingOverlays(h http.Handler, overlays []usage.PriceRecord) *atomic.Int32 {
	impl := h.(*handler)
	n := &atomic.Int32{}
	cloned := append([]usage.PriceRecord(nil), overlays...)
	impl.priceOverlayLoad = func() []usage.PriceRecord {
		n.Add(1)
		return append([]usage.PriceRecord(nil), cloned...)
	}
	return n
}

func operatorGPT56Overlay(input, output float64) []usage.PriceRecord {
	return []usage.PriceRecord{{
		Provider:    "openai-apikey",
		Model:       "gpt-5.6",
		Cost4:       usage.Cost4{Input: input, Output: output},
		SourceClass: usage.SourceOperator,
		SourceRef:   "config.modelCosts",
		Status:      usage.StatusOperator,
		Currency:    "USD",
	}}
}

func exactGPT56Provider() Provider {
	return &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1000, OutputTokens: 10}},
	}}
}
