package managedfs

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAssertSameHomeAcceptsEquivalentPathsAndRefusesAMismatch(t *testing.T) {
	home := t.TempDir()
	other := t.TempDir()
	if err := AssertSameHome(home, filepath.Clean(filepath.Join(home, "."))); err != nil {
		t.Fatal(err)
	}
	if err := AssertSameHome(home, other); !errors.Is(err, ErrCrossHome) {
		t.Fatalf("mismatch=%v", err)
	}
}

func TestAssertSameHomeRequiresAtLeastOneHome(t *testing.T) {
	if err := AssertSameHome(); err == nil {
		t.Fatal("empty accepted")
	}
}
