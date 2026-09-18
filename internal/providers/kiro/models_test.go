package kiro

import "testing"

func TestNormalizeModelIDAndKnownWindows(t *testing.T) {
	if got := NormalizeModelID("Kiro/claude-4-5-sonnet-high"); got != "claude-sonnet-4.5" {
		t.Fatalf("got=%s", got)
	}
	if got := NormalizeModelID("kiro-auto"); got != "auto" {
		t.Fatalf("auto=%s", got)
	}
	if ContextWindow("claude-sonnet-4.5") != 200_000 {
		t.Fatal("known window")
	}
	if ContextWindow("auto") != 0 || ContextWindow("unknown-model") != 0 {
		t.Fatal("unknown/auto must not invent a window")
	}
}
