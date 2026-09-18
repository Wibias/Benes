package websearch

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

type Stream struct {
	inner   providers.EventStream
	client  *Client
	ctx     context.Context
	pending []protocol.Event
}

func Wrap(ctx context.Context, inner providers.EventStream, client *Client) providers.EventStream {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Stream{inner: inner, client: client, ctx: ctx}
}

func (s *Stream) Next() (protocol.Event, error) {
	if s == nil {
		return protocol.Event{}, nil
	}
	if len(s.pending) > 0 {
		ev := s.pending[0]
		s.pending = s.pending[1:]
		return ev, nil
	}
	if s.inner == nil {
		return protocol.Event{}, nil
	}
	ev, err := s.inner.Next()
	if err != nil {
		return ev, err
	}
	if !isWebSearchTool(ev) {
		return ev, nil
	}
	s.pending = append(s.pending, protocol.Event{Type: protocol.EventWebSearchCallBegin, ID: ev.ID})
	query := queryFromArguments(ev.Arguments)
	result, searchErr := s.client.Search(s.ctx, query)
	end := protocol.Event{Type: protocol.EventWebSearchCallEnd, ID: ev.ID, Queries: []string{query}, Status: "completed"}
	if searchErr != nil {
		end.Status = "failed"
		end.Message = searchErr.Error()
	} else {
		end.Sources = citations(result.Sources)
	}
	s.pending = append(s.pending, end)
	ev = s.pending[0]
	s.pending = s.pending[1:]
	return ev, nil
}

func (s *Stream) Close() error {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

// PhysicalOwnership forwards the inner provider pin. Wrap is identity-transparent.
func (s *Stream) PhysicalOwnership() *providers.PhysicalPin {
	if s == nil || s.inner == nil {
		return nil
	}
	if reporter, ok := s.inner.(interface{ PhysicalOwnership() *providers.PhysicalPin }); ok {
		return reporter.PhysicalOwnership()
	}
	return nil
}

func isWebSearchTool(ev protocol.Event) bool {
	if ev.Type != protocol.EventToolCallEnd {
		return false
	}
	name := strings.ToLower(strings.TrimSpace(ev.Name))
	return name == "web_search" || strings.HasPrefix(name, "web_search_")
}

func queryFromArguments(raw string) string {
	var payload struct {
		Query string `json:"query"`
	}
	if json.Unmarshal([]byte(raw), &payload) == nil && strings.TrimSpace(payload.Query) != "" {
		return payload.Query
	}
	return strings.TrimSpace(raw)
}

func citations(sources []Source) []protocol.URLCitation {
	safe := SanitizeSources(sources)
	out := make([]protocol.URLCitation, 0, len(safe))
	for _, source := range safe {
		out = append(out, protocol.URLCitation{URL: source.URL, Title: source.Title})
	}
	return out
}
