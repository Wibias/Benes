package usageledger

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEnumerateFromSnapshotStartZero(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1<<20)
	row := []byte(`{"timestamp":1,"requestId":"req_1"}`)
	if err := l.Append(row); err != nil {
		t.Fatal(err)
	}
	var recs []Record
	end, err := l.EnumerateFromSnapshot(0, func(rec Record) error {
		recs = append(recs, rec)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := int64(len(row) + 1)
	if end != want {
		t.Fatalf("end=%d want=%d", end, want)
	}
	if len(recs) != 1 || recs[0].Offset != 0 || recs[0].End != want || string(recs[0].Line) != string(row) {
		t.Fatalf("%+v", recs)
	}
}

func TestEnumerateFromSnapshotExactLineBoundary(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1<<20)
	first := []byte(`{"timestamp":1,"requestId":"req_1"}`)
	second := []byte(`{"timestamp":2,"requestId":"req_2"}`)
	if err := l.Append(first); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(second); err != nil {
		t.Fatal(err)
	}
	start := int64(len(first) + 1)
	var recs []Record
	end, err := l.EnumerateFromSnapshot(start, func(rec Record) error {
		recs = append(recs, rec)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := start + int64(len(second)+1)
	if end != want {
		t.Fatalf("end=%d want=%d", end, want)
	}
	if len(recs) != 1 || recs[0].Offset != start || string(recs[0].Line) != string(second) {
		t.Fatalf("%+v", recs)
	}
}

func TestEnumerateFromSnapshotSpansSealedAndActive(t *testing.T) {
	home := t.TempDir()
	row := []byte(`{"timestamp":1,"requestId":"req_1"}`)
	l := mustOpen(t, home, int64(len(row)+1))
	for i := 1; i <= 3; i++ {
		body := []byte(`{"timestamp":` + strconv.Itoa(i) + `,"requestId":"req_` + strconv.Itoa(i) + `"}`)
		if err := l.Append(body); err != nil {
			t.Fatal(err)
		}
	}
	st, err := l.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.SealedCount < 2 {
		t.Fatalf("need sealed sources: %+v", st)
	}
	start := st.Sealed[0].Bytes
	var ids []string
	end, err := l.EnumerateFromSnapshot(start, func(rec Record) error {
		if len(rec.Line) > 0 {
			ids = append(ids, idsFromLines(t, [][]byte{rec.Line})...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if end != st.LogicalBytes {
		t.Fatalf("end=%d logical=%d", end, st.LogicalBytes)
	}
	if len(ids) != 2 || ids[0] != "req_2" || ids[1] != "req_3" {
		t.Fatalf("ids=%v", ids)
	}
}

func TestEnumerateFromSnapshotLegacyThenSegmented(t *testing.T) {
	home := t.TempDir()
	legacy := []byte(`{"timestamp":1,"requestId":"req_legacy"}` + "\n")
	if err := os.WriteFile(filepath.Join(home, LegacyFile), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	l := mustOpen(t, home, 1<<20)
	next := []byte(`{"timestamp":2,"requestId":"req_new"}`)
	if err := l.Append(next); err != nil {
		t.Fatal(err)
	}
	var ids []string
	end, err := l.EnumerateFromSnapshot(int64(len(legacy)), func(rec Record) error {
		if len(rec.Line) > 0 {
			ids = append(ids, idsFromLines(t, [][]byte{rec.Line})...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if end != int64(len(legacy)+len(next)+1) {
		t.Fatalf("end=%d", end)
	}
	if len(ids) != 1 || ids[0] != "req_new" {
		t.Fatalf("ids=%v", ids)
	}
}

func TestEnumerateFromSnapshotStartAtEnd(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1<<20)
	row := []byte(`{"timestamp":1,"requestId":"req_1"}`)
	if err := l.Append(row); err != nil {
		t.Fatal(err)
	}
	called := false
	end, err := l.EnumerateFromSnapshot(int64(len(row)+1), func(Record) error {
		called = true
		return nil
	})
	if err != nil || called || end != int64(len(row)+1) {
		t.Fatalf("end=%d called=%v err=%v", end, called, err)
	}
}

func TestEnumerateFromSnapshotStartBeyondEnd(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1<<20)
	row := []byte(`{"timestamp":1,"requestId":"req_1"}`)
	if err := l.Append(row); err != nil {
		t.Fatal(err)
	}
	_, err := l.EnumerateFromSnapshot(int64(len(row)+2), func(Record) error { return nil })
	if !errors.Is(err, ErrOffsetBeyondEnd) {
		t.Fatalf("err=%v", err)
	}
}

func TestEnumerateFromSnapshotStartInsideRecord(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1<<20)
	row := []byte(`{"timestamp":1,"requestId":"req_1"}`)
	if err := l.Append(row); err != nil {
		t.Fatal(err)
	}
	_, err := l.EnumerateFromSnapshot(3, func(Record) error { return nil })
	if !errors.Is(err, ErrUnalignedOffset) {
		t.Fatalf("err=%v", err)
	}
}

func TestEnumerateFromSnapshotExcludesIncompleteSuffix(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1<<20)
	row := []byte(`{"timestamp":1,"requestId":"req_keep"}`)
	if err := l.Append(row); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(filepath.Join(home, DirName, ActiveFile), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"timestamp":2,"requestId":"req_partial"`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var ids []string
	end, err := l.EnumerateFromSnapshot(0, func(rec Record) error {
		if len(rec.Line) > 0 {
			ids = append(ids, idsFromLines(t, [][]byte{rec.Line})...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if end != int64(len(row)+1) {
		t.Fatalf("end=%d", end)
	}
	if len(ids) != 1 || ids[0] != "req_keep" {
		t.Fatalf("ids=%v", ids)
	}
}

func TestEnumerateFromSnapshotMalformedLineAdvances(t *testing.T) {
	home := t.TempDir()
	raw := "not-json\n{\"timestamp\":1,\"requestId\":\"req_ok\"}\n"
	if err := os.WriteFile(filepath.Join(home, LegacyFile), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	l := mustOpen(t, home, 1<<20)
	var recs []Record
	end, err := l.EnumerateFromSnapshot(0, func(rec Record) error {
		recs = append(recs, rec)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if end != int64(len(raw)) {
		t.Fatalf("end=%d want=%d", end, len(raw))
	}
	if len(recs) != 2 || recs[0].End != int64(len("not-json\n")) || string(recs[1].Line) != `{"timestamp":1,"requestId":"req_ok"}` {
		t.Fatalf("%+v", recs)
	}
}

func TestEnumerateFromSnapshotBlankLineAdvances(t *testing.T) {
	home := t.TempDir()
	raw := "\n{\"timestamp\":1,\"requestId\":\"req_ok\"}\n"
	if err := os.WriteFile(filepath.Join(home, LegacyFile), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	l := mustOpen(t, home, 1<<20)
	var recs []Record
	end, err := l.EnumerateFromSnapshot(0, func(rec Record) error {
		recs = append(recs, rec)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if end != int64(len(raw)) || len(recs) != 2 || recs[0].End != 1 || len(recs[0].Line) != 0 {
		t.Fatalf("end=%d recs=%+v", end, recs)
	}
}

func TestEnumerateFromSnapshotRotationDuringDiscovery(t *testing.T) {
	home := t.TempDir()
	row1 := []byte(`{"timestamp":1,"requestId":"req_1"}`)
	row2 := []byte(`{"timestamp":2,"requestId":"req_2"}`)
	row3 := []byte(`{"timestamp":3,"requestId":"req_3"}`)
	writer := mustOpen(t, home, int64(len(row1)+1))
	if err := writer.Append(row1); err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(row2); err != nil {
		t.Fatal(err)
	}
	reader := mustOpen(t, home, int64(len(row1)+1))
	testHookAfterListSources = func() {
		testHookAfterListSources = nil
		if err := writer.Append(row3); err != nil {
			t.Errorf("rotate: %v", err)
		}
	}
	defer func() { testHookAfterListSources = nil }()
	var ids []string
	_, err := reader.EnumerateFromSnapshot(0, func(rec Record) error {
		if len(rec.Line) > 0 {
			ids = append(ids, idsFromLines(t, [][]byte{rec.Line})...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || ids[0] != "req_1" || ids[2] != "req_3" {
		t.Fatalf("%v", ids)
	}
}

func TestEnumerateFromSnapshotIgnoresAppendAfterOpen(t *testing.T) {
	home := t.TempDir()
	keep := []byte(`{"timestamp":1,"requestId":"req_keep"}`)
	added := []byte(`{"timestamp":2,"requestId":"req_new"}`)
	writer := mustOpen(t, home, 1<<20)
	if err := writer.Append(keep); err != nil {
		t.Fatal(err)
	}
	reader := mustOpen(t, home, 1<<20)
	testHookAfterOpenSources = func() {
		testHookAfterOpenSources = nil
		if err := writer.Append(added); err != nil {
			t.Errorf("append: %v", err)
		}
	}
	defer func() { testHookAfterOpenSources = nil }()
	var ids []string
	end, err := reader.EnumerateFromSnapshot(0, func(rec Record) error {
		if len(rec.Line) > 0 {
			ids = append(ids, idsFromLines(t, [][]byte{rec.Line})...)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if end != int64(len(keep)+1) || len(ids) != 1 || ids[0] != "req_keep" {
		t.Fatalf("end=%d ids=%v", end, ids)
	}
}

func TestEnumerateFromSnapshotSkipsPrefixBytes(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1<<20)
	var prefix int64
	for i := 1; i <= 20; i++ {
		row := []byte(`{"timestamp":` + strconv.Itoa(i) + `,"requestId":"req_` + strconv.Itoa(i) + `"}`)
		if err := l.Append(row); err != nil {
			t.Fatal(err)
		}
		prefix += int64(len(row) + 1)
	}
	extra := []byte(`{"timestamp":21,"requestId":"req_21"}`)
	if err := l.Append(extra); err != nil {
		t.Fatal(err)
	}
	var scanned int64
	SetEnumerateProgressTestHook(func(start, end, n int64) {
		if start != prefix {
			t.Errorf("start=%d want=%d", start, prefix)
		}
		scanned = n
	})
	defer SetEnumerateProgressTestHook(nil)
	end, err := l.EnumerateFromSnapshot(prefix, func(Record) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	wantScan := int64(len(extra) + 1)
	if scanned != wantScan {
		t.Fatalf("scanned=%d want=%d end=%d", scanned, wantScan, end)
	}
}

func TestEnumerateFromSnapshotSequenceGap(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, DirName, SegmentsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, DirName, SegmentsDir, "00000001.jsonl"), []byte(`{"timestamp":1,"requestId":"req_1"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, DirName, SegmentsDir, "00000003.jsonl"), []byte(`{"timestamp":3,"requestId":"req_3"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := mustOpen(t, home, 1<<20)
	_, err := l.EnumerateFromSnapshot(0, func(Record) error { return nil })
	if !errors.Is(err, ErrCorruptLedger) {
		t.Fatalf("err=%v", err)
	}
}

func TestEnumerateFromSnapshotOversizedLineAdvances(t *testing.T) {
	home := t.TempDir()
	big := strings.Repeat("x", defaultMaxLine+8)
	raw := big + "\n{\"timestamp\":1,\"requestId\":\"req_ok\"}\n"
	if err := os.WriteFile(filepath.Join(home, LegacyFile), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	l := mustOpen(t, home, 1<<20)
	var recs []Record
	end, err := l.EnumerateFromSnapshot(0, func(rec Record) error {
		recs = append(recs, rec)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if end != int64(len(raw)) {
		t.Fatalf("end=%d want=%d", end, len(raw))
	}
	if len(recs) != 2 || !recs[0].Oversized || recs[0].End != int64(len(big)+1) {
		t.Fatalf("%+v", recs)
	}
	if recs[1].Oversized || string(recs[1].Line) != `{"timestamp":1,"requestId":"req_ok"}` {
		t.Fatalf("%+v", recs)
	}
}

func TestEnumerateFromSnapshotRefusesUsageSymlink(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, DirName)); err != nil {
		t.Skip(err)
	}
	l := mustOpen(t, home, 1024)
	_, err := l.EnumerateFromSnapshot(0, func(Record) error { return nil })
	if err == nil {
		t.Fatal("followed usage symlink")
	}
}
