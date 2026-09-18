package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/sessions"
)

func TestSessionsAPIRequiresLoopback(t *testing.T) {
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": providerFunc(nil)}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSessionsAPIAdmissionDeniedDoesNotRecord(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	denied := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	denied.Header.Set("Authorization", "Bearer wrong")
	denied.Header.Set("thread-id", "denied-thread")
	denied.Header.Set("X-Request-ID", "req-denied")
	deniedRR := httptest.NewRecorder()
	h.ServeHTTP(deniedRR, denied)
	if deniedRR.Code == http.StatusOK {
		t.Fatalf("denied succeeded %s", deniedRR.Body.String())
	}
	listed := getJSON(t, h, "/api/sessions")
	if sessionsRaw, _ := listed["sessions"].([]any); len(sessionsRaw) != 0 {
		t.Fatalf("admission-denied created sessions %s", mustJSON(listed))
	}
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "kept-thread", "req-kept")
	listed = getJSON(t, h, "/api/sessions")
	row := listed["sessions"].([]any)[0].(map[string]any)
	if row["requestCount"] != float64(1) || row["externalId"] != "kept-thread" {
		t.Fatalf("summary=%s", mustJSON(row))
	}
	deniedExisting := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	deniedExisting.Header.Set("Authorization", "Bearer wrong")
	deniedExisting.Header.Set("thread-id", "kept-thread")
	deniedExisting.Header.Set("X-Request-ID", "req-denied-existing")
	existingRR := httptest.NewRecorder()
	h.ServeHTTP(existingRR, deniedExisting)
	if existingRR.Code == http.StatusOK {
		t.Fatalf("denied existing succeeded %s", existingRR.Body.String())
	}
	listed = getJSON(t, h, "/api/sessions")
	row = listed["sessions"].([]any)[0].(map[string]any)
	if row["requestCount"] != float64(1) {
		t.Fatalf("denied mutated session %s", mustJSON(listed))
	}
	detail := getJSON(t, h, "/api/sessions/"+row["id"].(string))
	if len(detail["requests"].([]any)) != 1 {
		t.Fatalf("denied appended request %s", mustJSON(detail))
	}
}

func TestSessionsAPIDuplicateClientRequestIDDoesNotCollide(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "thread-a", "shared-client-id")
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "thread-b", "shared-client-id")
	listed := getJSON(t, h, "/api/sessions")
	sessionsRaw := listed["sessions"].([]any)
	if len(sessionsRaw) != 2 {
		t.Fatalf("expected two sessions %s", mustJSON(listed))
	}
	ids := map[string]bool{}
	for _, raw := range sessionsRaw {
		row := raw.(map[string]any)
		if row["requestCount"] != float64(1) {
			t.Fatalf("phantom or merged session %s", mustJSON(row))
		}
		detail := getJSON(t, h, "/api/sessions/"+row["id"].(string))
		req := detail["requests"].([]any)[0].(map[string]any)
		if req["id"] == "shared-client-id" {
			t.Fatalf("client request id used as durable id %s", mustJSON(req))
		}
		if req["correlationId"] != "shared-client-id" {
			t.Fatalf("missing correlation id %s", mustJSON(req))
		}
		ids[req["id"].(string)] = true
	}
	if len(ids) != 2 {
		t.Fatalf("durable ids collided %#v", ids)
	}
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "thread-a", "shared-client-id")
	listed = getJSON(t, h, "/api/sessions")
	var threadA map[string]any
	for _, raw := range listed["sessions"].([]any) {
		row := raw.(map[string]any)
		if row["externalId"] == "thread-a" {
			threadA = row
		}
	}
	if threadA["requestCount"] != float64(2) {
		t.Fatalf("same-session duplicate client id %s", mustJSON(listed))
	}
}

