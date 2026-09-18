package timeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRoundTripAndIgnoresOldRows(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, 8)
	tr := New("req-1", 8)
	tr.Mark(StagePreDispatch, SideLocal, MilestoneDispatch, true, "")
	tr.Mark(StageUpstreamWaitHeaders, SideUpstream, "", false, "timeout")
	if err := store.Save(tr); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("req-1")
	if err != nil {
		t.Fatal(err)
	}
	attr := got.Classify()
	if attr.Side != SideUpstream || attr.Stage != StageUpstreamWaitHeaders {
		t.Fatalf("loaded=%#v", attr)
	}
	legacy := filepath.Join(dir, "old.json")
	if err := os.WriteFile(legacy, []byte(`{"id":"old","events":[{"stage":"admit"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := store.Load("old")
	if err != nil || old == nil {
		t.Fatalf("old=%v err=%v", old, err)
	}
}

func TestStoreSaveNeverIncludesSecretsAndWriteFailureIsIgnored(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "timelines")
	store := NewStore(dir, 4)
	tr := New("req-2", 4)
	tr.Mark(StagePreDispatch, SideLocal, "", false, "admission_denied")
	if err := store.Save(tr); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "req-2.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Authorization") || strings.Contains(string(raw), "sk-") {
		t.Fatalf("leaked=%s", raw)
	}
	blockedPath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blockedPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewStore(blockedPath, 4).Save(New("x", 1)); err != nil {
		t.Fatalf("write failure must not surface: %v", err)
	}
}
