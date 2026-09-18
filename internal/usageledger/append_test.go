package usageledger

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFirstAppendCreatesActiveStorage(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_1"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(l.activePath()); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(l.segmentsPath()); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("sealed=%d", len(entries))
	}
	if _, err := os.Stat(filepath.Join(home, LegacyFile)); !os.IsNotExist(err) {
		t.Fatalf("legacy file was created: %v", err)
	}
}

func TestManyAppendsStayReadable(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1<<20)
	for i := 1; i <= 50; i++ {
		if err := l.Append([]byte(`{"timestamp":` + strconv.Itoa(i) + `,"requestId":"req_` + strconv.Itoa(i) + `"}`)); err != nil {
			t.Fatal(err)
		}
	}
	got := committedIDs(t, home)
	if len(got) != 50 {
		t.Fatalf("n=%d", len(got))
	}
	if got[0] != "req_1" || got[49] != "req_50" {
		t.Fatalf("%v", got)
	}
}

func TestNewlineIsCommitMarker(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_1"}`)); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(l.activePath(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"timestamp":2,"requestId":"req_pending"}`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	got := committedIDs(t, home)
	if len(got) != 1 || got[0] != "req_1" {
		t.Fatalf("unterminated record leaked: %v", got)
	}
}

func TestAppendRejectsNewlineInRecord(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := l.Append([]byte("{\"a\":1}\n{\"b\":2}")); err == nil {
		t.Fatal("accepted newline")
	}
	if _, err := os.Stat(l.activePath()); !os.IsNotExist(err) {
		t.Fatal("partial write created active")
	}
}

func TestRotateAtThresholdDoesNotDuplicateOrLoseRows(t *testing.T) {
	home := t.TempDir()
	row := []byte(`{"timestamp":1,"requestId":"req_x"}`)
	l := mustOpen(t, home, int64(len(row)+1)*2)
	for i := 1; i <= 5; i++ {
		body := []byte(`{"timestamp":` + strconv.Itoa(i) + `,"requestId":"req_` + strconv.Itoa(i) + `"}`)
		if err := l.Append(body); err != nil {
			t.Fatal(err)
		}
	}
	st, err := l.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.SealedCount == 0 {
		t.Fatal("expected sealed segments")
	}
	got := committedIDs(t, home)
	if len(got) != 5 {
		t.Fatalf("n=%d sealed=%d active=%d ids=%v", len(got), st.SealedCount, st.ActiveBytes, got)
	}
	for i := 1; i <= 5; i++ {
		want := "req_" + strconv.Itoa(i)
		if got[i-1] != want {
			t.Fatalf("order %v", got)
		}
	}
}

func TestRecordLargerThanRemainingCapacityRotatesThenWrites(t *testing.T) {
	home := t.TempDir()
	small := []byte(`{"timestamp":1,"requestId":"req_small"}`)
	large := []byte(`{"timestamp":2,"requestId":"req_large","pad":"` + strings.Repeat("x", 40) + `"}`)
	l := mustOpen(t, home, int64(len(small)+1+20))
	if err := l.Append(small); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(large); err != nil {
		t.Fatal(err)
	}
	st, err := l.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.SealedCount < 1 {
		t.Fatal("expected rotation before large record")
	}
	got := committedIDs(t, home)
	if len(got) != 2 || got[0] != "req_small" || got[1] != "req_large" {
		t.Fatalf("%v", got)
	}
}

func TestSingleRecordLargerThanTargetIsStoredWithoutInfiniteRotation(t *testing.T) {
	home := t.TempDir()
	huge := []byte(`{"timestamp":1,"requestId":"req_huge","pad":"` + strings.Repeat("y", 80) + `"}`)
	l := mustOpen(t, home, 32)
	if int64(len(huge)+1) <= l.TargetBytes() {
		t.Fatal("fixture is not larger than target")
	}
	if err := l.Append(huge); err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"timestamp":2,"requestId":"req_next"}`)); err != nil {
		t.Fatal(err)
	}
	st, err := l.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.SealedCount < 1 {
		t.Fatal("oversized record was not sealed")
	}
	got := committedIDs(t, home)
	if len(got) != 2 || got[0] != "req_huge" || got[1] != "req_next" {
		t.Fatalf("%v", got)
	}
}

func TestRenameDoesNotReplaceExistingSealedSegment(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	if err := os.MkdirAll(l.segmentsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	active := l.activePath()
	dest := l.segmentPath(1)
	if err := os.WriteFile(active, []byte(`{"timestamp":2,"requestId":"req_new"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := []byte(`{"timestamp":1,"requestId":"req_keep"}` + "\n")
	if err := os.WriteFile(dest, keep, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := renameNoReplace(active, dest); err == nil {
		t.Fatal("replaced sealed segment")
	}
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, keep) {
		t.Fatalf("sealed contents changed: %q", raw)
	}
}

func TestSegmentFilenamesContainNoUserStrings(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 40)
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_1","provider":"openai","model":"gpt-5","account":"main"}`)); err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"timestamp":2,"requestId":"req_2","provider":"openai","model":"gpt-5","account":"main"}`)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(l.segmentsPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		for _, leak := range []string{"openai", "gpt-5", "main", "req_"} {
			if strings.Contains(name, leak) {
				t.Fatalf("segment name %q contains %q", name, leak)
			}
		}
		if _, ok := parseSegmentName(name); !ok && !strings.HasSuffix(name, ".tmp") {
			t.Fatalf("unexpected segment name %q", name)
		}
	}
}

func mustOpen(t *testing.T, home string, target int64) *Ledger {
	t.Helper()
	l, err := OpenWithTarget(home, target)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func committedIDs(t *testing.T, home string) []string {
	t.Helper()
	raw, err := CommittedBytes(home)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil
	}
	var ids []string
	for _, line := range strings.Split(text, "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		id, _ := row["requestId"].(string)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}
