package usageledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSnapshotCompleteLedgerBelowBound(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_1"}`)); err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"timestamp":2,"requestId":"req_2"}`)); err != nil {
		t.Fatal(err)
	}
	snap, err := l.SnapshotNewest(SnapshotLimits{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if snap.HistoryTruncated || snap.TruncatedPrefixBytes != 0 {
		t.Fatalf("%+v", snap)
	}
	if len(snap.Lines) != 2 {
		t.Fatalf("lines=%d", len(snap.Lines))
	}
}

func TestSnapshotNewestWindowSpansMultipleSegments(t *testing.T) {
	home := t.TempDir()
	rowSize := len(`{"timestamp":1,"requestId":"req_1"}`) + 1
	l := mustOpen(t, home, int64(rowSize))
	for i := 1; i <= 6; i++ {
		if err := l.Append([]byte(`{"timestamp":` + strconv.Itoa(i) + `,"requestId":"req_` + strconv.Itoa(i) + `"}`)); err != nil {
			t.Fatal(err)
		}
	}
	st, err := l.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.SealedCount < 2 {
		t.Fatalf("need multiple sealed sources: %+v", st)
	}
	limit := int64(rowSize * 2)
	snap, err := l.SnapshotNewest(SnapshotLimits{MaxBytes: limit})
	if err != nil {
		t.Fatal(err)
	}
	if !snap.HistoryTruncated {
		t.Fatal("expected truncation")
	}
	if snap.TruncatedPrefixBytes != snap.LogicalBytes-limit && snap.TruncatedPrefixBytes < snap.LogicalBytes-limit {
		t.Fatalf("prefix=%d logical=%d limit=%d", snap.TruncatedPrefixBytes, snap.LogicalBytes, limit)
	}
	ids := idsFromLines(t, snap.Lines)
	if len(ids) == 0 || ids[len(ids)-1] != "req_6" {
		t.Fatalf("newest missing: %v", ids)
	}
	for _, id := range ids {
		n, _ := strconv.Atoi(strings.TrimPrefix(id, "req_"))
		if n <= 3 {
			t.Fatalf("old row leaked into newest window: %v", ids)
		}
	}
}

func TestSnapshotTruncatedLedgerAboveReadBound(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, LegacyFile), []byte(`{"timestamp":1,"requestId":"req_old"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := mustOpen(t, home, 40)
	if err := l.Append([]byte(`{"timestamp":2,"requestId":"req_mid"}`)); err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"timestamp":3,"requestId":"req_new"}`)); err != nil {
		t.Fatal(err)
	}
	st, err := l.Status()
	if err != nil {
		t.Fatal(err)
	}
	limit := st.LogicalBytes / 2
	if limit <= 0 {
		t.Fatal("fixture too small")
	}
	snap, err := l.SnapshotNewest(SnapshotLimits{MaxBytes: limit})
	if err != nil {
		t.Fatal(err)
	}
	if !snap.HistoryTruncated || snap.TruncatedPrefixBytes <= 0 {
		t.Fatalf("%+v", snap)
	}
	ids := idsFromLines(t, snap.Lines)
	if containsID(ids, "req_old") {
		t.Fatalf("oldest leaked: %v", ids)
	}
	if !containsID(ids, "req_new") {
		t.Fatalf("newest missing: %v", ids)
	}
}

func TestSnapshotIgnoresIncompleteActiveTail(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_ok"}`)); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(l.activePath(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"timestamp":2,"requestId":"req_tail"`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	snap, err := l.SnapshotNewest(SnapshotLimits{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ids := idsFromLines(t, snap.Lines)
	if len(ids) != 1 || ids[0] != "req_ok" {
		t.Fatalf("%v", ids)
	}
}

func TestSnapshotWindowUsesCommittedBytesNotIncompleteTail(t *testing.T) {
	home := t.TempDir()
	row := `{"timestamp":1,"requestId":"req_keep"}`
	l := mustOpen(t, home, 1<<20)
	if err := l.Append([]byte(row)); err != nil {
		t.Fatal(err)
	}
	committed := int64(len(row) + 1)
	limit := committed + 32
	before, err := os.ReadFile(l.activePath())
	if err != nil {
		t.Fatal(err)
	}
	appendIncompleteTail(t, l.activePath(), strings.Repeat("x", int(limit)))
	after, err := os.ReadFile(l.activePath())
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(after)) <= limit {
		t.Fatalf("raw size %d should exceed limit %d", len(after), limit)
	}
	snap, err := l.SnapshotNewest(SnapshotLimits{MaxBytes: limit})
	if err != nil {
		t.Fatal(err)
	}
	if snap.HistoryTruncated {
		t.Fatalf("uncommitted tail consumed the window: %+v", snap)
	}
	if snap.LogicalBytes != committed {
		t.Fatalf("logical=%d committed=%d", snap.LogicalBytes, committed)
	}
	ids := idsFromLines(t, snap.Lines)
	if len(ids) != 1 || ids[0] != "req_keep" {
		t.Fatalf("%v", ids)
	}
	st, err := l.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.LogicalBytes != committed || st.ActiveBytes != committed {
		t.Fatalf("status logical=%d active=%d", st.LogicalBytes, st.ActiveBytes)
	}
	unchanged, err := os.ReadFile(l.activePath())
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != string(after) || !strings.HasPrefix(string(after), string(before)) {
		t.Fatal("read path rewrote the active file")
	}
}

