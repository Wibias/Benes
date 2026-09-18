package bridge

import (
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestRepairToolArgumentsStringOnlyBareInteger(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"cell_id":       map[string]any{"type": "string"},
			"yield_time_ms": map[string]any{"type": "integer"},
			"label":         map[string]any{"type": []any{"string", "null"}},
			"loose":         map[string]any{"type": []any{"integer", "string"}},
		},
	}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "reported wait call", in: `{"cell_id":4,"yield_time_ms":10000}`, want: `{"cell_id":"4","yield_time_ms":10000}`},
		{name: "string null union", in: `{"label":12}`, want: `{"label":"12"}`},
		{name: "numeric union stays numeric", in: `{"loose":4}`, want: `{"loose":4}`},
		{name: "fraction stays invalid for string", in: `{"cell_id":4.5}`, want: `{"cell_id":4.5}`},
		{name: "unsafe integer keeps bytes", in: `{"cell_id":18446744073709551615}`, want: `{"cell_id":18446744073709551615}`},
		{name: "valid string keeps bytes", in: `{ "cell_id" : "4", "yield_time_ms" : 10000 }`, want: `{ "cell_id" : "4", "yield_time_ms" : 10000 }`},
		{name: "repairs compose", in: `{"cell_id":4,"yield_time_ms":120000.0}`, want: `{"cell_id":"4","yield_time_ms":120000}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := repairToolArguments(tc.in, schema); got != tc.want {
				t.Fatalf("repairToolArguments()=%q want %q", got, tc.want)
			}
		})
	}
}

func TestRepairToolArgumentsNestedSchemasAndLocalRefs(t *testing.T) {
	schema := map[string]any{
		"type":  "object",
		"$defs": map[string]any{"identifier": map[string]any{"type": "string"}},
		"properties": map[string]any{
			"page": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"$ref": "#/$defs/identifier"}}},
			"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	in := `{"page":{"id":7},"tags":[1,2]}`
	want := `{"page":{"id":"7"},"tags":["1","2"]}`
	if got := repairToolArguments(in, schema); got != want {
		t.Fatalf("repairToolArguments()=%q want %q", got, want)
	}
}

func TestBridgeRepairsCompletedFunctionArgumentsAgainstDeclaredSchema(t *testing.T) {
	b := New("gpt-5", Options{
		ResponseID: "resp_1",
		ID:         func(string) string { return "fc_1" },
		Tools: []protocol.Tool{{Name: "wait", Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"cell_id":       map[string]any{"type": "string"},
				"yield_time_ms": map[string]any{"type": "integer"},
			},
		}}},
	})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallStart, ID: "call_1", Name: "wait"})
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallDelta, Arguments: `{"cell_id":4,"yield_time_ms":120000.0}`})
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallEnd})
	item := object(t, frames[len(frames)-1].Data["item"])
	if got := item["arguments"]; got != `{"cell_id":"4","yield_time_ms":120000}` {
		t.Fatalf("arguments=%v", got)
	}
}

func TestBridgeLeavesArgumentsUntouchedWithoutMatchingToolSchema(t *testing.T) {
	b := New("gpt-5", Options{
		ResponseID: "resp_1",
		ID:         func(string) string { return "fc_1" },
		Tools:      []protocol.Tool{{Name: "other", Parameters: map[string]any{"type": "object"}}},
	})
	_ = b.Start()
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallStart, ID: "call_1", Name: "wait"})
	_ = mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallDelta, Arguments: `{ "cell_id" : 4 }`})
	frames := mustHandle(t, b, protocol.Event{Type: protocol.EventToolCallEnd})
	item := object(t, frames[len(frames)-1].Data["item"])
	if got := item["arguments"]; got != `{ "cell_id" : 4 }` {
		t.Fatalf("arguments=%v", got)
	}
}
