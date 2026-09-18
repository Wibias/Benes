package requesthistory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/usageledger"
)

func TestRebuildAndLookup(t *testing.T) {
	home := t.TempDir()
	line := `{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"direct"}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := Rebuild(home)
	if err != nil || meta.IndexedRows != 1 {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	row, err := Lookup(home, "req_1")
	if err != nil {
		t.Fatal(err)
	}
	if row["requestId"] != "req_1" {
		t.Fatalf("%#v", row)
	}
	status := Status(home)
	if status.IndexedRows != 1 {
		t.Fatalf("%#v", status)
	}
}

func TestRebuildSkipsRowsWithoutRequestID(t *testing.T) {
	home := t.TempDir()
	lines := strings.Join([]string{
		`{"timestamp":1,"provider":"openai","model":"gpt-5","usageStatus":"reported"}`,
		`{"requestId":"req_2","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"direct"}}`,
		`{"requestId":"","provider":"openai","model":"gpt-5","status":200}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := Rebuild(home)
	if err != nil || meta.IndexedRows != 1 {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	if _, err := Lookup(home, "req_2"); err != nil {
		t.Fatal(err)
	}
}

func TestRebuildMissingUsage(t *testing.T) {
	home := t.TempDir()
	meta, err := Rebuild(home)
	if err != nil || meta.IndexedRows != 0 {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
}

func TestRebuildReadsSegmentedLedger(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.OpenWithTarget(home, 80)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_a","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"direct"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_b","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"combo"}}`)); err != nil {
		t.Fatal(err)
	}
	meta, err := Rebuild(home)
	if err != nil || meta.IndexedRows != 2 {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	if meta.SourceSize <= 0 || meta.IndexedOffset != meta.SourceSize {
		t.Fatalf("logical size meta=%#v", meta)
	}
	row, err := Lookup(home, "req_b")
	if err != nil {
		t.Fatal(err)
	}
	decision, _ := row["routeDecision"].(map[string]any)
	if decision["routeKind"] != "combo" {
		t.Fatalf("%#v", row)
	}
}

func TestRebuildReadsLegacyAndSegmentedRows(t *testing.T) {
	home := t.TempDir()
	legacy := `{"requestId":"req_legacy","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"direct"}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_new","provider":"openai","model":"gpt-5.6","status":200,"routeDecision":{"routeKind":"direct"}}`)); err != nil {
		t.Fatal(err)
	}
	meta, err := Rebuild(home)
	if err != nil || meta.IndexedRows != 2 {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	if _, err := Lookup(home, "req_legacy"); err != nil {
		t.Fatal(err)
	}
	if _, err := Lookup(home, "req_new"); err != nil {
		t.Fatal(err)
	}
}

func TestRebuildExcludesUncommittedSuffixFromLogicalSize(t *testing.T) {
	home := t.TempDir()
	row := `{"requestId":"req_keep","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"direct"}}`
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(row)); err != nil {
		t.Fatal(err)
	}
	before, err := Rebuild(home)
	if err != nil || before.IndexedRows != 1 {
		t.Fatalf("before=%#v err=%v", before, err)
	}
	file, err := os.OpenFile(filepath.Join(home, usageledger.DirName, usageledger.ActiveFile), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(strings.Repeat("x", 4096)); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := Rebuild(home)
	if err != nil || after.IndexedRows != 1 {
		t.Fatalf("after=%#v err=%v", after, err)
	}
	if after.SourceSize != before.SourceSize || after.IndexedOffset != before.IndexedOffset {
		t.Fatalf("uncommitted suffix counted as history before=%#v after=%#v", before, after)
	}
	if after.SourceSize != int64(len(row)+1) {
		t.Fatalf("sourceSize=%d committed=%d", after.SourceSize, len(row)+1)
	}
}

func TestRebuildMetadataMatchesEnumeratedSnapshot(t *testing.T) {
	home := t.TempDir()
	keep := `{"requestId":"req_keep","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"direct"}}`
	added := `{"requestId":"req_new","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"direct"}}`
	writer, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Append([]byte(keep)); err != nil {
		t.Fatal(err)
	}
	usageledger.SetAfterOpenSourcesTestHook(func() {
		usageledger.SetAfterOpenSourcesTestHook(nil)
		if err := writer.Append([]byte(added)); err != nil {
			t.Errorf("writer append: %v", err)
		}
	})
	defer usageledger.SetAfterOpenSourcesTestHook(nil)
	meta, err := Rebuild(home)
	if err != nil {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	_, newErr := storedRequest(home, "req_new")
	indexedNew := newErr == nil
	if indexedNew {
		want := int64(len(keep) + 1 + len(added) + 1)
		if meta.SourceSize != want || meta.IndexedOffset != want || meta.IndexedRows != 2 {
			t.Fatalf("indexed req_new against an earlier snapshot meta=%#v", meta)
		}
		return
	}
	want := int64(len(keep) + 1)
	if meta.IndexedOffset != want || meta.IndexedRows != 1 {
		t.Fatalf("metadata drifted from enumerated rows meta=%#v indexedNew=%v", meta, indexedNew)
	}
	if _, err := storedRequest(home, "req_keep"); err != nil {
		t.Fatal(err)
	}
}

func storedRequest(home, id string) (map[string]any, error) {
	return lookupOnly(home, id)
}

func TestRebuildSurfacesSealedSequenceGap(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "usage", "segments"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "usage", "segments", "00000001.jsonl"), []byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "usage", "segments", "00000003.jsonl"), []byte(`{"requestId":"req_3","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := Rebuild(home)
	if err == nil {
		t.Fatalf("expected corruption err meta=%#v", meta)
	}
	if meta.LastError == "" {
		t.Fatal("corruption was not recorded")
	}
	if meta.IndexedRows != 0 {
		t.Fatalf("indexed incomplete history: %#v", meta)
	}
}
