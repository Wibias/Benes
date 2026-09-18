package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/sessions"
)

func TestDiagnosticsAPIRequiresLoopback(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"p": providerFunc(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/diagnostics/requests", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, blocked)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestLogsAPIRemainsLegacyArrayProjection(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postOK(t, h, "/v1/responses", `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "legacy-id")
	rr := diagnosticsGET(t, h, "/api/logs?limit=50")
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var entries []requestLogEntry
	if err := json.Unmarshal(rr.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected logs")
	}
	found := false
	for _, entry := range entries {
		if entry.Path == "/v1/responses" {
			found = true
			if entry.ID != "legacy-id" {
				t.Fatalf("legacy id=%q", entry.ID)
			}
			if entry.Method != http.MethodPost || entry.Status != 200 || entry.DurationMs < 0 || entry.Timestamp == "" {
				t.Fatalf("entry=%#v", entry)
			}
		}
	}
	if !found {
		t.Fatalf("missing responses log %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), `"requestId"`) {
		t.Fatalf("legacy logs grew new fields: %s", rr.Body.String())
	}
}

func TestDiagnosticsSummaryAndDetailForDirectRequest(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 4, OutputTokens: 2}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postOK(t, h, "/v1/responses", `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "corr-direct")
	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	if listed.NextCursor == "" || listed.Reset {
		t.Fatalf("list=%s", mustJSON(listed))
	}
	var row diagnosticsRequestSummary
	for _, item := range listed.Requests {
		if item.Path == "/v1/responses" {
			row = item
		}
	}
	if row.RequestID == "" || row.RequestID == "corr-direct" {
		t.Fatalf("requestId=%q", row.RequestID)
	}
	if !strings.HasPrefix(row.RequestID, "req_") {
		t.Fatalf("requestId=%q", row.RequestID)
	}
	if row.Provider != "openai-apikey" || row.ResolvedModel != "gpt-5.6" || row.Status != 200 {
		t.Fatalf("summary=%s", mustJSON(row))
	}
	if row.Protocol != sessions.ProtocolResponses || row.UsageStatus != "reported" || row.TotalTokens == nil || *row.TotalTokens != 6 {
		t.Fatalf("usage summary=%s", mustJSON(row))
	}
	detail := getDiagnosticsDetail(t, h, row.RequestID)
	if detail.CorrelationID != "corr-direct" || detail.RequestID != row.RequestID {
		t.Fatalf("identity=%s", mustJSON(detail))
	}
	if detail.Routing == nil || detail.Routing.Kind != "direct" || detail.Routing.RequestedModel != "openai-apikey/gpt-5.6" {
		t.Fatalf("routing=%s", mustJSON(detail.Routing))
	}
	if detail.Usage == nil || detail.Usage.Status != "reported" {
		t.Fatalf("usage=%s", mustJSON(detail.Usage))
	}
	if detail.Cost == nil || detail.Cost.Kind != "unavailable" {
		t.Fatalf("cost=%s", mustJSON(detail.Cost))
	}
	raw := diagnosticsGET(t, h, "/api/diagnostics/requests/"+row.RequestID).Body.String()
	if strings.Contains(raw, `"surface"`) || strings.Contains(raw, `"client"`) {
		t.Fatalf("invented client/surface: %s", raw)
	}
}

func TestDiagnosticsSessionLinkageAndUngroupedDenied(t *testing.T) {
	h, _ := newSessionHandler(t)
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "thread-diag", "client-a")
	listed := getJSON(t, h, "/api/sessions")
	id := listed["sessions"].([]any)[0].(map[string]any)["id"].(string)

	rows := getDiagnosticsList(t, h, "/api/diagnostics/requests?sessionId="+id)
	if len(rows.Requests) == 0 {
		t.Fatal("expected session-linked diagnostics")
	}
	for _, row := range rows.Requests {
		if row.SessionID != id {
			t.Fatalf("leaked %s", mustJSON(row))
		}
	}

	chat := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}]}`))
	chat.Header.Set("Authorization", "Bearer local-secret")
	chatRR := httptest.NewRecorder()
	h.ServeHTTP(chatRR, chat)
	if chatRR.Code != http.StatusOK {
		t.Fatalf("chat status=%d body=%s", chatRR.Code, chatRR.Body.String())
	}
	denied := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	denied.Header.Set("Authorization", "Bearer wrong")
	denied.Header.Set("X-Request-ID", "denied-corr")
	deniedRR := httptest.NewRecorder()
	h.ServeHTTP(deniedRR, denied)
	if deniedRR.Code == http.StatusOK {
		t.Fatal("denied succeeded")
	}

	all := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	foundChat := false
	foundDenied := false
	for _, row := range all.Requests {
		if row.Path == "/v1/chat/completions" {
			foundChat = true
			if row.SessionID != "" {
				t.Fatalf("ungrouped chat sessionId=%s", mustJSON(row))
			}
			if row.Protocol != sessions.ProtocolChat {
				t.Fatalf("chat protocol=%s", mustJSON(row))
			}
		}
		if row.Path == "/v1/responses" && row.Status != 200 {
			foundDenied = true
			if row.SessionID != "" {
				t.Fatalf("denied sessionId=%s", mustJSON(row))
			}
			detail := getDiagnosticsDetail(t, h, row.RequestID)
			if detail.CorrelationID != "denied-corr" {
				t.Fatalf("denied correlation=%s", mustJSON(detail))
			}
			if detail.Failure == nil || detail.Failure.Cause != "admission_denied" {
				t.Fatalf("denied failure=%s", mustJSON(detail.Failure))
			}
		}
	}
	if !foundChat || !foundDenied {
		t.Fatalf("missing ungrouped/denied %s", mustJSON(all))
	}

	malformed := diagnosticsGET(t, h, "/api/diagnostics/requests?sessionId=not-a-session")
	if malformed.Code != http.StatusBadRequest || !strings.Contains(malformed.Body.String(), "invalid_session_id") {
		t.Fatalf("malformed=%d %s", malformed.Code, malformed.Body.String())
	}
}

