package websearch

import (
	"reflect"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestReplaceHostedToolsInjectsCanonicalWebSearchFunction(t *testing.T) {
	got := ReplaceHostedTools([]protocol.Tool{
		{Name: "lookup", Description: "find"},
		{Name: "web_search_preview", HostedWebSearch: true},
		{Name: "web_search", HostedWebSearch: true},
	})
	if len(got) != 2 {
		t.Fatalf("tools=%#v", got)
	}
	if got[0].Name != "lookup" || got[0].HostedWebSearch {
		t.Fatalf("passthrough=%#v", got[0])
	}
	tool := got[1]
	if tool.Name != "web_search" || tool.Description == "" || tool.HostedWebSearch || !tool.SidecarWebSearch {
		t.Fatalf("synthetic=%#v", tool)
	}
	wantKinds := []protocol.HostedWebSearchKind{protocol.HostedWebSearchPreview, protocol.HostedWebSearchWebSearch}
	if !reflect.DeepEqual(tool.SidecarWebSearchKinds, wantKinds) {
		t.Fatalf("sidecar kinds=%#v want=%#v", tool.SidecarWebSearchKinds, wantKinds)
	}
	props, _ := tool.Parameters["properties"].(map[string]any)
	if props["query"] == nil || props["queries"] == nil {
		t.Fatalf("parameters=%#v", tool.Parameters)
	}
}

func TestReplaceHostedToolsLeavesOrdinaryToolsUnchanged(t *testing.T) {
	in := []protocol.Tool{{Name: "lookup"}}
	got := ReplaceHostedTools(in)
	if len(got) != 1 || got[0].Name != "lookup" {
		t.Fatalf("got=%#v", got)
	}
}
