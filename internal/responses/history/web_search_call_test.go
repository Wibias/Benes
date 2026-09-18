package history

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/request"
	"github.com/Wibias/Benes/internal/responses/websearchcall"
)

func TestBuildHealsWebSearchCallQueryShapes(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		id      string
		query   string
		queries []string
	}{
		{
			name:    "query only",
			raw:     `{"type":"web_search_call","id":"ws_q","action":{"type":"search","query":"alpha"}}`,
			id:      "ws_q",
			query:   "alpha",
			queries: []string{"alpha"},
		},
		{
			name:    "queries only",
			raw:     `{"type":"web_search_call","id":"ws_qs","action":{"type":"search","queries":["one","two"]}}`,
			id:      "ws_qs",
			query:   "one",
			queries: []string{"one", "two"},
		},
		{
			name:    "both",
			raw:     `{"type":"web_search_call","id":"ws_both","action":{"query":"one","queries":["one","two"]}}`,
			id:      "ws_both",
			query:   "one",
			queries: []string{"one", "two"},
		},
		{
			name:    "empty queries keeps query",
			raw:     `{"type":"web_search_call","id":"ws_legacy","action":{"query":"legacy","queries":[]}}`,
			id:      "ws_legacy",
			query:   "legacy",
			queries: []string{"legacy"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := parse(t, request.Input{Items: []request.Item{item(t, tc.raw)}})
			if len(ctx.Messages) != 1 || ctx.Messages[0].Role != protocol.RoleAssistant {
				t.Fatalf("messages=%#v", ctx.Messages)
			}
			if len(ctx.Messages[0].Content) != 1 {
				t.Fatalf("content=%#v", ctx.Messages[0].Content)
			}
			part := ctx.Messages[0].Content[0]
			if !websearchcall.IsHosted(part) || part.ToolCallID != tc.id || part.ToolName != "web_search" {
				t.Fatalf("part=%#v", part)
			}
			if part.Arguments["query"] != tc.query || !reflect.DeepEqual(part.Arguments["queries"], tc.queries) {
				t.Fatalf("args=%#v", part.Arguments)
			}
		})
	}
}

func TestBuildRejectsMalformedWebSearchCall(t *testing.T) {
	tests := []string{
		`{"type":"web_search_call","id":"ws"}`,
		`{"type":"web_search_call","action":{"query":"a"}}`,
		`{"type":"web_search_call","id":"ws","action":{"queries":[]}}`,
		`{"type":"web_search_call","id":"ws","action":{"queries":[42]}}`,
		`{"type":"web_search_call","id":"ws","action":{"queries":["a",42]}}`,
		`{"type":"web_search_call","id":"ws","action":{"query":"a","queries":["b"]}}`,
		`{"type":"web_search_call","id":"ws","action":{"type":"open_page","query":"a"}}`,
	}
	for _, raw := range tests {
		_, err := Build(&request.Request{Model: "m", Input: request.Input{Items: []request.Item{item(t, raw)}}}, 1)
		if !errors.Is(err, websearchcall.ErrMalformedAction) {
			t.Fatalf("raw=%s err=%v", raw, err)
		}
	}
}

func TestBuildDoesNotInsertDuplicateWebSearchCall(t *testing.T) {
	ctx := parse(t, request.Input{Items: []request.Item{
		item(t, `{"type":"web_search_call","id":"ws_1","action":{"query":"alpha"}}`),
	}})
	if len(ctx.Messages) != 1 || len(ctx.Messages[0].Content) != 1 {
		t.Fatalf("messages=%#v", ctx.Messages)
	}
	if ctx.Messages[0].Role == protocol.RoleToolResult {
		t.Fatal("replay inserted a tool result")
	}
}
