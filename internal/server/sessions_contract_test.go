package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/sessions"
)

func TestSessionsAPIIncludesWholeSessionAggregates(t *testing.T) {
	h, _ := newSessionHandler(t)
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "agg-thread", "req-1")
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "agg-thread", "req-2")
	listed := getJSON(t, h, "/api/sessions")
	id := listed["sessions"].([]any)[0].(map[string]any)["id"].(string)
	page := getJSON(t, h, "/api/sessions/"+id+"?limit=1")
	full := getJSON(t, h, "/api/sessions/"+id)
	if page["hasMore"] != true {
		t.Fatalf("expected paged requests %s", mustJSON(page))
	}
	pageAgg := page["aggregates"].(map[string]any)
	fullAgg := full["aggregates"].(map[string]any)
	if mustJSON(pageAgg) != mustJSON(fullAgg) {
		t.Fatalf("limit changed aggregates page=%s full=%s", mustJSON(pageAgg), mustJSON(fullAgg))
	}
	models := pageAgg["models"].([]any)
	if len(models) != 1 || models[0] != "gpt-5.6" {
		t.Fatalf("models=%s", mustJSON(models))
	}
	if pageAgg["hadFailover"] != false || pageAgg["failoverRequestCount"] != float64(0) {
		t.Fatalf("failover=%s", mustJSON(pageAgg))
	}
	session := listed["sessions"].([]any)[0].(map[string]any)
	if protocols := session["protocols"].([]any); len(protocols) != 1 || protocols[0] != "responses" {
		t.Fatalf("list protocols=%s", mustJSON(session))
	}
	usage := pageAgg["usage"].(map[string]any)
	input := usage["inputTokens"].(map[string]any)
	if input["complete"] != true || input["value"] != float64(8) {
		t.Fatalf("usage=%s", mustJSON(usage))
	}
}

func TestSessionsAPIComboFailoverCountAndPolicyIds(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	first := &capturingOpenProvider{err: fmt.Errorf("Google GenerateContent returned HTTP 503")}
	second := &capturingOpenProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google": first, "openai-apikey": second},
		Sessions:       store,
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
	postSession(t, h, `{"model":"combo/fast","store":false,"stream":true}`, "thread-combo", "req-combo")
	listed := getJSON(t, h, "/api/sessions")
	id := listed["sessions"].([]any)[0].(map[string]any)["id"].(string)
	detail := getJSON(t, h, "/api/sessions/"+id)
	agg := detail["aggregates"].(map[string]any)
	if agg["failoverRequestCount"] != float64(1) || agg["hadFailover"] != true {
		t.Fatalf("aggregates=%s", mustJSON(agg))
	}
	if comboIDs := agg["comboIds"].([]any); len(comboIDs) != 1 || comboIDs[0] != "fast" {
		t.Fatalf("comboIds=%s", mustJSON(agg))
	}
	if models := agg["models"].([]any); len(models) != 1 || models[0] != "gpt-5.4" {
		t.Fatalf("models=%s", mustJSON(agg))
	}
}

func TestSessionsAPISearchFiltersAndMetadata(t *testing.T) {
	h, _ := newSessionHandler(t)
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "keep-thread", "req-keep")
	listed := getJSON(t, h, "/api/sessions")
	id := listed["sessions"].([]any)[0].(map[string]any)["id"].(string)

	byQ := getJSON(t, h, "/api/sessions?q=KEEP-thread")
	if len(byQ["sessions"].([]any)) != 1 {
		t.Fatalf("q=%s", mustJSON(byQ))
	}
	byNS := getJSON(t, h, "/api/sessions?namespace=codex/thread")
	if len(byNS["sessions"].([]any)) != 1 {
		t.Fatalf("namespace=%s", mustJSON(byNS))
	}
	byProtocol := getJSON(t, h, "/api/sessions?protocol=responses")
	if len(byProtocol["sessions"].([]any)) != 1 {
		t.Fatalf("protocol=%s", mustJSON(byProtocol))
	}
	if protocols := byProtocol["sessions"].([]any)[0].(map[string]any)["protocols"].([]any); len(protocols) != 1 || protocols[0] != "responses" {
		t.Fatalf("filtered protocols=%s", mustJSON(byProtocol))
	}
	missing := getJSON(t, h, "/api/sessions?protocol=chat_completions")
	if len(missing["sessions"].([]any)) != 0 {
		t.Fatalf("chat filter=%s", mustJSON(missing))
	}
	bad := httptest.NewRequest(http.MethodGet, "/api/sessions?protocol=nope", nil)
	bad.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, bad)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad protocol status=%d body=%s", rr.Code, rr.Body.String())
	}
	filters := getJSON(t, h, "/api/sessions/filters")
	if protocols := filters["protocols"].([]any); len(protocols) != 1 || protocols[0] != "responses" {
		t.Fatalf("filters=%s", mustJSON(filters))
	}
	if namespaces := filters["namespaces"].([]any); len(namespaces) != 1 || namespaces[0] != "codex/thread" {
		t.Fatalf("namespaces=%s", mustJSON(filters))
	}
	_ = id
}

func TestSessionsAPIHealthyEmptyFilters(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": providerFunc(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	filters := getJSON(t, h, "/api/sessions/filters")
	if len(filters["protocols"].([]any)) != 0 || len(filters["namespaces"].([]any)) != 0 {
		t.Fatalf("empty filters=%s", mustJSON(filters))
	}
}

func TestSessionsAPIUnavailableStoreFilters(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken:      "local-secret",
		Providers:           map[string]Provider{"openai-apikey": providerFunc(nil)},
		SessionsUnavailable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	assertSessionStoreUnavailable(t, h, "/api/sessions/filters")
}

func TestSessionsAPISearchDoesNotLookAtPrompts(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "secret-prompt-token"}, {Type: protocol.EventDone}}}},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true,"input":"secret-prompt-token"}`, "visible-thread", "req-visible")
	listed := getJSON(t, h, "/api/sessions?q=secret-prompt-token")
	if len(listed["sessions"].([]any)) != 0 {
		t.Fatalf("prompt leaked into search %s", mustJSON(listed))
	}
}

func TestSessionsAPIPolicyAggregateDoesNotTreatAliasAsModel(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"routingProfiles":{"fast":{"candidates":[{"provider":"openai-apikey","model":"gpt-5.4"}]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}},
		ConfigPath:     configPath,
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postSession(t, h, `{"model":"policy/fast","store":false,"stream":true}`, "thread-policy", "req-policy")
	listed := getJSON(t, h, "/api/sessions")
	id := listed["sessions"].([]any)[0].(map[string]any)["id"].(string)
	detail := getJSON(t, h, "/api/sessions/"+id)
	agg := detail["aggregates"].(map[string]any)
	if policyIDs := agg["policyIds"].([]any); len(policyIDs) != 1 || policyIDs[0] != "fast" {
		t.Fatalf("policyIds=%s", mustJSON(agg))
	}
	models := agg["models"].([]any)
	for _, model := range models {
		if strings.HasPrefix(fmt.Sprint(model), "policy/") {
			t.Fatalf("requested alias treated as model %s", mustJSON(agg))
		}
	}
}
