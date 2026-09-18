package google

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
)

func googleTestAuthority(t *testing.T) *continuation.Authority {
	t.Helper()
	store := continuation.NewStore(continuation.StoreLimits{
		MaxEntryBytes: 1 << 20, MaxTotalBytes: 4 << 20, MaxEntries: 32, TTL: time.Hour,
	}, time.Now)
	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = 'g'
	}
	authority, err := continuation.NewAuthority(store, nil, salt)
	if err != nil {
		t.Fatal(err)
	}
	return authority
}

func googleTestTurn(t *testing.T) *resourcebudget.Turn {
	t.Helper()
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns: 4, MaxProcessBytes: 4 << 20, MaxTurnBytes: 4 << 20,
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 4 << 20},
	})
	turn, err := budget.AcquireTurn(context.Background(), "thread")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turn.Close() })
	return turn
}

func TestThoughtSignatureDoesNotReplayAcrossStudioAndVertex(t *testing.T) {
	authority := googleTestAuthority(t)
	turn := googleTestTurn(t)
	studio, err := New(context.Background(), Config{APIKey: "gk-studio", Continuation: authority})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := studio.bindContinuation(providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{PreviousResponseID: "thread-1", ModelID: "gemini-3.5-flash"},
		Turn:   turn,
	}, Identity{PublicID: "gemini-3.5-flash", WireID: "gemini-3.5-flash"}, "gk-studio")
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.RememberSignature(bound.Owner, bound.Durable, "thread-1", "c1", "sig-studio"); err != nil {
		t.Fatal(err)
	}
	bound.Release()

	var studioBody, vertexBody map[string]any
	studioUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&studioBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\n\n")
	}))
	t.Cleanup(studioUp.Close)
	vertexUp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&vertexBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\n\n")
	}))
	t.Cleanup(vertexUp.Close)

	studio.httpClient = studioUp.Client()
	studio.testOrigin = studioUp.URL
	req := providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			ModelID:            "gemini-3.5-flash",
			PreviousResponseID: "thread-1",
			Context: protocol.Context{Messages: []protocol.Message{
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
				{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
					Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup", Arguments: map[string]any{"q": "x"},
				}}},
				{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "out"}}},
			}},
		},
	}
	stream, err := studio.Open(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()

	vertex, err := New(context.Background(), Config{
		Kind: KindVertex, Project: "proj", Location: "us-central1", AccessToken: "tok-vertex",
		Continuation: authority, HTTPClient: vertexUp.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	vertex.testOrigin = vertexUp.URL
	stream, err = vertex.Open(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()

	studioSig := thoughtSigFromBody(studioBody)
	vertexSig := thoughtSigFromBody(vertexBody)
	if studioSig != "sig-studio" {
		t.Fatalf("studio should replay owned signature, got %q body=%#v", studioSig, studioBody)
	}
	if vertexSig != "" {
		t.Fatalf("vertex replayed studio signature %q", vertexSig)
	}
}

func TestThoughtSignatureDoesNotReplayAcrossCredentials(t *testing.T) {
	authority := googleTestAuthority(t)
	turn := googleTestTurn(t)
	var bodies []map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\n\n")
	}))
	t.Cleanup(up.Close)
	first, err := New(context.Background(), Config{APIKey: "gk-a", Continuation: authority, HTTPClient: up.Client()})
	if err != nil {
		t.Fatal(err)
	}
	first.testOrigin = up.URL
	bound, err := first.bindContinuation(providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{PreviousResponseID: "thread-1", ModelID: "gemini-3.5-flash"},
		Turn:   turn,
	}, Identity{PublicID: "gemini-3.5-flash", WireID: "gemini-3.5-flash"}, "gk-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.RememberSignature(bound.Owner, bound.Durable, "thread-1", "c1", "sig-a"); err != nil {
		t.Fatal(err)
	}
	bound.Release()

	req := providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			ModelID:            "gemini-3.5-flash",
			PreviousResponseID: "thread-1",
			Context: protocol.Context{Messages: []protocol.Message{
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
				{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
					Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup", Arguments: map[string]any{"q": "x"},
				}}},
				{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "out"}}},
			}},
		},
	}
	second, err := New(context.Background(), Config{APIKey: "gk-b", Continuation: authority, HTTPClient: up.Client()})
	if err != nil {
		t.Fatal(err)
	}
	second.testOrigin = up.URL
	stream, err := second.Open(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
	if len(bodies) != 1 || thoughtSigFromBody(bodies[0]) != "" {
		t.Fatalf("foreign key replayed signature bodies=%#v", bodies)
	}
}

func TestVertexOpenStoresADCTokenInContinuationSecret(t *testing.T) {
	globalADC = adcCache{}
	path := filepath.Join(t.TempDir(), "adc.json")
	if err := os.WriteFile(path, []byte(`{"type":"authorized_user","client_id":"cid","client_secret":"csec","refresh_token":"rt"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\n\n")
	}))
	t.Cleanup(up.Close)
	client, err := New(context.Background(), Config{
		Kind: KindVertex, Project: "proj", Location: "us-central1", HTTPClient: up.Client(),
		ADC: ADCEnv{
			Environ: map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": path},
			HTTP: &http.Client{Transport: adcRoundTrip(func(req *http.Request) (*http.Response, error) {
				return adcJSONResponse(200, map[string]any{"access_token": "ya29.secret", "expires_in": 3600}), nil
			})},
			Now:   func() time.Time { return time.Unix(1_700_000_000, 0) },
			Sleep: func(time.Duration) {},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.testOrigin = up.URL
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		ModelID: "gemini-3.7-flash",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
		}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
	secret := string(continuationSecret(client))
	if !strings.Contains(secret, "ya29.secret") {
		t.Fatalf("secret=%q", secret)
	}
	if continuationAuthClass(client) != "bearer" {
		t.Fatalf("auth=%s", continuationAuthClass(client))
	}
}

func thoughtSigFromBody(body map[string]any) string {
	contents, _ := body["contents"].([]any)
	if len(contents) < 2 {
		return ""
	}
	msg, _ := contents[1].(map[string]any)
	parts, _ := msg["parts"].([]any)
	if len(parts) == 0 {
		return ""
	}
	part, _ := parts[0].(map[string]any)
	sig, _ := part["thoughtSignature"].(string)
	return sig
}
