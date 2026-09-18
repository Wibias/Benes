package usageledger

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyOnlyIsReadableAndNotRewritten(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, LegacyFile)
	body := `{"timestamp":1,"requestId":"req_legacy"}` + "\n"
	if err := os.WriteFile(legacy, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if len(got) != 1 || got[0] != "req_legacy" {
		t.Fatalf("%v", got)
	}
	raw, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != body {
		t.Fatal("legacy file was rewritten")
	}
}

func TestSegmentedOnlyDoesNotRequireLegacyFile(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_new"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, LegacyFile)); !os.IsNotExist(err) {
		t.Fatal("legacy file created")
	}
	got := committedIDs(t, home)
	if len(got) != 1 || got[0] != "req_new" {
		t.Fatalf("%v", got)
	}
}

func TestLegacyPlusSegmentedHasDeterministicOrderAndNoDoubleCount(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, LegacyFile), []byte(`{"timestamp":1,"requestId":"req_legacy"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := mustOpen(t, home, 64)
	if err := l.Append([]byte(`{"timestamp":2,"requestId":"req_new"}`)); err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"timestamp":3,"requestId":"req_newer"}`)); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if len(got) != 3 {
		t.Fatalf("double-count or drop: %v", got)
	}
	if got[0] != "req_legacy" || got[1] != "req_new" || got[2] != "req_newer" {
		t.Fatalf("order=%v", got)
	}
	st, err := l.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !st.HasLegacy || st.LegacyBytes <= 0 {
		t.Fatalf("legacy status=%+v", st)
	}
}

func TestMalformedLegacyTailDoesNotHideSegmentedRows(t *testing.T) {
	home := t.TempDir()
	legacy := `{"timestamp":1,"requestId":"req_legacy"}` + "\n" + `{"timestamp":2,"requestId":"req_partial"`
	if err := os.WriteFile(filepath.Join(home, LegacyFile), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	l := mustOpen(t, home, 1024)
	if err := l.Append([]byte(`{"timestamp":3,"requestId":"req_seg"}`)); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if len(got) != 2 || got[0] != "req_legacy" || got[1] != "req_seg" {
		t.Fatalf("%v", got)
	}
}

func TestNewWritesDoNotAppendLegacyFile(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, LegacyFile)
	before := `{"timestamp":1,"requestId":"req_legacy"}` + "\n"
	if err := os.WriteFile(legacy, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	l := mustOpen(t, home, 1024)
	if err := l.Append([]byte(`{"timestamp":2,"requestId":"req_new"}`)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != before {
		t.Fatalf("legacy mutated: %q", raw)
	}
}