func TestSessionsAPIGroupsThreadAndSurvivesRestart(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions.sqlite")
	store, err := sessions.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	provider := &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 4, OutputTokens: 2}},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		Sessions:       store,
		ConfigPath:     filepath.Join(home, "config.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "thread-durable", "req-1")
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "thread-durable", "req-2")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sessions.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	h2, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		Sessions:       reopened,
	})
	if err != nil {
		t.Fatal(err)
	}
	h2 = attachHandlerClose(t, h2)
	listed := getJSON(t, h2, "/api/sessions")
	sessionsRaw, _ := listed["sessions"].([]any)
	if len(sessionsRaw) != 1 {
		t.Fatalf("list=%s", mustJSON(listed))
	}
	row := sessionsRaw[0].(map[string]any)
	if row["namespace"] != "codex/thread" || row["externalId"] != "thread-durable" || row["requestCount"] != float64(2) {
		t.Fatalf("summary=%s", mustJSON(row))
	}
	detail := getJSON(t, h2, "/api/sessions/"+row["id"].(string))
	requests, _ := detail["requests"].([]any)
	if len(requests) != 2 {
		t.Fatalf("detail=%s", mustJSON(detail))
	}
	first := requests[0].(map[string]any)
	routing := first["routing"].(map[string]any)
	if routing["kind"] != "direct" || routing["requestedModel"] != "openai-apikey/gpt-5.6" || routing["provider"] != "openai-apikey" || routing["resolvedModel"] != "gpt-5.6" {
		t.Fatalf("routing=%s", mustJSON(routing))
	}
	usage, _ := first["usage"].(map[string]any)
	if usage["inputTokens"] != float64(4) || usage["outputTokens"] != float64(2) {
		t.Fatalf("usage=%s", mustJSON(usage))
	}
}

func TestSessionsAPIComboFailoverIsOneRequest(t *testing.T) {
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
	requests := detail["requests"].([]any)
	if len(requests) != 1 {
		t.Fatalf("expected one inbound request got %s", mustJSON(detail))
	}
	routing := requests[0].(map[string]any)["routing"].(map[string]any)
	if routing["kind"] != "combo" || routing["comboId"] != "fast" || routing["resolvedModel"] != "gpt-5.4" || routing["provider"] != "openai-apikey" {
		t.Fatalf("routing=%s", mustJSON(routing))
	}
	attempts, _ := routing["attempts"].([]any)
	if len(attempts) < 2 {
		t.Fatalf("attempts=%s", mustJSON(routing))
	}
}

func TestSessionsAPIPolicyRouteAttribution(t *testing.T) {
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
	provider := &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
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
	routing := detail["requests"].([]any)[0].(map[string]any)["routing"].(map[string]any)
	if routing["kind"] != "policy" || routing["policyId"] != "fast" || routing["requestedModel"] != "policy/fast" {
		t.Fatalf("routing=%s", mustJSON(routing))
	}
}

func TestSessionsAPIChatWithoutIdentityIsUngrouped(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hi"}, {Type: protocol.EventDone}}}},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}],"stream":false}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	listed := getJSON(t, h, "/api/sessions")
	if sessionsRaw, _ := listed["sessions"].([]any); len(sessionsRaw) != 0 {
		t.Fatalf("fabricated sessions %s", mustJSON(listed))
	}
}

func TestSessionsAPIOmitsEstimatedUsageAndSecrets(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	provider := &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 9, OutputTokens: 1, Estimated: true}},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("thread-id", "secret-thread")
	req.Header.Set("X-Request-ID", "req-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	listed := getJSON(t, h, "/api/sessions")
	body := mustJSON(listed)
	if strings.Contains(body, "local-secret") {
		t.Fatalf("secret leaked %s", body)
	}
	id := listed["sessions"].([]any)[0].(map[string]any)["id"].(string)
	detail := getJSON(t, h, "/api/sessions/"+id)
	detailBody := mustJSON(detail)
	if strings.Contains(detailBody, "local-secret") || strings.Contains(detailBody, "Authorization") {
		t.Fatalf("secret leaked %s", detailBody)
	}
	reqJSON := detail["requests"].([]any)[0].(map[string]any)
	if _, ok := reqJSON["usage"]; ok {
		t.Fatalf("estimated usage persisted %s", detailBody)
	}
}

