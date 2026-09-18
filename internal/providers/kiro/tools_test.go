package kiro

import "testing"

func TestToolNameRegistryAliasesAndRestores(t *testing.T) {
	reg := NewToolNameRegistry()
	alias, err := reg.Alias("workspace agents_create_agent")
	if err != nil || alias == "workspace agents_create_agent" || stringsContainsSpace(alias) {
		t.Fatalf("alias=%q err=%v", alias, err)
	}
	again, err := reg.Alias("workspace agents_create_agent")
	if err != nil || again != alias {
		t.Fatalf("stable=%q", again)
	}
	if got := reg.Restore(alias); got != "workspace agents_create_agent" {
		t.Fatalf("restore=%q", got)
	}
	if _, err := reg.Alias(completionToolName); err == nil {
		t.Fatal("reserved")
	}
}

func stringsContainsSpace(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			return true
		}
	}
	return false
}
