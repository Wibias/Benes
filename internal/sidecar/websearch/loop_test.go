package websearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

type sliceStream struct{ events []protocol.Event }

func (s *sliceStream) Next() (protocol.Event, error) {
	if len(s.events) == 0 {
		return protocol.Event{}, io.EOF
	}
	ev := s.events[0]
	s.events = s.events[1:]
	return ev, nil
}
func (s *sliceStream) Close() error { return nil }

func TestLoopExecutesWebSearchToolCall(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&map[string]any{})
		_, _ = w.Write([]byte(`{"sources":[{"url":"https://example.com","title":"Example"}]}`))
	}))
	t.Cleanup(upstream.Close)
	client, err := New(Config{
		Enabled: true, ProviderID: "opencode-zen", Endpoint: upstream.URL,
		AuthClass: "key", Wire: "openai-responses", ModelID: "deepseek-v4-flash",
		AllowedModels: []string{"deepseek-v4-flash"}, APIKey: "sidecar-key", HTTPClient: upstream.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	loop := Wrap(context.Background(), &sliceStream{events: []protocol.Event{
		{Type: protocol.EventToolCallEnd, ID: "c1", Name: "web_search", Arguments: `{"query":"benes"}`},
		{Type: protocol.EventDone},
	}}, client)
	begin, err := loop.Next()
	if err != nil || begin.Type != protocol.EventWebSearchCallBegin || begin.ID != "c1" {
		t.Fatalf("begin=%#v err=%v", begin, err)
	}
	end, err := loop.Next()
	if err != nil || end.Type != protocol.EventWebSearchCallEnd || end.Status != "completed" || len(end.Sources) != 1 {
		t.Fatalf("end=%#v err=%v", end, err)
	}
	done, err := loop.Next()
	if err != nil || done.Type != protocol.EventDone {
		t.Fatalf("done=%#v err=%v", done, err)
	}
	_ = providers.EventStream(loop)
}

func TestCitationsDropsUnsafeSources(t *testing.T) {
	got := citations([]Source{
		{URL: "javascript:alert(1)", Title: "xss"},
		{URL: "https://example.com", Title: "Example"},
	})
	if len(got) != 1 || got[0].URL != "https://example.com" || got[0].Title != "Example" {
		t.Fatalf("citations=%#v", got)
	}
}