func TestSessionsAPIPaginationUnknownAndMalformed(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	provider := &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	for i := 0; i < 3; i++ {
		postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "thread-"+strconv.Itoa(i), "req-"+strconv.Itoa(i))
	}
	page1 := getJSON(t, h, "/api/sessions?limit=2")
	if page1["hasMore"] != true || page1["nextCursor"] == nil {
		t.Fatalf("page1=%s", mustJSON(page1))
	}
	page2 := getJSON(t, h, "/api/sessions?limit=2&cursor="+queryEscape(page1["nextCursor"].(string)))
	if page2["hasMore"] != false || len(page2["sessions"].([]any)) != 1 {
		t.Fatalf("page2=%s", mustJSON(page2))
	}
	missing := httptest.NewRequest(http.MethodGet, "/api/sessions/ses_missing", nil)
	missing.Host = "127.0.0.1"
	missingRR := httptest.NewRecorder()
	h.ServeHTTP(missingRR, missing)
	if missingRR.Code != http.StatusNotFound {
		t.Fatalf("missing status=%d body=%s", missingRR.Code, missingRR.Body.String())
	}
	bad := httptest.NewRequest(http.MethodGet, "/api/sessions/..%2Fetc", nil)
	bad.Host = "127.0.0.1"
	badRR := httptest.NewRecorder()
	h.ServeHTTP(badRR, bad)
	if badRR.Code != http.StatusBadRequest && badRR.Code != http.StatusNotFound {
		t.Fatalf("malformed status=%d body=%s", badRR.Code, badRR.Body.String())
	}
	cursor := httptest.NewRequest(http.MethodGet, "/api/sessions?cursor=bad", nil)
	cursor.Host = "127.0.0.1"
	cursorRR := httptest.NewRecorder()
	h.ServeHTTP(cursorRR, cursor)
	if cursorRR.Code != http.StatusBadRequest {
		t.Fatalf("cursor status=%d body=%s", cursorRR.Code, cursorRR.Body.String())
	}
}

func TestSessionsAPIContinuationJoinsPreviousResponse(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	provider := &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "req-seed")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	respID := policyResponseID(rr.Body.String())
	if respID == "" {
		t.Fatalf("missing response id body=%s", rr.Body.String())
	}
	follow := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true,"previous_response_id":"`+respID+`"}`))
	follow.Header.Set("Authorization", "Bearer local-secret")
	follow.Header.Set("X-Request-ID", "req-follow")
	followRR := httptest.NewRecorder()
	h.ServeHTTP(followRR, follow)
	if followRR.Code != http.StatusOK {
		t.Fatalf("follow status=%d body=%s", followRR.Code, followRR.Body.String())
	}
	listed := getJSON(t, h, "/api/sessions")
	if len(listed["sessions"].([]any)) != 1 {
		t.Fatalf("list=%s", mustJSON(listed))
	}
	id := listed["sessions"].([]any)[0].(map[string]any)["id"].(string)
	detail := getJSON(t, h, "/api/sessions/"+id)
	if len(detail["requests"].([]any)) != 2 {
		t.Fatalf("detail=%s", mustJSON(detail))
	}
}

func TestSessionsAPIChatMetadataConversationID(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hi"}, {Type: protocol.EventDone}}}},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	body := `{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}],"stream":false,"metadata":{"conversation_id":"conv-gui"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "req-chat-meta")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	listed := getJSON(t, h, "/api/sessions")
	sessionsRaw, _ := listed["sessions"].([]any)
	if len(sessionsRaw) != 1 {
		t.Fatalf("list=%s", mustJSON(listed))
	}
	row := sessionsRaw[0].(map[string]any)
	if row["namespace"] != "chat_completions/metadata/conversation_id" || row["externalId"] != "conv-gui" {
		t.Fatalf("summary=%s", mustJSON(row))
	}
}

