package timeline

import (
	"errors"
	"testing"
)

func TestLookupReturnsASavedTraceAndFailsClosedWhenMissing(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, 8)
	tr := New("req-lookup", 8)
	tr.Mark(StagePreDispatch, SideLocal, MilestoneDispatch, true, "")
	if err := store.Save(tr); err != nil {
		t.Fatal(err)
	}
	got, err := store.Lookup("req-lookup")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "req-lookup" {
		t.Fatalf("id=%q", got.ID())
	}
	if _, err := store.Lookup("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing err=%v", err)
	}
	if _, err := store.Lookup(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty id err=%v", err)
	}
	if _, err := store.Lookup("../escape"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("escape err=%v", err)
	}
}
