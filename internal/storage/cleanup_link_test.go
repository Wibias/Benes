package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCandidateReplacedByLinkIsStale(t *testing.T) {
	home := t.TempDir()
	path := writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "target.jsonl")
	if err := os.WriteFile(outside, []byte("keep-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Skip("symlinks not available")
	}
	_, err = ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModePermanent, Digest: preview.Digest})
	if err == nil || asError(err).Code != CodeStalePreview {
		t.Fatalf("expected stale_preview, got %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("outside target mutated")
	}
}