func TestSessionsAPIResponsesWithoutIdentitySeedsChain(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}}}},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "req-chain")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	respID := policyResponseID(rr.Body.String())
	if respID == "" {
		t.Fatalf("missing response id body=%s", rr.Body.String())
	}
	listed := getJSON(t, h, "/api/sessions")
	sessionsRaw, _ := listed["sessions"].([]any)
	if len(sessionsRaw) != 1 {
		t.Fatalf("list=%s", mustJSON(listed))
	}
	row := sessionsRaw[0].(map[string]any)
	if row["namespace"] != "responses/chain" {
		t.Fatalf("summary=%s", mustJSON(row))
	}
	if _, ok := row["externalId"]; ok {
		t.Fatalf("unexpected external id %s", mustJSON(row))
	}
	follow := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true,"previous_response_id":"`+respID+`"}`))
	follow.Header.Set("Authorization", "Bearer local-secret")
	follow.Header.Set("X-Request-ID", "req-chain-follow")
	followRR := httptest.NewRecorder()
	h.ServeHTTP(followRR, follow)
	if followRR.Code != http.StatusOK {
		t.Fatalf("follow status=%d body=%s", followRR.Code, followRR.Body.String())
	}
	listed = getJSON(t, h, "/api/sessions")
	if len(listed["sessions"].([]any)) != 1 {
		t.Fatalf("continuation split sessions %s", mustJSON(listed))
	}
	detail := getJSON(t, h, "/api/sessions/"+row["id"].(string))
	if len(detail["requests"].([]any)) != 2 {
		t.Fatalf("detail=%s", mustJSON(detail))
	}
}

func TestSessionsAPIUnprovenResponsesDoesNotCreateSession(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &capturingOpenProvider{err: fmt.Errorf("provider down")}},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	malformed := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{not-json`))
	malformed.Header.Set("Authorization", "Bearer local-secret")
	malformed.Header.Set("X-Request-ID", "req-malformed")
	malformedRR := httptest.NewRecorder()
	h.ServeHTTP(malformedRR, malformed)
	if malformedRR.Code == http.StatusOK {
		t.Fatalf("malformed succeeded %s", malformedRR.Body.String())
	}
	routing := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"combo/missing","store":false,"stream":true}`))
	routing.Header.Set("Authorization", "Bearer local-secret")
	routing.Header.Set("X-Request-ID", "req-routing")
	routingRR := httptest.NewRecorder()
	h.ServeHTTP(routingRR, routing)
	if routingRR.Code == http.StatusOK {
		t.Fatalf("routing succeeded %s", routingRR.Body.String())
	}
	openFail := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	openFail.Header.Set("Authorization", "Bearer local-secret")
	openFail.Header.Set("X-Request-ID", "req-open")
	openRR := httptest.NewRecorder()
	h.ServeHTTP(openRR, openFail)
	if openRR.Code == http.StatusOK {
		t.Fatalf("open succeeded %s", openRR.Body.String())
	}
	listed := getJSON(t, h, "/api/sessions")
	if sessionsRaw, _ := listed["sessions"].([]any); len(sessionsRaw) != 0 {
		t.Fatalf("fabricated sessions %s", mustJSON(listed))
	}
}

func TestSessionsAPIIdentifiedFailureIsRecorded(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{}},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{not-json`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("thread-id", "failed-thread")
	req.Header.Set("X-Request-ID", "req-identified-fail")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code == http.StatusOK {
		t.Fatalf("malformed succeeded %s", rr.Body.String())
	}
	listed := getJSON(t, h, "/api/sessions")
	sessionsRaw, _ := listed["sessions"].([]any)
	if len(sessionsRaw) != 1 {
		t.Fatalf("list=%s", mustJSON(listed))
	}
	row := sessionsRaw[0].(map[string]any)
	if row["namespace"] != "codex/thread" || row["externalId"] != "failed-thread" {
		t.Fatalf("summary=%s", mustJSON(row))
	}
	detail := getJSON(t, h, "/api/sessions/"+row["id"].(string))
	request := detail["requests"].([]any)[0].(map[string]any)
	if request["status"] == float64(200) {
		t.Fatalf("failed request stored as success %s", mustJSON(detail))
	}
}