func TestSnapshotTruncationIgnoresIncompleteSuffix(t *testing.T) {
	home := t.TempDir()
	row := `{"timestamp":1,"requestId":"req_x"}`
	l := mustOpen(t, home, int64(len(row)+1))
	for i := 1; i <= 6; i++ {
		if err := l.Append([]byte(`{"timestamp":` + strconv.Itoa(i) + `,"requestId":"req_` + strconv.Itoa(i) + `"}`)); err != nil {
			t.Fatal(err)
		}
	}
	limit := int64(len(row)+1) * 2
	first, err := l.SnapshotNewest(SnapshotLimits{MaxBytes: limit})
	if err != nil {
		t.Fatal(err)
	}
	if !first.HistoryTruncated {
		t.Fatal("expected committed truncation")
	}
	appendIncompleteTail(t, l.activePath(), strings.Repeat("y", int(limit)*4))
	second, err := l.SnapshotNewest(SnapshotLimits{MaxBytes: limit})
	if err != nil {
		t.Fatal(err)
	}
	if second.LogicalBytes != first.LogicalBytes {
		t.Fatalf("logical first=%d second=%d", first.LogicalBytes, second.LogicalBytes)
	}
	if second.HistoryTruncated != first.HistoryTruncated || second.TruncatedPrefixBytes != first.TruncatedPrefixBytes {
		t.Fatalf("truncation first=%+v second=%+v", first, second)
	}
	if strings.Join(idsFromLines(t, second.Lines), ",") != strings.Join(idsFromLines(t, first.Lines), ",") {
		t.Fatalf("rows first=%v second=%v", idsFromLines(t, first.Lines), idsFromLines(t, second.Lines))
	}
}

func TestSnapshotLegacyIncompleteTailDoesNotConsumeWindowOrRewrite(t *testing.T) {
	home := t.TempDir()
	row := `{"timestamp":1,"requestId":"req_legacy"}`
	legacy := filepath.Join(home, LegacyFile)
	tail := strings.Repeat("z", 4096)
	body := row + "\n" + `{"timestamp":2,"requestId":"req_partial"` + tail
	if err := os.WriteFile(legacy, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	l := mustOpen(t, home, 1<<20)
	committed := int64(len(row) + 1)
	limit := committed + 16
	if int64(len(body)) <= limit {
		t.Fatal("raw legacy size should exceed the window")
	}
	snap, err := l.SnapshotNewest(SnapshotLimits{MaxBytes: limit})
	if err != nil {
		t.Fatal(err)
	}
	if snap.HistoryTruncated || snap.LogicalBytes != committed {
		t.Fatalf("%+v committed=%d", snap, committed)
	}
	ids := idsFromLines(t, snap.Lines)
	if len(ids) != 1 || ids[0] != "req_legacy" {
		t.Fatalf("%v", ids)
	}
	raw, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != body {
		t.Fatal("legacy file was rewritten")
	}
}

func appendIncompleteTail(t *testing.T, path, tail string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(tail); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotZeroSealedSegments(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1<<20)
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_1"}`)); err != nil {
		t.Fatal(err)
	}
	st, err := l.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.SealedCount != 0 || !st.HasActive {
		t.Fatalf("%+v", st)
	}
	snap, err := l.SnapshotNewest(SnapshotLimits{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if snap.HistoryTruncated || len(snap.Lines) != 1 {
		t.Fatalf("%+v", snap)
	}
}

func TestSnapshotManySealedSegments(t *testing.T) {
	home := t.TempDir()
	row := `{"timestamp":1,"requestId":"req_x"}`
	l := mustOpen(t, home, int64(len(row)+1))
	for i := 1; i <= 12; i++ {
		if err := l.Append([]byte(`{"timestamp":` + strconv.Itoa(i) + `,"requestId":"req_` + strconv.Itoa(i) + `"}`)); err != nil {
			t.Fatal(err)
		}
	}
	st, err := l.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.SealedCount < 8 {
		t.Fatalf("sealed=%d", st.SealedCount)
	}
	got := committedIDs(t, home)
	if len(got) != 12 {
		t.Fatalf("%v", got)
	}
}

func TestHomeFromPath(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, LegacyFile)
	if got := HomeFromPath(path); got != home {
		t.Fatalf("got=%q want=%q", got, home)
	}
	if got := HomeFromPath(home); got != home {
		t.Fatalf("dir=%q", got)
	}
}

func idsFromLines(t *testing.T, lines [][]byte) []string {
	t.Helper()
	var ids []string
	for _, line := range lines {
		var row map[string]any
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		id, _ := row["requestId"].(string)
		ids = append(ids, id)
	}
	return ids
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
