package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/requesthistory"
)

func TestAdmittedUsageWriterFeedsRequestHistoryExplain(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": exactGPT56Provider()},
		UsageLogPath:   usagePath,
		ConfigPath:     filepath.Join(home, "config.json"),
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

	malformed := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{not-json`))
	malformed.Header.Set("Authorization", "Bearer local-secret")
	malformed.Header.Set("X-Request-ID", "admitted-malformed")
	malformedRR := httptest.NewRecorder()
	h.ServeHTTP(malformedRR, malformed)
	if malformedRR.Code == http.StatusOK {
		t.Fatal("malformed succeeded")
	}

	postUsage(t, h, "thread-ok", "admitted-ok", "codex")

	rows := parseUsageRows(t, usagePath)
	if len(rows) != 2 {
		t.Fatalf("expected one durable row per admitted request, got %d body=%s", len(rows), readUsageLog(t, usagePath))
	}

	malformedRow := rows[0]
	successRow := rows[1]
	malformedID := requireBenesRequestID(t, malformedRow)
	successID := requireBenesRequestID(t, successRow)
	if malformedID == successID {
		t.Fatalf("admitted requests shared requestId %q", successID)
	}
	if got := jsonNumber(malformedRow["status"]); got != float64(malformedRR.Code) {
		t.Fatalf("malformed status=%v want %d row=%s", malformedRow["status"], malformedRR.Code, mustJSON(malformedRow))
	}
	if _, ok := malformedRow["account"]; ok {
		t.Fatalf("malformed account leaked: %s", mustJSON(malformedRow))
	}
	requireRouteDecisionObject(t, malformedRow)

	if got := jsonNumber(successRow["status"]); got != 200 {
		t.Fatalf("success status=%v row=%s", successRow["status"], mustJSON(successRow))
	}
	if successRow["provider"] != "openai-apikey" || successRow["model"] != "gpt-5.6" {
		t.Fatalf("final route=%s", mustJSON(successRow))
	}
	if successRow["requestedModel"] != "openai-apikey/gpt-5.6" {
		t.Fatalf("requestedModel=%v", successRow["requestedModel"])
	}
	if successRow["surface"] != "codex" {
		t.Fatalf("surface=%v", successRow["surface"])
	}
	if _, ok := successRow["account"]; ok {
		t.Fatalf("non-Codex account leaked: %s", mustJSON(successRow))
	}
	if successRow["usageStatus"] != "reported" || successRow["usage"] == nil {
		t.Fatalf("usage measurement missing: %s", mustJSON(successRow))
	}
	if _, ok := successRow["durationMs"]; !ok {
		t.Fatalf("durationMs missing: %s", mustJSON(successRow))
	}
	decision := requireRouteDecisionObject(t, successRow)
	if decision["routeKind"] != "direct" {
		t.Fatalf("routeKind=%v decision=%s", decision["routeKind"], mustJSON(decision))
	}
	selected, _ := decision["selected"].(map[string]any)
	if selected["provider"] != "openai-apikey" || selected["model"] != "gpt-5.6" {
		t.Fatalf("selected=%s", mustJSON(decision))
	}
	if _, ok := decision["candidates"]; ok {
		t.Fatalf("invented candidates: %s", mustJSON(decision))
	}
	if _, ok := decision["profile"]; ok {
		t.Fatalf("invented profile: %s", mustJSON(decision))
	}
	raw := readUsageLog(t, usagePath)
	for _, leak := range []string{"Bearer ", "local-secret", "denied-bearer", "@", "apiKey"} {
		if strings.Contains(raw, leak) {
			t.Fatalf("secret or identity leaked %q in %s", leak, raw)
		}
	}

	listed := getDiagnosticsList(t, h, "/api/diagnostics/requests")
	deniedID := ""
	for _, item := range listed.Requests {
		if item.Status != 200 && item.Path == "/v1/responses" && item.RequestID != malformedID {
			deniedID = item.RequestID
		}
	}
	if deniedID == "" {
		t.Fatalf("diagnostics omitted denied request: %s", mustJSON(listed))
	}

	meta, err := requesthistory.CatchUp(home)
	if err != nil {
		t.Fatal(err)
	}
	if meta.IndexedRows != 2 {
		t.Fatalf("indexed=%d meta=%#v body=%s", meta.IndexedRows, meta, raw)
	}
	if _, err := requesthistory.Lookup(home, deniedID); err == nil {
		t.Fatalf("denied request %q was indexed", deniedID)
	}

	looked, err := requesthistory.Lookup(home, successID)
	if err != nil {
		t.Fatal(err)
	}
	if looked["requestId"] != successID {
		t.Fatalf("lookup=%s", mustJSON(looked))
	}

	explained := getJSON(t, h, "/api/request-history/"+successID+"/route-decision")
	if explained["requestId"] != successID {
		t.Fatalf("explain requestId=%s", mustJSON(explained))
	}
	if explained["routeDecision"] == nil {
		t.Fatalf("explain routeDecision is null: %s", mustJSON(explained))
	}
	gotDecision, _ := explained["routeDecision"].(map[string]any)
	if gotDecision["routeKind"] != "direct" {
		t.Fatalf("explain decision=%s", mustJSON(explained))
	}
	outcome, _ := explained["outcome"].(map[string]any)
	if jsonNumber(outcome["status"]) != 200 {
		t.Fatalf("explain outcome=%s", mustJSON(explained))
	}
	summary, _ := explained["summary"].(map[string]any)
	if summary["requestedModel"] != "openai-apikey/gpt-5.6" || summary["finalProvider"] != "openai-apikey" || summary["finalModel"] != "gpt-5.6" {
		t.Fatalf("explain summary=%s", mustJSON(explained))
	}

	malformedExplain := getJSON(t, h, "/api/request-history/"+malformedID+"/route-decision")
	if malformedExplain["routeDecision"] == nil {
		t.Fatalf("malformed explain dropped routeDecision: %s", mustJSON(malformedExplain))
	}
	malformedOutcome, _ := malformedExplain["outcome"].(map[string]any)
	if jsonNumber(malformedOutcome["status"]) != float64(malformedRR.Code) {
		t.Fatalf("malformed explain outcome=%s", mustJSON(malformedExplain))
	}
}

func TestRequestHistoryRebuildKeepsLegacyRowsAndNewUsageRows(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	legacy := `{"requestId":"req_legacy","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"direct"}}` + "\n"
	if err := os.WriteFile(usagePath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": exactGPT56Provider()},
		UsageLogPath:   usagePath,
		ConfigPath:     filepath.Join(home, "config.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postUsage(t, h, "thread-new", "admitted-new", "")

	meta, err := requesthistory.Rebuild(home)
	if err != nil {
		t.Fatal(err)
	}
	if meta.IndexedRows != 2 {
		t.Fatalf("indexed=%d body=%s", meta.IndexedRows, readUsageLog(t, usagePath))
	}
	legacyRow, err := requesthistory.Lookup(home, "req_legacy")
	if err != nil {
		t.Fatal(err)
	}
	legacyDecision, _ := legacyRow["routeDecision"].(map[string]any)
	if legacyDecision["routeKind"] != "direct" {
		t.Fatalf("legacy=%s", mustJSON(legacyRow))
	}

	rows := parseUsageRows(t, usagePath)
	if len(rows) != 2 {
		t.Fatalf("rows=%d body=%s", len(rows), readUsageLog(t, usagePath))
	}
	newID := requireBenesRequestID(t, rows[1])
	explained := getJSON(t, h, "/api/request-history/"+newID+"/route-decision")
	if explained["routeDecision"] == nil {
		t.Fatalf("new row unexplained: %s", mustJSON(explained))
	}
	legacyExplain := getJSON(t, h, "/api/request-history/req_legacy/route-decision")
	if legacyExplain["requestId"] != "req_legacy" {
		t.Fatalf("legacy explain=%s", mustJSON(legacyExplain))
	}
}

func parseUsageRows(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw := strings.TrimSpace(readUsageLog(t, path))
	if raw == "" {
		return nil
	}
	var rows []map[string]any
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("row %q: %v", line, err)
		}
		rows = append(rows, row)
	}
	return rows
}

func requireBenesRequestID(t *testing.T, row map[string]any) string {
	t.Helper()
	id, _ := row["requestId"].(string)
	if !strings.HasPrefix(id, "req_") {
		t.Fatalf("requestId=%q row=%s", id, mustJSON(row))
	}
	return id
}

func requireRouteDecisionObject(t *testing.T, row map[string]any) map[string]any {
	t.Helper()
	if row["routeDecision"] == nil {
		t.Fatalf("routeDecision is null: %s", mustJSON(row))
	}
	decision, ok := row["routeDecision"].(map[string]any)
	if !ok {
		t.Fatalf("routeDecision type=%T row=%s", row["routeDecision"], mustJSON(row))
	}
	return decision
}

func jsonNumber(value any) float64 {
	n, _ := value.(float64)
	return n
}
