package tools

import (
	"errors"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestBuildFlattensNamespacedToolsAndDetectsCodeMode(t *testing.T) {
	catalog, err := Build([]protocol.Tool{
		{Name: "exec", Freeform: true},
		{Name: "lookup", Namespace: "mcp"},
		{Name: "apply_patch", Freeform: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if catalog.entries[0].WireName != "exec" || !catalog.entries[0].CodeMode {
		t.Fatalf("exec=%#v", catalog.entries[0])
	}
	if catalog.entries[1].WireName != "mcp__lookup" || catalog.entries[1].CodeMode {
		t.Fatalf("lookup=%#v", catalog.entries[1])
	}
	if catalog.entries[2].CodeMode {
		t.Fatal("apply_patch must not be Code Mode")
	}
}

func TestStructuredExecAndBareShellAreNotCodeMode(t *testing.T) {
	structured, err := Build([]protocol.Tool{{Name: "exec", Parameters: map[string]any{"type": "object"}}})
	if err != nil {
		t.Fatal(err)
	}
	if structured.entries[0].CodeMode {
		t.Fatal("structured exec")
	}
	withShell, err := Build([]protocol.Tool{
		{Name: "exec", Freeform: true},
		{Name: "shell"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if withShell.entries[0].CodeMode {
		t.Fatal("flat bridge exec")
	}
}

func TestBuildRejectsWireCollisionsAndAmbiguousChoice(t *testing.T) {
	if _, err := Build([]protocol.Tool{{Name: "read", Namespace: "mcp"}, {Name: "mcp__read"}}); !errors.Is(err, ErrCollision) {
		t.Fatalf("collision=%v", err)
	}
	catalog, err := Build([]protocol.Tool{{Name: "read"}, {Name: "read", Namespace: "mcp"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ResolveChoice("read"); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("ambiguous=%v", err)
	}
	got, err := catalog.ResolveChoice("mcp.read")
	if err != nil || got != "mcp__read" {
		t.Fatalf("dotted=%q err=%v", got, err)
	}
	if _, err := catalog.ResolveChoice("missing"); !errors.Is(err, ErrUndeclared) {
		t.Fatalf("missing=%v", err)
	}
}

func TestWireForCallUsesCustomAliasOnlyWhenDeclared(t *testing.T) {
	catalog, err := Build([]protocol.Tool{{Name: "search", ToolSearch: true}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := catalog.WireForCall(protocol.ContentPart{ToolName: "search", CustomWireName: "search"})
	if err != nil || got != "search" {
		t.Fatalf("declared alias=%q err=%v", got, err)
	}
	if _, err := catalog.WireForCall(protocol.ContentPart{ToolName: "search", CustomWireName: "undeclared"}); !errors.Is(err, ErrUndeclared) {
		t.Fatalf("undeclared alias=%v", err)
	}
}

func TestCodeModeExecBridgesProviderHelperNames(t *testing.T) {
	catalog, err := Build([]protocol.Tool{{Name: "exec", Freeform: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shell", "bash", "command", "exec_command", "shell_command", "apply_patch"} {
		got, err := catalog.WireForCall(protocol.ContentPart{ToolName: name})
		if err != nil || got != "exec" {
			t.Fatalf("helper %q: got=%q err=%v", name, got, err)
		}
	}
	if _, err := catalog.WireForCall(protocol.ContentPart{ToolName: "lookup"}); !errors.Is(err, ErrUndeclared) {
		t.Fatalf("unrelated tool=%v", err)
	}
}

func TestDeclaredShellKeepsExactNameAndDoesNotBridge(t *testing.T) {
	catalog, err := Build([]protocol.Tool{
		{Name: "exec", Freeform: true},
		{Name: "shell"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := catalog.WireForCall(protocol.ContentPart{ToolName: "shell"})
	if err != nil || got != "shell" {
		t.Fatalf("declared shell=%q err=%v", got, err)
	}
	if _, err := catalog.WireForCall(protocol.ContentPart{ToolName: "apply_patch"}); !errors.Is(err, ErrUndeclared) {
		t.Fatalf("helpers must not bridge when code mode is off: %v", err)
	}
}
