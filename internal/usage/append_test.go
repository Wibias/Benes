package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalSurfaceAllowlist(t *testing.T) {
	if CanonicalSurface("codex") != "codex" || CanonicalSurface("Claude-Desktop") != "claude-desktop" {
		t.Fatal("allowlist rejected known surfaces")
	}
	if CanonicalSurface("Codex") != "codex" {
		t.Fatal("case fold failed")
	}
	if CanonicalSurface("") != "" || CanonicalSurface("chatgpt") != "" || CanonicalSurface("unknown") != "" {
		t.Fatal("unknown surface was invented")
	}
}

func TestAppendWritesExplicitSurfaceAndOmitsUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := Append(path, Entry{Timestamp: 1, Provider: "openai", Model: "gpt-5", Surface: "codex", UsageStatus: "reported"}); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, Entry{Timestamp: 2, Provider: "xai", Model: "grok-4", Surface: "chatgpt", UsageStatus: "reported"}); err != nil {
		t.Fatal(err)
	}
	got := mustSummarizeLimited(t, path, Query{Range: RangeAll, Now: 2}, defaultReadLimits())
	if got.SurfaceAttribution.Codex != 1 || got.SurfaceAttribution.Unattributed != 1 {
		t.Fatalf("attribution=%+v", got.SurfaceAttribution)
	}
	codex := mustSummarizeLimited(t, path, Query{Range: RangeAll, Surface: SurfaceCodex, Now: 2}, defaultReadLimits())
	if codex.Summary.Requests != 1 {
		t.Fatalf("codex selected unknown: %+v", codex.Summary)
	}
}

func TestAppendDoesNotWriteWhenPathEmpty(t *testing.T) {
	if err := Append("", Entry{Timestamp: 1, Provider: "a", Model: "m"}); err != nil {
		t.Fatal(err)
	}
}

func TestClipFieldBoundsUntrustedStrings(t *testing.T) {
	long := strings.Repeat("x", 300)
	if got := ClipField(long); len([]rune(got)) != maxFieldRunes {
		t.Fatalf("len=%d", len([]rune(got)))
	}
}

func TestAppendCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "usage.jsonl")
	if err := Append(path, Entry{Timestamp: 1, Provider: "a", Model: "m", UsageStatus: "unreported"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