func TestDiagnosticsComboAndPolicyAttempts(t *testing.T) {
	first := &capturingOpenProvider{err: fmt.Errorf("Google GenerateContent returned HTTP 503")}
	second := &capturingOpenProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google": first, "openai-apikey": second},
		Combos: []Combo{{
			ID: "fast",
			Targets: []ComboTarget{
				{ProviderID: "google", Model: "gemini-flash", Protocol: "google"},
				{ProviderID: "openai-apikey", Model: "gpt-5.4", Protocol: "openai-chat"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postOK(t, h, "/v1/responses", `{"model":"combo/fast","store":false,"stream":true}`, "combo-corr")
	detail := latestResponsesDetail(t, h)
	if detail.Routing == nil || detail.Routing.Kind != "combo" || detail.Routing.ComboID != "fast" {
		t.Fatalf("combo routing=%s", mustJSON(detail.Routing))
	}
	if detail.Routing.Provider != "openai-apikey" || detail.Routing.ResolvedModel != "gpt-5.4" {
		t.Fatalf("committed=%s", mustJSON(detail.Routing))
	}
	if len(detail.Attempts) < 2 || detail.Attempts[0].Decision != "hop" || detail.Attempts[len(detail.Attempts)-1].Decision != "committed" {
		t.Fatalf("attempts=%s", mustJSON(detail.Attempts))
	}

	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"routingProfiles":{"fast":{"candidates":[{"provider":"openai-apikey","model":"gpt-5.4"}]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	policy = attachHandlerClose(t, policy)
	postOK(t, policy, "/v1/responses", `{"model":"policy/fast","store":false,"stream":true}`, "policy-corr")
	policyDetail := latestResponsesDetail(t, policy)
	if policyDetail.Routing == nil || policyDetail.Routing.Kind != "policy" || policyDetail.Routing.PolicyID != "fast" {
		t.Fatalf("policy routing=%s", mustJSON(policyDetail.Routing))
	}
}

func TestDiagnosticsUsageAndCostProvenance(t *testing.T) {
	none, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}}})
	if err != nil {
		t.Fatal(err)
	}
	none = attachHandlerClose(t, none)
	postOK(t, none, "/v1/responses", `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "no-usage")
	noUsage := latestResponsesDetail(t, none)
	if noUsage.Usage == nil || noUsage.Usage.Status != "unreported" || noUsage.Usage.TotalTokens != nil {
		t.Fatalf("unreported=%s", mustJSON(noUsage.Usage))
	}

	estimated, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"xai": &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1000, OutputTokens: 10, Estimated: true}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	estimated = attachHandlerClose(t, estimated)
	postOK(t, estimated, "/v1/responses", `{"model":"xai/grok-4.6","store":false,"stream":true}`, "est-usage")
	est := latestResponsesDetail(t, estimated)
	if est.Usage == nil || est.Usage.Status != "estimated" {
		t.Fatalf("estimated usage=%s", mustJSON(est.Usage))
	}
	if est.Cost == nil || est.Cost.Kind != "estimated" || est.Cost.Total == nil {
		t.Fatalf("estimated cost=%s", mustJSON(est.Cost))
	}

	exact, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"xai": &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1000, OutputTokens: 10}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	exact = attachHandlerClose(t, exact)
	postOK(t, exact, "/v1/responses", `{"model":"xai/grok-4.6","store":false,"stream":true}`, "exact-usage")
	got := latestResponsesDetail(t, exact)
	if got.Usage == nil || got.Usage.Status != "reported" {
		t.Fatalf("reported=%s", mustJSON(got.Usage))
	}
	if got.Cost == nil || got.Cost.Kind != "exact" || got.Cost.Currency != "USD" || got.Cost.Total == nil {
		t.Fatalf("exact cost=%s", mustJSON(got.Cost))
	}
}

func TestDiagnosticsIncrementalPollingAndReset(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postOK(t, h, "/v1/models", "", "poll-1")
	first := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	if first.NextCursor == "" {
		t.Fatal("missing cursor")
	}
	empty := getDiagnosticsList(t, h, "/api/diagnostics/requests?cursor="+first.NextCursor)
	if len(empty.Requests) != 0 || empty.Reset || empty.HistoryTruncated {
		t.Fatalf("empty poll=%s", mustJSON(empty))
	}
	postOK(t, h, "/v1/models", "", "poll-2")
	postOK(t, h, "/v1/models", "", "poll-3")
	delta := getDiagnosticsList(t, h, "/api/diagnostics/requests?cursor="+first.NextCursor)
	if len(delta.Requests) != 2 {
		t.Fatalf("delta=%s", mustJSON(delta))
	}
	filtered := getDiagnosticsList(t, h, "/api/diagnostics/requests?cursor="+first.NextCursor+"&status=200")
	if len(filtered.Requests) != 2 {
		t.Fatalf("filtered=%s", mustJSON(filtered))
	}
	malformed := diagnosticsGET(t, h, "/api/diagnostics/requests?cursor=not-a-cursor")
	if malformed.Code != http.StatusBadRequest || !strings.Contains(malformed.Body.String(), "invalid_cursor") {
		t.Fatalf("malformed cursor=%d %s", malformed.Code, malformed.Body.String())
	}

	impl := h.(*handler)
	impl.diagnostics.rotateGenerationForTest()
	reset := getDiagnosticsList(t, h, "/api/diagnostics/requests?cursor="+delta.NextCursor)
	if !reset.Reset {
		t.Fatalf("generation reset=%s", mustJSON(reset))
	}

	for i := 0; i < requestLogMax+5; i++ {
		impl.diagnostics.append(requestTelemetryRecord{RequestID: sessions.NewRequestID(), LegacyID: fmt.Sprintf("evict-%d", i), Path: "/v1/models", Method: http.MethodGet, Status: 200})
	}
	stale := encodeDiagnosticsCursor(impl.diagnostics.generation, 1)
	truncated := getDiagnosticsList(t, h, "/api/diagnostics/requests?cursor="+stale)
	if !truncated.HistoryTruncated || !truncated.Reset {
		t.Fatalf("truncated=%s", mustJSON(truncated))
	}
}

func TestDiagnosticsDetail404WhenEvicted(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"p": providerFunc(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := diagnosticsGET(t, h, "/api/diagnostics/requests/req_deadbeefdeadbeefdeadbeefdeadbeef")
	if rr.Code != http.StatusNotFound || !strings.Contains(rr.Body.String(), "request_not_retained") {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDiagnosticsBoundsOversizedRequestID(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	normal := "client-corr-1"
	postOK(t, h, "/v1/responses", `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, normal)
	oversized := strings.Repeat("x", diagnosticsStringMax+512)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", oversized)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	var normalRow, hugeRow diagnosticsRequestSummary
	for _, row := range listed.Requests {
		if row.Path != "/v1/responses" {
			continue
		}
		detail := getDiagnosticsDetail(t, h, row.RequestID)
		if !strings.HasPrefix(detail.RequestID, "req_") {
			t.Fatalf("benes id truncated %q", detail.RequestID)
		}
		if detail.CorrelationID == normal {
			normalRow = row
			if len(detail.CorrelationID) != len(normal) {
				t.Fatalf("normal correlation changed %q", detail.CorrelationID)
			}
		}
		if strings.Trim(detail.CorrelationID, "x") == "" {
			hugeRow = row
			if len(detail.CorrelationID) > diagnosticsStringMax {
				t.Fatalf("correlation unbounded %d", len(detail.CorrelationID))
			}
			if !utf8.ValidString(detail.CorrelationID) {
				t.Fatal("correlation invalid utf8")
			}
		}
	}
	if normalRow.RequestID == "" || hugeRow.RequestID == "" {
		t.Fatalf("missing rows %s", mustJSON(listed))
	}
	legacy := getLogEntries(t, h, "/api/logs")
	foundNormal, foundHuge := false, false
	for _, entry := range legacy {
		if entry.Path != "/v1/responses" {
			continue
		}
		if entry.ID == normal {
			foundNormal = true
		}
		if strings.Trim(entry.ID, "x") == "" {
			foundHuge = true
			if len(entry.ID) > diagnosticsStringMax {
				t.Fatalf("legacy id unbounded %d", len(entry.ID))
			}
		}
	}
	if !foundNormal || !foundHuge {
		t.Fatalf("legacy projection missing bound ids %s", mustJSON(legacy))
	}
	impl := h.(*handler)
	impl.diagnostics.mu.Lock()
	defer impl.diagnostics.mu.Unlock()
	for _, record := range impl.diagnostics.records {
		if len(record.LegacyID) > diagnosticsStringMax || len(record.CorrelationID) > diagnosticsStringMax {
			t.Fatalf("retained unbounded id legacy=%d corr=%d", len(record.LegacyID), len(record.CorrelationID))
		}
	}
}

func TestDiagnosticsPanicBeforeWriteRecords500(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{"openai-apikey": providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
			panic("boom-secret-should-not-leak")
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "panic-before")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "boom-secret-should-not-leak") {
		t.Fatalf("panic payload leaked to client %s", rr.Body.String())
	}
	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	rows := 0
	var detail diagnosticsRequestDetail
	for _, row := range listed.Requests {
		if row.Path == "/v1/responses" {
			rows++
			detail = getDiagnosticsDetail(t, h, row.RequestID)
		}
	}
	if rows != 1 {
		t.Fatalf("records=%d list=%s", rows, mustJSON(listed))
	}
	if detail.Status != http.StatusInternalServerError {
		t.Fatalf("telemetry status=%d", detail.Status)
	}
	if detail.Failure == nil || detail.Failure.Cause != "internal_panic" || detail.ErrorCode != "internal_panic" {
		t.Fatalf("failure=%s", mustJSON(detail.Failure))
	}
	raw := diagnosticsGET(t, h, "/api/diagnostics/requests/"+detail.RequestID).Body.String()
	if strings.Contains(raw, "boom-secret-should-not-leak") {
		t.Fatalf("panic payload leaked to diagnostics %s", raw)
	}
	legacy := getLogEntries(t, h, "/api/logs")
	found := false
	for _, entry := range legacy {
		if entry.Path == "/v1/responses" {
			found = true
			if entry.Status != http.StatusInternalServerError || entry.ID != "panic-before" {
				t.Fatalf("legacy=%#v", entry)
			}
		}
	}
	if !found {
		t.Fatalf("legacy missing %s", mustJSON(legacy))
	}
}

func TestDiagnosticsPanicAfterCommittedStatus(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{"openai-apikey": providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
			return &panicAfterEventStream{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}}, panicOn: 1}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "panic-after")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("committed status=%d body=%s", rr.Code, rr.Body.String())
	}
	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	rows := 0
	var detail diagnosticsRequestDetail
	for _, row := range listed.Requests {
		if row.Path == "/v1/responses" {
			rows++
			detail = getDiagnosticsDetail(t, h, row.RequestID)
		}
	}
	if rows != 1 {
		t.Fatalf("records=%d list=%s", rows, mustJSON(listed))
	}
	if detail.Status != http.StatusOK {
		t.Fatalf("telemetry status=%d want committed 200", detail.Status)
	}
	if detail.Failure == nil || detail.Failure.Cause != "internal_panic" {
		t.Fatalf("failure=%s", mustJSON(detail.Failure))
	}
	raw := diagnosticsGET(t, h, "/api/diagnostics/requests/"+detail.RequestID).Body.String()
	if strings.Contains(raw, "boom-secret-should-not-leak") {
		t.Fatalf("panic payload leaked %s", raw)
	}
	legacy := getLogEntries(t, h, "/api/logs")
	found := false
	for _, entry := range legacy {
		if entry.Path == "/v1/responses" {
			found = true
			if entry.Status != http.StatusOK {
				t.Fatalf("legacy status=%d", entry.Status)
			}
		}
	}
	if !found {
		t.Fatal("legacy missing")
	}
}

