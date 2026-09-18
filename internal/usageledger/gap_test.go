package usageledger

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSealedGapIsCorruptNotComplete(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := os.MkdirAll(l.segmentsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.segmentPath(1), []byte(`{"timestamp":1,"requestId":"req_1"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.segmentPath(3), []byte(`{"timestamp":3,"requestId":"req_3"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := l.SnapshotNewest(SnapshotLimits{MaxBytes: 1 << 20}); !errors.Is(err, ErrCorruptLedger) {
		t.Fatalf("snapshot err=%v", err)
	}
	if err := l.Enumerate(func([]byte) error { return nil }); !errors.Is(err, ErrCorruptLedger) {
		t.Fatalf("enumerate err=%v", err)
	}
	if err := l.Append([]byte(`{"timestamp":4,"requestId":"req_4"}`)); !errors.Is(err, ErrCorruptLedger) {
		t.Fatalf("append err=%v", err)
	}
	raw, err := os.ReadFile(l.segmentPath(1))
	if err != nil || string(raw) != `{"timestamp":1,"requestId":"req_1"}`+"\n" {
		t.Fatalf("segment 1 mutated: %q", raw)
	}
	raw, err = os.ReadFile(l.segmentPath(3))
	if err != nil || string(raw) != `{"timestamp":3,"requestId":"req_3"}`+"\n" {
		t.Fatalf("segment 3 mutated: %q", raw)
	}
}

func TestSealedStartingAtTwoIsCorrupt(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := os.MkdirAll(l.segmentsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.segmentPath(2), []byte(`{"timestamp":2,"requestId":"req_2"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Status(); !errors.Is(err, ErrCorruptLedger) {
		t.Fatalf("status err=%v", err)
	}
	if err := l.Append([]byte(`{"timestamp":3,"requestId":"req_3"}`)); !errors.Is(err, ErrCorruptLedger) {
		t.Fatalf("append err=%v", err)
	}
}

func TestContiguousSealedSegmentsAreValid(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := os.MkdirAll(l.segmentsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.segmentPath(1), []byte(`{"timestamp":1,"requestId":"req_1"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.segmentPath(2), []byte(`{"timestamp":2,"requestId":"req_2"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.segmentPath(3), []byte(`{"timestamp":3,"requestId":"req_3"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if len(got) != 3 || got[0] != "req_1" || got[2] != "req_3" {
		t.Fatalf("%v", got)
	}
}

func TestUnknownAndTmpNamesDoNotCreateSequenceGaps(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := os.MkdirAll(l.segmentsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.segmentPath(1), []byte(`{"timestamp":1,"requestId":"req_1"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.segmentsPath(), "openai.jsonl"), []byte(`{"timestamp":9,"requestId":"req_bad"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.segmentsPath(), "00000002.jsonl.tmp"), []byte(`{"timestamp":8,"requestId":"req_tmp"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if len(got) != 1 || got[0] != "req_1" {
		t.Fatalf("%v", got)
	}
	if err := l.Append([]byte(`{"timestamp":2,"requestId":"req_2"}`)); err != nil {
		t.Fatal(err)
	}
	got = committedIDs(t, home)
	if !containsID(got, "req_1") || !containsID(got, "req_2") {
		t.Fatalf("%v", got)
	}
}
