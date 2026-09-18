package tools

import (
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestMaterializeKeepsSearchDeferredUntilUsed(t *testing.T) {
	catalog, err := Build([]protocol.Tool{
		{Name: "tool_search", ToolSearch: true, Parameters: map[string]any{"type": "object"}},
		{Name: "wait", Parameters: map[string]any{"type": "object"}},
		{Name: "huge", LoadedFromToolSearch: true, Parameters: map[string]any{"type": "object", "properties": map[string]any{"blob": map[string]any{"type": "string", "description": strings.Repeat("x", 100)}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := catalog.Materialize(MaterializeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Strategy != StrategySearch || plan.Deferred != 1 || plan.Materialized != 2 {
		t.Fatalf("plan=%#v", plan)
	}
	if names := toolNames(plan.Tools); strings.Join(names, ",") != "tool_search,wait" {
		t.Fatalf("tools=%v", names)
	}
}

func TestMaterializeNeverDropsARequiredLoadedTool(t *testing.T) {
	catalog, err := Build([]protocol.Tool{
		{Name: "tool_search", ToolSearch: true, Parameters: map[string]any{"type": "object"}},
		{Name: "late", LoadedFromToolSearch: true, Parameters: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := catalog.Materialize(MaterializeOptions{
		Choice: &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: "late"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Materialized != 2 || strings.Join(toolNames(plan.Tools), ",") != "tool_search,late" {
		t.Fatalf("plan=%#v tools=%v", plan, toolNames(plan.Tools))
	}
}

func TestMaterializePinsCodeModeExecWhenFillerWouldExhaustTheBound(t *testing.T) {
	catalog, err := Build([]protocol.Tool{
		{
			Name:        "filler",
			Description: strings.Repeat("z", 80),
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"blob": map[string]any{"type": "string", "description": strings.Repeat("y", 80)},
				},
			},
		},
		{Name: "exec", Freeform: true, Description: "Code Mode"},
	})
	if err != nil {
		t.Fatal(err)
	}
	full, err := catalog.Materialize(MaterializeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := catalog.Materialize(MaterializeOptions{MaxBytes: full.Bytes - 10})
	if err != nil {
		t.Fatal(err)
	}
	names := strings.Join(toolNames(plan.Tools), ",")
	if !strings.Contains(names, "exec") {
		t.Fatalf("code-mode exec dropped under bound: plan=%#v tools=%v", plan, names)
	}
	if strings.Contains(names, "filler") && !strings.Contains(names, "exec") {
		t.Fatal("unreachable")
	}
}

func TestMaterializeFailsClosedWhenRequiredSchemasExceedBound(t *testing.T) {
	catalog, err := Build([]protocol.Tool{{
		Name: "only",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"blob": map[string]any{"type": "string", "description": strings.Repeat("y", 200)},
			},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = catalog.Materialize(MaterializeOptions{
		MaxBytes: 32,
		Choice:   &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: "only"},
	})
	if err == nil {
		t.Fatal("expected bound error")
	}
}

func toolNames(tools []protocol.Tool) []string {
	out := make([]string, 0, len(tools))
	for _, tool := range tools {
		out = append(out, WireName(tool))
	}
	return out
}