type panicAfterEventStream struct {
	events  []protocol.Event
	n       int
	panicOn int
}

func (s *panicAfterEventStream) Next() (protocol.Event, error) {
	if s.n == s.panicOn {
		panic("boom-secret-should-not-leak")
	}
	if s.n >= len(s.events) {
		return protocol.Event{}, io.EOF
	}
	event := s.events[s.n]
	s.n++
	return event, nil
}

func (s *panicAfterEventStream) Close() error { return nil }

func TestDiagnosticsPrivacyOmitsSecretsAndBodies(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	body := `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true,"input":"PROMPT-SECRET-TEXT-123"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("x-api-key", "super-secret-api-key-value")
	req.Header.Set("x-benes-api-key", "super-secret-benes-key-value")
	req.Header.Set("Cookie", "session=super-secret-cookie")
	req.Header.Set("X-Request-ID", "privacy-corr")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	id := latestResponses(listed).RequestID
	raw := diagnosticsGET(t, h, "/api/diagnostics/requests/"+id).Body.String()
	for _, leak := range []string{"PROMPT-SECRET-TEXT-123", "local-secret", "super-secret-api-key-value", "super-secret-benes-key-value", "super-secret-cookie", "Authorization", "x-api-key"} {
		if strings.Contains(raw, leak) {
			t.Fatalf("leaked %q in %s", leak, raw)
		}
	}
	legacy := diagnosticsGET(t, h, "/api/logs").Body.String()
	if strings.Contains(legacy, "PROMPT-SECRET-TEXT-123") || strings.Contains(legacy, "local-secret") {
		t.Fatalf("legacy leaked %s", legacy)
	}
}

func TestDiagnosticsFiltersComposeWithCursor(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{
		"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}},
		"xai":           &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postOK(t, h, "/v1/responses", `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "filter-a")
	snapshot := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	postOK(t, h, "/v1/responses", `{"model":"xai/grok-4.6","store":false,"stream":true}`, "filter-b")
	delta := getDiagnosticsList(t, h, "/api/diagnostics/requests?cursor="+snapshot.NextCursor+"&provider=xai")
	if len(delta.Requests) != 1 || delta.Requests[0].Provider != "xai" {
		t.Fatalf("provider filter=%s", mustJSON(delta))
	}
	kind := getDiagnosticsList(t, h, "/api/diagnostics/requests?routeKind=direct")
	if len(kind.Requests) == 0 {
		t.Fatal("expected direct routes")
	}
	unknown := getDiagnosticsList(t, h, "/api/diagnostics/requests?protocol=chat_completions")
	if len(unknown.Requests) != 0 {
		t.Fatalf("protocol filter leaked %s", mustJSON(unknown))
	}
}