func TestSessionsAPIChatThreadIDDoesNotJoinCodex(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	provider := &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hi"}, {Type: protocol.EventDone}}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	chat := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}],"stream":false}`))
	chat.Header.Set("Authorization", "Bearer local-secret")
	chat.Header.Set("thread-id", "shared-thread")
	chat.Header.Set("X-Request-ID", "req-chat-thread")
	chatRR := httptest.NewRecorder()
	h.ServeHTTP(chatRR, chat)
	if chatRR.Code != http.StatusOK {
		t.Fatalf("chat status=%d body=%s", chatRR.Code, chatRR.Body.String())
	}
	listed := getJSON(t, h, "/api/sessions")
	if sessionsRaw, _ := listed["sessions"].([]any); len(sessionsRaw) != 0 {
		t.Fatalf("chat thread-id created session %s", mustJSON(listed))
	}
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "shared-thread", "req-codex-thread")
	listed = getJSON(t, h, "/api/sessions")
	sessionsRaw := listed["sessions"].([]any)
	if len(sessionsRaw) != 1 {
		t.Fatalf("list=%s", mustJSON(listed))
	}
	row := sessionsRaw[0].(map[string]any)
	if row["namespace"] != "codex/thread" || row["externalId"] != "shared-thread" || row["requestCount"] != float64(1) {
		t.Fatalf("summary=%s", mustJSON(row))
	}
}

func TestSessionsAPIHealthyEmptyVersusUnavailable(t *testing.T) {
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	healthy, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{}},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	healthy = attachHandlerClose(t, healthy)
	listed := getJSON(t, healthy, "/api/sessions")
	if listed["hasMore"] != false || len(listed["sessions"].([]any)) != 0 {
		t.Fatalf("healthy empty=%s", mustJSON(listed))
	}
	unavail, err := NewHandler(Options{
		DataPlaneToken:      "local-secret",
		Providers:           map[string]Provider{"openai-apikey": &fakeProvider{}},
		SessionsUnavailable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	unavail = attachHandlerClose(t, unavail)
	assertSessionStoreUnavailable(t, unavail, "/api/sessions")
	assertSessionStoreUnavailable(t, unavail, "/api/sessions/ses_missing")
	head := httptest.NewRequest(http.MethodHead, "/api/sessions", nil)
	head.Host = "127.0.0.1"
	headRR := httptest.NewRecorder()
	unavail.ServeHTTP(headRR, head)
	if headRR.Code != http.StatusServiceUnavailable {
		t.Fatalf("head status=%d body=%s", headRR.Code, headRR.Body.String())
	}
	closed, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{}},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	closed = attachHandlerClose(t, closed)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	assertSessionStoreUnavailable(t, closed, "/api/sessions")
	assertSessionStoreUnavailable(t, closed, "/api/sessions/ses_missing")
}

func TestSessionsRecordFailureDoesNotFailRequest(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("thread-id", "closed-store")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func postSession(t *testing.T, h http.Handler, body, threadID, requestID string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("thread-id", threadID)
	req.Header.Set("X-Request-ID", requestID)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func assertSessionStoreUnavailable(t *testing.T, h http.Handler, path string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET %s status=%d body=%s", path, rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	errObj, _ := out["error"].(map[string]any)
	if errObj["code"] != "store_unavailable" {
		t.Fatalf("error=%s", mustJSON(out))
	}
	message, _ := errObj["message"].(string)
	if strings.Contains(strings.ToLower(message), "sqlite") || strings.Contains(message, `\`) || strings.Contains(message, "/") {
		t.Fatalf("leaked store detail %q", message)
	}
}

func getJSON(t *testing.T, h http.Handler, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, rr.Code, rr.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func queryEscape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, ":", "%3A"), " ", "%20")
}

func mustJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
