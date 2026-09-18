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
	"time"

	_ "time/tzdata"

	"github.com/Wibias/Benes/internal/sessions"
	"github.com/Wibias/Benes/internal/usageledger"
)

func TestUsageAPIIsLoopbackAndCountsTokens(t *testing.T) {
	now := time.Now().UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	line := `{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"openai-apikey","model":"gpt-5.5","usageStatus":"reported","usage":{"inputTokens":10,"outputTokens":2}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		UsageLogPath:   path,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/usage?range=all", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/usage?range=all", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-") || strings.Contains(rr.Body.String(), `"apiKey"`) {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var got struct {
		Summary struct {
			Requests     int   `json:"requests"`
			InputTokens  int64 `json:"inputTokens"`
			OutputTokens int64 `json:"outputTokens"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Summary.Requests != 1 || got.Summary.InputTokens != 10 || got.Summary.OutputTokens != 2 {
		t.Fatalf("%+v body=%s", got.Summary, rr.Body.String())
	}
}

func TestUsageAPIPricesOperatorOverlayAndLeavesUnknownUnpriced(t *testing.T) {
	now := time.Now().UnixMilli()
	dir := t.TempDir()
	usagePath := filepath.Join(dir, "usage.jsonl")
	configPath := filepath.Join(dir, "config.json")
	line := `{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"openai-apikey","model":"gpt-5.5","usageStatus":"reported","usage":{"inputTokens":1000000,"outputTokens":0}}` + "\n" +
		`{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"together","model":"llama-free","usageStatus":"reported","usage":{"inputTokens":1000000,"outputTokens":0}}` + "\n"
	if err := os.WriteFile(usagePath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := `{"providers":{"openai-apikey":{"adapter":"openai-responses","baseUrl":"https://api.openai.com/v1","apiKey":"local-test-key","modelCosts":{"gpt-5.5":{"input":5,"output":0}}}}}`
	if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		UsageLogPath:   usagePath,
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/usage?range=all", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "local-test-key") || strings.Contains(rr.Body.String(), `"apiKey"`) {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var got struct {
		Summary struct {
			ExactCostUsd     float64 `json:"exactCostUsd"`
			EstimatedCostUsd float64 `json:"estimatedCostUsd"`
			PricedRequests   int     `json:"pricedRequests"`
			UnpricedRequests int     `json:"unpricedRequests"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Summary.ExactCostUsd != 5 || got.Summary.EstimatedCostUsd != 5 || got.Summary.PricedRequests != 1 || got.Summary.UnpricedRequests != 1 {
		t.Fatalf("%+v body=%s", got.Summary, rr.Body.String())
	}
}

func TestUsageAPIRejectsInvalidCustomRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": providerFunc(nil)}, UsageLogPath: path})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/usage?start=2026-08-22&end=2026-08-20", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"invalid_range"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestUsageAPIAcceptsMinutePrecisionAndFilters(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 8, 20, 14, 30, 0, 0, loc)
	end := time.Date(2026, 8, 20, 15, 0, 0, 0, loc)
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	lines := []string{
		`{"timestamp":` + strconv.FormatInt(start.UnixMilli(), 10) + `,"provider":"openai","model":"gpt-5","account":"acct-a","usageStatus":"reported","usage":{"inputTokens":3,"outputTokens":1}}`,
		`{"timestamp":` + strconv.FormatInt(start.UnixMilli(), 10) + `,"provider":"xai","model":"gpt-5","account":"acct-a","usageStatus":"reported","usage":{"inputTokens":9,"outputTokens":1}}`,
		`{"timestamp":` + strconv.FormatInt(end.UnixMilli(), 10) + `,"provider":"openai","model":"gpt-5","account":"acct-a","usageStatus":"reported","usage":{"inputTokens":9,"outputTokens":1}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": providerFunc(nil)}, UsageLogPath: path})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/usage?start=2026-08-20T14:30&end=2026-08-20T15:00&tz=UTC&provider=openai&model=gpt-5&account=acct-a", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		Summary struct {
			Requests     int   `json:"requests"`
			InputTokens  int64 `json:"inputTokens"`
			OutputTokens int64 `json:"outputTokens"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Summary.Requests != 1 || got.Summary.InputTokens != 3 || got.Summary.OutputTokens != 1 {
		t.Fatalf("%+v body=%s", got.Summary, rr.Body.String())
	}
}

func TestUsageAPIHeadIsOK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": providerFunc(nil)}, UsageLogPath: path})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodHead, "/api/usage?range=all", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Fatalf("head body=%q", rr.Body.String())
	}
}

func TestUsageAPIReportsCompletenessMetadata(t *testing.T) {
	now := time.Now().UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	line := `{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"openai","model":"gpt-5","surface":"codex","usageStatus":"reported","usage":{"inputTokens":1,"outputTokens":1}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": providerFunc(nil)}, UsageLogPath: path})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	got := getUsage(t, h, "/api/usage?range=all")
	if got["historyTruncated"] != false {
		t.Fatalf("truncated=%v", got["historyTruncated"])
	}
	if _, ok := got["snapshotWindowStart"]; !ok {
		t.Fatal("missing snapshotWindowStart")
	}
	attr, _ := got["surfaceAttribution"].(map[string]any)
	if attr["codex"] != float64(1) || attr["unattributed"] != float64(0) {
		t.Fatalf("attribution=%v", attr)
	}
}

func TestUsageAPISurfaceCodexDoesNotSelectUnattributed(t *testing.T) {
	now := time.Now().UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	lines := `{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"openai","model":"legacy","usageStatus":"reported"}` + "\n" +
		`{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"openai","model":"explicit","surface":"codex","usageStatus":"reported"}` + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": providerFunc(nil)}, UsageLogPath: path})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	got := getUsage(t, h, "/api/usage?range=all&surface=codex")
	summary := got["summary"].(map[string]any)
	if summary["requests"] != float64(1) {
		t.Fatalf("codex filter=%v", summary)
	}
}

func TestUsageAPILoadsPricingOncePerRequest(t *testing.T) {
	now := time.Now().UnixMilli()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	line := `{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"openai-apikey","model":"gpt-5.6","usageStatus":"reported","usage":{"inputTokens":1000,"outputTokens":10}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		UsageLogPath:   path,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	loads := installCountingOverlays(h, operatorGPT56Overlay(5, 10))
	got := getUsage(t, h, "/api/usage?range=all")
	if loads.Load() != 1 {
		t.Fatalf("loads=%d", loads.Load())
	}
	cost := got["cost"].(map[string]any)
	if cost["displayTotalSafe"] != true {
		t.Fatalf("cost=%v", cost)
	}
}

func TestUsageAPIObservesExternalModelCostsUpdate(t *testing.T) {
	now := time.Now().UnixMilli()
	dir := t.TempDir()
	usagePath := filepath.Join(dir, "usage.jsonl")
	configPath := filepath.Join(dir, "config.json")
	line := `{"timestamp":` + strconv.FormatInt(now, 10) + `,"provider":"openai-apikey","model":"gpt-5.5","usageStatus":"reported","usage":{"inputTokens":1000000,"outputTokens":0}}` + "\n"
	if err := os.WriteFile(usagePath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-responses","modelCosts":{"gpt-5.5":{"input":5,"output":0}}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		UsageLogPath:   usagePath,
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	first := getUsage(t, h, "/api/usage?range=all")
	if first["summary"].(map[string]any)["exactCostUsd"] != float64(5) {
		t.Fatalf("first=%v", first["summary"])
	}
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-responses","modelCosts":{"gpt-5.5":{"input":50,"output":0}}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	second := getUsage(t, h, "/api/usage?range=all")
	if second["summary"].(map[string]any)["exactCostUsd"] != float64(50) {
		t.Fatalf("second=%v", second["summary"])
	}
}

func TestUsageAPIReadFailurePreservesResolvedQuery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": providerFunc(nil)}, UsageLogPath: path})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	custom := getUsage(t, h, "/api/usage?start=2026-09-01T10:00&end=2026-09-01T12:00&tz=Europe/Berlin")
	assertUsageReadFailed(t, custom)
	if custom["range"] != "custom" {
		t.Fatalf("custom range=%v", custom["range"])
	}
	wantStart := float64(time.Date(2026, 9, 1, 10, 0, 0, 0, berlin).UnixMilli())
	wantEnd := float64(time.Date(2026, 9, 1, 12, 0, 0, 0, berlin).UnixMilli())
	if custom["since"] != wantStart || custom["until"] != wantEnd {
		t.Fatalf("custom window since=%v until=%v", custom["since"], custom["until"])
	}

	day := getUsage(t, h, "/api/usage?date=2026-09-01&tz=Europe/Berlin")
	assertUsageReadFailed(t, day)
	if day["range"] != "date" {
		t.Fatalf("date range=%v", day["range"])
	}
	wantDay := float64(time.Date(2026, 9, 1, 0, 0, 0, 0, berlin).UnixMilli())
	wantNext := float64(time.Date(2026, 9, 2, 0, 0, 0, 0, berlin).UnixMilli())
	if day["since"] != wantDay || day["until"] != wantNext {
		t.Fatalf("date window since=%v until=%v", day["since"], day["until"])
	}

	today := getUsage(t, h, "/api/usage?range=today&tz=Europe/Berlin")
	assertUsageReadFailed(t, today)
	if today["range"] != "today" {
		t.Fatalf("today range=%v", today["range"])
	}
	yesterday := getUsage(t, h, "/api/usage?range=yesterday&tz=Europe/Berlin")
	assertUsageReadFailed(t, yesterday)
	if yesterday["range"] != "yesterday" {
		t.Fatalf("yesterday range=%v", yesterday["range"])
	}
}

func assertUsageReadFailed(t *testing.T, got map[string]any) {
	t.Helper()
	if got["error"] != "read_failed" {
		t.Fatalf("error=%v body=%v", got["error"], got)
	}
	if got["historyTruncated"] != false {
		t.Fatalf("fabricated truncation=%v", got["historyTruncated"])
	}
	if got["truncatedPrefixBytes"] != float64(0) {
		t.Fatalf("fabricated prefix=%v", got["truncatedPrefixBytes"])
	}
	if got["snapshotWindowStart"] != nil || got["snapshotWindowEnd"] != nil {
		t.Fatalf("fabricated snapshot window %v %v", got["snapshotWindowStart"], got["snapshotWindowEnd"])
	}
	summary, _ := got["summary"].(map[string]any)
	if summary["requests"] != float64(0) {
		t.Fatalf("fabricated totals=%v", summary)
	}
}

func TestUsageAPIReadFailureDoesNotFabricateCompleteness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": providerFunc(nil)}, UsageLogPath: path})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	got := getUsage(t, h, "/api/usage?range=all")
	if got["error"] != "read_failed" {
		t.Fatalf("error=%v body=%v", got["error"], got)
	}
	if got["historyTruncated"] != false {
		t.Fatalf("fabricated truncation=%v", got["historyTruncated"])
	}
	if got["snapshotWindowStart"] != nil || got["snapshotWindowEnd"] != nil {
		t.Fatalf("fabricated window %v %v", got["snapshotWindowStart"], got["snapshotWindowEnd"])
	}
}

func TestUsageLogOmitsDeniedRequestsAndKeepsAdmittedRows(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": exactGPT56Provider()},
		Sessions:       store,
		UsageLogPath:   usagePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	denied := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	denied.Header.Set("Authorization", "Bearer wrong")
	denied.Header.Set("X-Request-ID", "denied-bearer")
	deniedRR := httptest.NewRecorder()
	h.ServeHTTP(deniedRR, denied)
	if deniedRR.Code == http.StatusOK {
		t.Fatal("invalid bearer succeeded")
	}
	if usageRowCount(t, usagePath) != 0 {
		t.Fatalf("denied bearer wrote usage: %s", readUsageLog(t, usagePath))
	}

	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	foundDenied := false
	for _, row := range listed.Requests {
		if row.Path == "/v1/responses" && row.Status != 200 {
			foundDenied = true
			if row.SessionID != "" {
				t.Fatalf("denied sessionId=%s", mustJSON(row))
			}
			detail := getDiagnosticsDetail(t, h, row.RequestID)
			if detail.Failure == nil || detail.Failure.Cause != "admission_denied" {
				t.Fatalf("denied failure=%s", mustJSON(detail.Failure))
			}
		}
	}
	if !foundDenied {
		t.Fatalf("diagnostics omitted denied request: %s", mustJSON(listed))
	}

	malformed := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{not-json`))
	malformed.Header.Set("Authorization", "Bearer local-secret")
	malformed.Header.Set("X-Request-ID", "admitted-malformed")
	malformedRR := httptest.NewRecorder()
	h.ServeHTTP(malformedRR, malformed)
	if malformedRR.Code == http.StatusOK {
		t.Fatal("malformed succeeded")
	}
	if usageRowCount(t, usagePath) != 1 {
		t.Fatalf("admitted malformed usage rows=%d body=%s", usageRowCount(t, usagePath), readUsageLog(t, usagePath))
	}

	postUsage(t, h, "thread-ok", "admitted-ok", "")
	if usageRowCount(t, usagePath) != 2 {
		t.Fatalf("admitted routed usage rows=%d body=%s", usageRowCount(t, usagePath), readUsageLog(t, usagePath))
	}
}

func TestUsageLogOmitsForbiddenHostAndOrigin(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	h, err := NewHandler(Options{
		AdmissionPolicy: &DataPlaneAdmissionPolicy{BindHostname: "127.0.0.1"},
		Providers:       map[string]Provider{"openai-apikey": exactGPT56Provider()},
		UsageLogPath:    usagePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	hostDenied := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	hostDenied.Host = "evil.example:23100"
	hostDenied.Header.Set("X-Request-ID", "denied-host")
	hostRR := httptest.NewRecorder()
	h.ServeHTTP(hostRR, hostDenied)
	if hostRR.Code != http.StatusForbidden {
		t.Fatalf("forbidden host status=%d body=%s", hostRR.Code, hostRR.Body.String())
	}

	originDenied := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	originDenied.Host = "127.0.0.1:23100"
	originDenied.Header.Set("Origin", "https://attacker.example")
	originDenied.Header.Set("X-Request-ID", "denied-origin")
	originRR := httptest.NewRecorder()
	h.ServeHTTP(originRR, originDenied)
	if originRR.Code != http.StatusForbidden {
		t.Fatalf("forbidden origin status=%d body=%s", originRR.Code, originRR.Body.String())
	}
	if usageRowCount(t, usagePath) != 0 {
		t.Fatalf("forbidden host/origin wrote usage: %s", readUsageLog(t, usagePath))
	}

	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	if len(listed.Requests) < 2 {
		t.Fatalf("diagnostics omitted forbidden requests: %s", mustJSON(listed))
	}
}

func TestUsageLogWriterRecordsExplicitAndUnknownSurfaces(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": exactGPT56Provider()},
		Sessions:       store,
		UsageLogPath:   usagePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postUsage(t, h, "thread-a", "corr-a", "codex")
	postUsage(t, h, "thread-b", "corr-b", "")
	all := getUsage(t, h, "/api/usage?range=all")
	attr := all["surfaceAttribution"].(map[string]any)
	if attr["codex"] != float64(1) || attr["unattributed"] != float64(1) {
		t.Fatalf("attribution=%v body=%s", attr, mustJSON(all))
	}
	codex := getUsage(t, h, "/api/usage?range=all&surface=codex")
	if codex["summary"].(map[string]any)["requests"] != float64(1) {
		t.Fatalf("surface=codex selected unknown: %v", codex["summary"])
	}
}

func getUsage(t *testing.T, h http.Handler, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, rr.Code, rr.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func postUsage(t *testing.T, h http.Handler, threadID, correlation, surface string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("thread-id", threadID)
	req.Header.Set("X-Request-ID", correlation)
	if surface != "" {
		req.Header.Set("X-Benes-Surface", surface)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("post status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func usageRowCount(t *testing.T, path string) int {
	t.Helper()
	raw := strings.TrimSpace(readUsageLog(t, path))
	if raw == "" {
		return 0
	}
	return len(strings.Split(raw, "\n"))
}

func readUsageLog(t *testing.T, path string) string {
	t.Helper()
	home := usageledger.HomeFromPath(path)
	if home == "" {
		home = filepath.Dir(path)
	}
	raw, err := usageledger.CommittedBytes(home)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
