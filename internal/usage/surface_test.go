package usage

import (
	"testing"
	"time"
)

func TestExplicitSurfacesAreAttributedAndFilterable(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	entries := []entry{
		{Timestamp: now, Provider: "openai", Model: "gpt-5", Surface: "codex", UsageStatus: "reported"},
		{Timestamp: now, Provider: "anthropic", Model: "claude-opus", Surface: "claude", UsageStatus: "reported"},
		{Timestamp: now, Provider: "anthropic", Model: "claude-sonnet", Surface: "claude-desktop", UsageStatus: "reported"},
		{Timestamp: now, Provider: "xai", Model: "grok-4", Surface: "grok", UsageStatus: "reported"},
		{Timestamp: now, Provider: "openai-apikey", Model: "gpt-5.5", UsageStatus: "reported"},
	}

	all := SummarizeQuery(entries, Query{Range: RangeAll, Now: now}, NewTable())
	if all.Summary.Requests != 5 {
		t.Fatalf("all=%d", all.Summary.Requests)
	}
	if all.SurfaceAttribution != (SurfaceCounts{Codex: 1, Claude: 1, ClaudeDesktop: 1, Grok: 1, Unattributed: 1}) {
		t.Fatalf("attribution=%+v", all.SurfaceAttribution)
	}

	codex := SummarizeQuery(entries, Query{Range: RangeAll, Surface: SurfaceCodex, Now: now}, NewTable())
	if codex.Summary.Requests != 1 || !modelPresent(codex, "gpt-5") {
		t.Fatalf("explicit codex=%+v", codex.Summary)
	}
	if modelPresent(codex, "gpt-5.5") {
		t.Fatal("surface=codex selected unattributed traffic")
	}

	claude := SummarizeQuery(entries, Query{Range: RangeAll, Surface: SurfaceClaude, Now: now}, NewTable())
	if claude.Summary.Requests != 2 {
		t.Fatalf("claude filter=%d", claude.Summary.Requests)
	}
	if !modelPresent(claude, "claude-opus") || !modelPresent(claude, "claude-sonnet") {
		t.Fatal("claude filter missed an explicit Claude surface")
	}

	grok := SummarizeQuery(entries, Query{Range: RangeAll, Surface: SurfaceGrok, Now: now}, NewTable())
	if grok.Summary.Requests != 1 || !modelPresent(grok, "grok-4") {
		t.Fatalf("grok filter=%+v", grok.Summary)
	}
}

func TestHistoricalEmptySurfaceIsUnattributedNotCodex(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC).UnixMilli()
	entries := []entry{
		{Timestamp: now, Provider: "openai", Model: "legacy", UsageStatus: "reported"},
		{Timestamp: now, Provider: "openai", Model: "explicit", Surface: "codex", UsageStatus: "reported"},
	}
	unknown := SummarizeQuery(entries, Query{Range: RangeAll, Now: now}, NewTable())
	if unknown.SurfaceAttribution.Unattributed != 1 || unknown.SurfaceAttribution.Codex != 1 {
		t.Fatalf("attribution=%+v", unknown.SurfaceAttribution)
	}
	codex := SummarizeQuery(entries, Query{Range: RangeAll, Surface: SurfaceCodex, Now: now}, NewTable())
	if codex.Summary.Requests != 1 || modelPresent(codex, "legacy") || !modelPresent(codex, "explicit") {
		t.Fatalf("empty surface promoted to Codex: %+v models=%+v", codex.Summary, codex.Models)
	}
}

func TestUnknownSurfaceQueryValueDoesNotInventAClient(t *testing.T) {
	if ParseSurface("") != SurfaceAll || ParseSurface("unknown") != SurfaceAll || ParseSurface("Codex") != SurfaceAll {
		t.Fatalf("ParseSurface invented a client: %q %q %q", ParseSurface(""), ParseSurface("unknown"), ParseSurface("Codex"))
	}
}
