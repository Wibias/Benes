package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestResponsesReasoningSummaryIntentControlsRawReasoningVisibility(t *testing.T) {
	t.Run("hidden by default", func(t *testing.T) {
		provider := &fakeProvider{events: []protocol.Event{
			{Type: protocol.EventReasoningRawDelta, Text: "private thought"},
			{Type: protocol.EventDone},
		}}
		h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": provider}})
		if err != nil {
			t.Fatal(err)
		}
		h = attachHandlerClose(t, h)
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/deepseek-v4","store":false,"stream":true,"input":"hi"}`))
		req.Header.Set("Authorization", "Bearer local-secret")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		if strings.Contains(body, "response.reasoning_text.delta") || strings.Contains(body, "response.reasoning_summary_text.delta") {
			t.Fatalf("hidden reasoning became visible: %s", body)
		}
		if !strings.Contains(body, `"encrypted_content":"benesr1:`) {
			t.Fatalf("hidden reasoning replay envelope missing: %s", body)
		}
	})

	t.Run("requested summary is expandable", func(t *testing.T) {
		provider := &fakeProvider{events: []protocol.Event{
			{Type: protocol.EventReasoningRawDelta, Text: "visible reasoning"},
			{Type: protocol.EventDone},
		}}
		h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": provider}})
		if err != nil {
			t.Fatal(err)
		}
		h = attachHandlerClose(t, h)
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/deepseek-v4","store":false,"stream":true,"input":"hi","reasoning":{"effort":"high","summary":"detailed"}}`))
		req.Header.Set("Authorization", "Bearer local-secret")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		if !strings.Contains(body, "response.reasoning_summary_part.added") || !strings.Contains(body, "response.reasoning_summary_text.delta") {
			t.Fatalf("requested summary lifecycle missing: %s", body)
		}
		if strings.Contains(body, "response.reasoning_text.delta") || strings.Contains(body, `"type":"reasoning_text"`) {
			t.Fatalf("requested summary leaked content-channel reasoning: %s", body)
		}
	})
}
