package contextprojection

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestArtifactRefUsesBenesDomainAndOccurrence(t *testing.T) {
	first := ArtifactRef(Identity{ToolCallID: "call-1", ToolNamespace: "tools", ToolName: "exec", OccurrenceOrdinal: 0})
	second := ArtifactRef(Identity{ToolCallID: "call-1", ToolNamespace: "tools", ToolName: "exec", OccurrenceOrdinal: 1})
	shifted := ArtifactRef(Identity{ToolCallID: "call-1", ToolNamespace: "tools", ToolName: "exec", OccurrenceOrdinal: 0})
	if first == second {
		t.Fatal("occurrence ordinal must change the ref")
	}
	if first != shifted {
		t.Fatal("message index is not part of identity")
	}
	if !strings.HasPrefix(first, "ctx_") || utf8.RuneCountInString(first) != 4+24 {
		t.Fatalf("ref=%q", first)
	}
	again := ArtifactRef(Identity{ToolCallID: "call-1", ToolNamespace: "tools", ToolName: "exec", OccurrenceOrdinal: 0})
	if again != first {
		t.Fatal("ref must be deterministic")
	}
}

func TestValidIdentityInputTrims(t *testing.T) {
	if ValidIdentityInput(" ", "exec") || ValidIdentityInput("call", "") || ValidIdentityInput("call", "  ") {
		t.Fatal("blank identities must be rejected")
	}
	if !ValidIdentityInput("call", "exec") {
		t.Fatal("expected valid identity")
	}
}

func TestIdentityKeyJoinsNamespace(t *testing.T) {
	if IdentityKey("a", "ns", "t") == IdentityKey("a", "", "t") {
		t.Fatal("namespace must participate")
	}
}
