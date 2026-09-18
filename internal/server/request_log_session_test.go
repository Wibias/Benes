package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/sessions"
)

func TestLogsAPIAttachesDurableSessionID(t *testing.T) {
	h, store := newSessionHandler(t)
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "thread-logs", "client-a")
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "thread-logs", "client-b")
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "thread-other", "client-c")

	listed := getJSON(t, h, "/api/sessions")
	rows := listed["sessions"].([]any)
	if len(rows) != 2 {
		t.Fatalf("sessions=%s", mustJSON(listed))
	}
	firstID := rows[0].(map[string]any)["id"].(string)
	secondID := rows[1].(map[string]any)["id"].(string)

	entries := getLogEntries(t, h, "/api/logs")
	grouped := map[string]int{}
	for _, entry := range entries {
		if entry.Path != "/v1/responses" {
			continue
		}
		if entry.SessionID == "" {
			t.Fatalf("grouped request missing sessionId %#v", entry)
		}
		if !sessions.ValidSessionID(entry.SessionID) {
			t.Fatalf("sessionId=%q", entry.SessionID)
		}
		grouped[entry.SessionID]++
	}
	if grouped[firstID] < 1 || grouped[secondID] < 1 {
		t.Fatalf("grouped=%v first=%s second=%s logs=%s", grouped, firstID, secondID, mustJSON(entries))
	}

	filtered := getLogEntries(t, h, "/api/logs?sessionId="+firstID)
	if len(filtered) == 0 {
		t.Fatal("expected filtered logs")
	}
	for _, entry := range filtered {
		if entry.SessionID != firstID {
			t.Fatalf("filter leaked %#v", entry)
		}
	}

	unknown := getLogEntries(t, h, "/api/logs?sessionId=ses_deadbeefdeadbeefdeadbeefdeadbeef")
	if len(unknown) != 0 {
		t.Fatalf("unknown session logs=%s", mustJSON(unknown))
	}

	malformed := httptest.NewRequest(http.MethodGet, "/api/logs?sessionId=not-a-session", nil)
	malformed.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, malformed)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("malformed status=%d body=%s", rr.Code, rr.Body.String())
	}

	_ = store
}

func TestLogsAPIOmitsSessionIDWhenUngroupedOrDenied(t *testing.T) {
	h, _ := newSessionHandler(t)
	chat := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}]}`))
	chat.Header.Set("Authorization", "Bearer local-secret")
	chatRR := httptest.NewRecorder()
	h.ServeHTTP(chatRR, chat)
	if chatRR.Code != http.StatusOK {
		t.Fatalf("chat status=%d body=%s", chatRR.Code, chatRR.Body.String())
	}

	denied := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`))
	denied.Header.Set("Authorization", "Bearer wrong")
	denied.Header.Set("thread-id", "denied-thread")
	deniedRR := httptest.NewRecorder()
	h.ServeHTTP(deniedRR, denied)
	if deniedRR.Code == http.StatusOK {
		t.Fatal("denied succeeded")
	}

	entries := getLogEntries(t, h, "/api/logs")
	foundChat := false
	foundDenied := false
	for _, entry := range entries {
		if entry.Path == "/v1/chat/completions" {
			foundChat = true
			if entry.SessionID != "" {
				t.Fatalf("ungrouped chat has sessionId %#v", entry)
			}
		}
		if entry.Path == "/v1/responses" && entry.Status != http.StatusOK {
			foundDenied = true
			if entry.SessionID != "" {
				t.Fatalf("denied request has sessionId %#v", entry)
			}
		}
	}
	if !foundChat || !foundDenied {
		t.Fatalf("missing ungrouped/denied logs %s", mustJSON(entries))
	}

	listed := getJSON(t, h, "/api/sessions")
	if rows, _ := listed["sessions"].([]any); len(rows) != 0 {
		t.Fatalf("ungrouped/denied created sessions %s", mustJSON(listed))
	}
}

func TestLogsAPIStoreFailureDoesNotSuppressRequestLog(t *testing.T) {
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
	entries := getLogEntries(t, h, "/api/logs")
	found := false
	for _, entry := range entries {
		if entry.Path == "/v1/responses" {
			found = true
			if entry.SessionID != "" {
				t.Fatalf("failed persist still linked %#v", entry)
			}
		}
	}
	if !found {
		t.Fatalf("missing request log %s", mustJSON(entries))
	}
}

func TestLogsAPISessionFilterComposesWithStatus(t *testing.T) {
	h, _ := newSessionHandler(t)
	postSession(t, h, `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "ok-thread", "ok-req")
	listed := getJSON(t, h, "/api/sessions")
	id := listed["sessions"].([]any)[0].(map[string]any)["id"].(string)
	ok := getLogEntries(t, h, "/api/logs?sessionId="+id+"&status=200")
	if len(ok) == 0 {
		t.Fatal("expected 200 logs for session")
	}
	none := getLogEntries(t, h, "/api/logs?sessionId="+id+"&status=503")
	if len(none) != 0 {
		t.Fatalf("status filter leaked %s", mustJSON(none))
	}
}

func newSessionHandler(t *testing.T) (http.Handler, *sessions.Store) {
	t.Helper()
	home := t.TempDir()
	store, err := sessions.Open(filepath.Join(home, "sessions.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 4, OutputTokens: 2}}}}},
		Sessions:       store,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	return h, store
}

func getLogEntries(t *testing.T, h http.Handler, path string) []requestLogEntry {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, rr.Code, rr.Body.String())
	}
	var entries []requestLogEntry
	if err := json.Unmarshal(rr.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if entries == nil {
		return []requestLogEntry{}
	}
	return entries
}