func TestDiagnosticsStoreFailureDoesNotSuppressTelemetry(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	postOK(t, h, "/v1/responses", `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "closed-store")
	detail := latestResponsesDetail(t, h)
	if detail.SessionID != "" {
		t.Fatalf("persist failure linked %s", mustJSON(detail))
	}
	if detail.Failure == nil || detail.Failure.Cause != "session_persist_failed" {
		t.Fatalf("persist failure=%s", mustJSON(detail.Failure))
	}
}

func diagnosticsGET(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func getDiagnosticsList(t *testing.T, h http.Handler, path string) diagnosticsListResponse {
	t.Helper()
	rr := diagnosticsGET(t, h, path)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, rr.Code, rr.Body.String())
	}
	var listed diagnosticsListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Requests == nil {
		listed.Requests = []diagnosticsRequestSummary{}
	}
	return listed
}

func getDiagnosticsDetail(t *testing.T, h http.Handler, id string) diagnosticsRequestDetail {
	t.Helper()
	rr := diagnosticsGET(t, h, "/api/diagnostics/requests/"+id)
	if rr.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", rr.Code, rr.Body.String())
	}
	var detail diagnosticsRequestDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	return detail
}

func postOK(t *testing.T, h http.Handler, path, body, correlation string) {
	t.Helper()
	method := http.MethodPost
	if path == "/v1/models" {
		method = http.MethodGet
		body = ""
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer local-secret")
	if correlation != "" {
		req.Header.Set("X-Request-ID", correlation)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("%s status=%d body=%s", path, rr.Code, rr.Body.String())
	}
}

func latestResponses(listed diagnosticsListResponse) diagnosticsRequestSummary {
	for i := len(listed.Requests) - 1; i >= 0; i-- {
		if listed.Requests[i].Path == "/v1/responses" {
			return listed.Requests[i]
		}
	}
	return diagnosticsRequestSummary{}
}

func latestResponsesDetail(t *testing.T, h http.Handler) diagnosticsRequestDetail {
	t.Helper()
	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	row := latestResponses(listed)
	if row.RequestID == "" {
		t.Fatalf("missing responses row %s", mustJSON(listed))
	}
	return getDiagnosticsDetail(t, h, row.RequestID)
}
