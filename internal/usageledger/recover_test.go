package usageledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMissingActiveWithSealedSegmentsIsReadableAndWritable(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 32)
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_1"}`)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(l.activePath()); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if len(got) != 1 || got[0] != "req_1" {
		t.Fatalf("lost sealed history: %v", got)
	}
	if err := l.Append([]byte(`{"timestamp":2,"requestId":"req_2"}`)); err != nil {
		t.Fatal(err)
	}
	got = committedIDs(t, home)
	if len(got) != 2 || got[1] != "req_2" {
		t.Fatalf("%v", got)
	}
}

func TestEmptyActiveFileIsIgnored(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 32)
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_1"}`)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.activePath(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if len(got) < 1 || got[0] != "req_1" {
		t.Fatalf("%v", got)
	}
}

func TestLeftoverTemporaryFilesAreIgnoredAndRemovedOnWrite(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := os.MkdirAll(l.segmentsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	tmpActive := filepath.Join(l.dir(), "active.jsonl.tmp")
	tmpSeg := filepath.Join(l.segmentsPath(), "00000001.jsonl.tmp")
	if err := os.WriteFile(tmpActive, []byte(`{"timestamp":9,"requestId":"req_tmp"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpSeg, []byte(`{"timestamp":8,"requestId":"req_tmpseg"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if containsID(got, "req_tmp") || containsID(got, "req_tmpseg") {
		t.Fatalf("tmp rows leaked: %v", got)
	}
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_ok"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tmpActive); !os.IsNotExist(err) {
		t.Fatal("active tmp survived write")
	}
	if _, err := os.Stat(tmpSeg); !os.IsNotExist(err) {
		t.Fatal("segment tmp survived write")
	}
	got = committedIDs(t, home)
	if len(got) != 1 || got[0] != "req_ok" {
		t.Fatalf("%v", got)
	}
}

func TestMalformedSealedRowDoesNotDiscardNeighboringSegment(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := os.MkdirAll(l.segmentsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.segmentPath(1), []byte(`{"timestamp":1,"requestId":"req_1"}`+"\nnot-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.segmentPath(2), []byte(`{"timestamp":2,"requestId":"req_2"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if len(got) != 2 || got[0] != "req_1" || got[1] != "req_2" {
		t.Fatalf("%v", got)
	}
}

func TestUnknownSegmentNamesAreNotHistory(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := os.MkdirAll(l.segmentsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.segmentsPath(), "openai.jsonl"), []byte(`{"timestamp":1,"requestId":"req_bad"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.segmentPath(1), []byte(`{"timestamp":2,"requestId":"req_ok"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if len(got) != 1 || got[0] != "req_ok" {
		t.Fatalf("%v", got)
	}
}

func TestCrashAfterAppendBeforeRotationKeepsCommittedRow(t *testing.T) {
	home := t.TempDir()
	row := []byte(`{"timestamp":1,"requestId":"req_1"}`)
	l := mustOpen(t, home, int64(len(row)+1)*4)
	if err := l.Append(row); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(l.activePath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < l.TargetBytes() {
		second := []byte(`{"timestamp":2,"requestId":"req_2"}`)
		if err := os.WriteFile(l.activePath(), append(append([]byte{}, mustRead(t, l.activePath())...), append(second, '\n')...), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := committedIDs(t, home)
	if len(got) < 1 || got[0] != "req_1" {
		t.Fatalf("%v", got)
	}
}

func TestActiveExactlyAtThresholdRotates(t *testing.T) {
	home := t.TempDir()
	row := []byte(`{"timestamp":1,"requestId":"req_1"}`)
	l := mustOpen(t, home, int64(len(row)+1))
	if err := l.Append(row); err != nil {
		t.Fatal(err)
	}
	st, err := l.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.SealedCount != 1 {
		t.Fatalf("sealed=%d", st.SealedCount)
	}
}

func TestFailClosedWhenActiveIsDirectory(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := os.MkdirAll(l.activePath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_1"}`)); err == nil {
		t.Fatal("expected fail closed")
	}
}

func TestAppendRecoversIncompleteActiveTail(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1<<20)
	complete := []byte(`{"timestamp":1,"requestId":"req_ok"}`)
	if err := l.Append(complete); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(l.activePath(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"timestamp":2,"requestId":"req_partial"`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"timestamp":3,"requestId":"req_new"}`)); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if len(got) != 2 || got[0] != "req_ok" || got[1] != "req_new" {
		t.Fatalf("%v", got)
	}
	if containsID(got, "req_partial") {
		t.Fatal("uncommitted tail was committed")
	}
}

func TestAppendRecoversActiveThatIsOnlyIncomplete(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1<<20)
	if err := os.MkdirAll(l.dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.activePath(), []byte(`{"timestamp":1,"requestId":"req_partial"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"timestamp":2,"requestId":"req_new"}`)); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if len(got) != 1 || got[0] != "req_new" {
		t.Fatalf("%v", got)
	}
}

func TestIncompleteTailIsNotSealedWhenRawSizeCrossesThreshold(t *testing.T) {
	home := t.TempDir()
	complete := []byte(`{"timestamp":1,"requestId":"req_ok"}`)
	l := mustOpen(t, home, int64(len(complete)+1+8))
	if err := l.Append(complete); err != nil {
		t.Fatal(err)
	}
	pad := strings.Repeat("x", 64)
	incomplete := `{"timestamp":2,"requestId":"req_partial","pad":"` + pad
	if err := os.WriteFile(l.activePath(), append(append(complete, '\n'), []byte(incomplete)...), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(l.activePath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= l.TargetBytes() {
		t.Fatalf("fixture raw size %d does not exceed target %d", info.Size(), l.TargetBytes())
	}
	if err := l.Append([]byte(`{"timestamp":3,"requestId":"req_new"}`)); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if containsID(got, "req_partial") {
		t.Fatal("uncommitted bytes were sealed")
	}
	if !containsID(got, "req_ok") || !containsID(got, "req_new") {
		t.Fatalf("%v", got)
	}
	raw, err := os.ReadFile(l.segmentPath(1))
	if err == nil && strings.Contains(string(raw), "req_partial") {
		t.Fatal("sealed segment contains uncommitted tail")
	}
}

func TestRecoveryThenThresholdRotationDoesNotLoseOrDuplicate(t *testing.T) {
	home := t.TempDir()
	row := []byte(`{"timestamp":1,"requestId":"req_1"}`)
	l := mustOpen(t, home, int64(len(row)+1))
	if err := os.MkdirAll(l.dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.activePath(), append(row, []byte("\n{\"timestamp\":2")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"timestamp":3,"requestId":"req_2"}`)); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if len(got) != 2 || got[0] != "req_1" || got[1] != "req_2" {
		t.Fatalf("%v", got)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
