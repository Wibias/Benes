package requesthistory

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Wibias/Benes/internal/usageledger"

	_ "modernc.org/sqlite"
)

func TestCatchUpInitialAndNoop(t *testing.T) {
	home := t.TempDir()
	row := `{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(row)); err != nil {
		t.Fatal(err)
	}
	first, err := CatchUp(home)
	if err != nil || first.IndexedRows != 1 || first.IndexedOffset != first.SourceSize || !first.CaughtUp {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := CatchUp(home)
	if err != nil || second.IndexedRows != 1 || second.IndexedOffset != first.IndexedOffset {
		t.Fatalf("second=%#v err=%v", second, err)
	}
}

func TestCatchUpIndexesSmallSuffixOnly(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	var prefix int64
	for i := 1; i <= 12; i++ {
		row := []byte(`{"requestId":"req_` + strconv.Itoa(i) + `","provider":"openai","model":"gpt-5","status":200}`)
		if err := l.Append(row); err != nil {
			t.Fatal(err)
		}
		prefix += int64(len(row) + 1)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	extra := []byte(`{"requestId":"req_new","provider":"openai","model":"gpt-5","status":200}`)
	if err := l.Append(extra); err != nil {
		t.Fatal(err)
	}
	var start, scanned int64
	usageledger.SetEnumerateProgressTestHook(func(s, _, n int64) {
		start = s
		scanned = n
	})
	defer usageledger.SetEnumerateProgressTestHook(nil)
	meta, err := CatchUp(home)
	if err != nil {
		t.Fatal(err)
	}
	if start != prefix {
		t.Fatalf("start=%d want prefix=%d", start, prefix)
	}
	if scanned != int64(len(extra)+1) {
		t.Fatalf("scanned=%d extra=%d meta=%#v", scanned, len(extra)+1, meta)
	}
	if meta.IndexedRows != 13 {
		t.Fatalf("rows=%d", meta.IndexedRows)
	}
	if _, err := Lookup(home, "req_new"); err != nil {
		t.Fatal(err)
	}
}

func TestCatchUpAdvancesPastNonIndexedLines(t *testing.T) {
	home := t.TempDir()
	raw := strings.Join([]string{
		`{"timestamp":1,"provider":"openai"}`,
		``,
		`not-json`,
		`{"requestId":"req_ok","provider":"openai","model":"gpt-5","status":200}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := CatchUp(home)
	if err != nil || meta.IndexedRows != 1 || meta.IndexedOffset != int64(len(raw)) {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
}

func TestCatchUpDuplicateRequestIDKeepsLastAndUniqueCount(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_dup","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_dup","provider":"openai","model":"gpt-5.6","status":201}`)); err != nil {
		t.Fatal(err)
	}
	meta, err := CatchUp(home)
	if err != nil || meta.IndexedRows != 1 {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	row, err := Lookup(home, "req_dup")
	if err != nil {
		t.Fatal(err)
	}
	if row["model"] != "gpt-5.6" {
		t.Fatalf("%#v", row)
	}
	again, err := CatchUp(home)
	if err != nil || again.IndexedRows != 1 {
		t.Fatalf("again=%#v err=%v", again, err)
	}
}

func TestCatchUpAndRebuildAreEquivalent(t *testing.T) {
	home := t.TempDir()
	legacy := `{"requestId":"req_legacy","provider":"openai","model":"gpt-5","status":200}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_new","provider":"openai","model":"gpt-5.6","status":200}`)); err != nil {
		t.Fatal(err)
	}
	caught, err := CatchUp(home)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := Rebuild(home)
	if err != nil {
		t.Fatal(err)
	}
	if caught.IndexedRows != rebuilt.IndexedRows || caught.IndexedOffset != rebuilt.IndexedOffset || rebuilt.IndexedOffset != rebuilt.SourceSize {
		t.Fatalf("caught=%#v rebuilt=%#v", caught, rebuilt)
	}
	if _, err := Lookup(home, "req_legacy"); err != nil {
		t.Fatal(err)
	}
	if _, err := Lookup(home, "req_new"); err != nil {
		t.Fatal(err)
	}
}

func TestCatchUpConcurrentCallers(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := CatchUp(home)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	meta := Status(home)
	if meta.IndexedRows != 1 || !meta.CaughtUp {
		t.Fatalf("%#v", meta)
	}
}

func TestCatchUpAfterConcurrentAppends(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 1; i <= 8; i++ {
		wg.Add(1)
		n := i
		go func() {
			defer wg.Done()
			row := []byte(`{"requestId":"req_` + strconv.Itoa(n) + `","provider":"openai","model":"gpt-5","status":200}`)
			if err := l.Append(row); err != nil {
				t.Errorf("append %d: %v", n, err)
			}
		}()
	}
	wg.Wait()
	meta, err := CatchUp(home)
	if err != nil || meta.IndexedRows != 8 || meta.IndexedOffset != meta.SourceSize {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
}

func TestCatchUpRetryAfterRowsRollback(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	SetAfterRowsTestHook(func() error { return errors.New("crash after rows") })
	_, err = CatchUp(home)
	SetAfterRowsTestHook(nil)
	if err == nil {
		t.Fatal("expected rollback")
	}
	st := Status(home)
	if st.IndexedOffset != 0 || st.IndexedRows != 0 {
		t.Fatalf("checkpoint advanced: %#v", st)
	}
	meta, err := CatchUp(home)
	if err != nil || meta.IndexedRows != 1 {
		t.Fatalf("retry=%#v err=%v", meta, err)
	}
}

func TestCatchUpRetryAfterCommitHookRollback(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	SetBeforeCommitTestHook(func() error { return errors.New("crash before commit") })
	_, err = CatchUp(home)
	SetBeforeCommitTestHook(nil)
	if err == nil {
		t.Fatal("expected rollback")
	}
	if st := Status(home); st.IndexedOffset != 0 || st.IndexedRows != 0 {
		t.Fatalf("%#v", st)
	}
	meta, err := CatchUp(home)
	if err != nil || meta.IndexedRows != 1 {
		t.Fatalf("retry=%#v err=%v", meta, err)
	}
}

func TestCatchUpFixedSnapshotThenNextSuffix(t *testing.T) {
	home := t.TempDir()
	keep := []byte(`{"requestId":"req_keep","provider":"openai","model":"gpt-5","status":200}`)
	added := []byte(`{"requestId":"req_new","provider":"openai","model":"gpt-5","status":200}`)
	writer, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(keep); err != nil {
		t.Fatal(err)
	}
	usageledger.SetAfterOpenSourcesTestHook(func() {
		usageledger.SetAfterOpenSourcesTestHook(nil)
		if err := writer.Append(added); err != nil {
			t.Errorf("append: %v", err)
		}
	})
	defer usageledger.SetAfterOpenSourcesTestHook(nil)
	first, err := CatchUp(home)
	if err != nil {
		t.Fatal(err)
	}
	if first.IndexedRows != 1 || first.IndexedOffset != int64(len(keep)+1) {
		t.Fatalf("first=%#v", first)
	}
	if first.CaughtUp || first.PendingBytes <= 0 || first.SourceSize <= first.IndexedOffset {
		t.Fatalf("falsely caught up after later append first=%#v", first)
	}
	second, err := CatchUp(home)
	if err != nil || second.IndexedRows != 2 {
		t.Fatalf("second=%#v err=%v", second, err)
	}
	if _, err := Lookup(home, "req_new"); err != nil {
		t.Fatal(err)
	}
}

func TestCatchUpSequenceGapDoesNotAdvance(t *testing.T) {
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
	meta, err := CatchUp(home)
	if err == nil {
		t.Fatalf("expected gap err meta=%#v", meta)
	}
	if meta.IndexedOffset != 0 {
		t.Fatalf("advanced: %#v", meta)
	}
}

func TestCatchUpV1HealthyMigratesWithoutRescan(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	row := []byte(`{"requestId":"req_old","provider":"openai","model":"gpt-5","status":200}`)
	if err := l.Append(row); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	if err := writeSchemaVersion(home, 1); err != nil {
		t.Fatal(err)
	}
	extra := []byte(`{"requestId":"req_new","provider":"openai","model":"gpt-5","status":200}`)
	if err := l.Append(extra); err != nil {
		t.Fatal(err)
	}
	var start int64
	usageledger.SetEnumerateProgressTestHook(func(s, _, _ int64) { start = s })
	defer usageledger.SetEnumerateProgressTestHook(nil)
	meta, err := CatchUp(home)
	if err != nil || meta.SchemaVersion != 3 || meta.IndexedRows != 2 {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	if start != int64(len(row)+1) {
		t.Fatalf("rescanned prefix start=%d", start)
	}
}

func TestCatchUpV1ErrorRequiresRebuild(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	if err := writeMetaKV(home, map[string]string{
		"schema_version": "1",
		"last_error":     "read_failed",
		"indexed_offset": "999",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := CatchUp(home)
	if !errors.Is(err, ErrRebuildRequired) {
		t.Fatalf("err=%v", err)
	}
	st := Status(home)
	if !st.RebuildRequired {
		t.Fatalf("%#v", st)
	}
}

func TestCatchUpOffsetAheadRequiresRebuild(t *testing.T) {
	home := t.TempDir()
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	if err := writeMetaKV(home, map[string]string{"indexed_offset": "999"}); err != nil {
		t.Fatal(err)
	}
	_, err := CatchUp(home)
	if !errors.Is(err, ErrRebuildRequired) {
		t.Fatalf("err=%v", err)
	}
	st := Status(home)
	if !st.RebuildRequired || st.IndexedOffset != 999 {
		t.Fatalf("%#v", st)
	}
}

func TestCatchUpCorruptSQLiteRequiresRebuild(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, dbName), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := CatchUp(home)
	if err == nil {
		t.Fatal("expected corrupt error")
	}
	st := Status(home)
	if !st.RebuildRequired || st.LastError == "" {
		t.Fatalf("%#v", st)
	}
}

func TestCatchUpMissingLedgerIsHealthyEmpty(t *testing.T) {
	home := t.TempDir()
	meta, err := CatchUp(home)
	if err != nil || meta.IndexedRows != 0 || meta.IndexedOffset != 0 || !meta.CaughtUp {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
}

func TestCatchUpSymlinkDoesNotAdvance(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := CatchUp(home)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.RemoveAll(filepath.Join(home, usageledger.DirName)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, usageledger.DirName)); err != nil {
		t.Skip(err)
	}
	meta, err := CatchUp(home)
	if err == nil {
		t.Fatal("expected symlink error")
	}
	if meta.IndexedOffset != first.IndexedOffset {
		t.Fatalf("advanced on symlink err first=%#v meta=%#v", first, meta)
	}
}

func TestStatusMissingDBIsUninitialized(t *testing.T) {
	home := t.TempDir()
	st := Status(home)
	if st.SchemaVersion != 0 || st.RebuildRequired || st.IndexedOffset != 0 {
		t.Fatalf("%#v", st)
	}
	if !st.CaughtUp || st.SourceSize != 0 {
		t.Fatalf("empty ledger should be caught up: %#v", st)
	}
}

func TestRebuildRepairsUntrustedIndex(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	if err := writeMetaKV(home, map[string]string{"indexed_offset": "999", "rebuild_required": "1"}); err != nil {
		t.Fatal(err)
	}
	meta, err := Rebuild(home)
	if err != nil || meta.IndexedRows != 1 || meta.IndexedOffset != meta.SourceSize || meta.RebuildRequired {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
}

func TestIndexerCoalescesAppendDuringCatchUp(t *testing.T) {
	home := t.TempDir()
	keep := []byte(`{"requestId":"req_keep","provider":"openai","model":"gpt-5","status":200}`)
	added := []byte(`{"requestId":"req_new","provider":"openai","model":"gpt-5","status":200}`)
	writer, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(keep); err != nil {
		t.Fatal(err)
	}
	idx, err := OpenIndexer(home)
	if err != nil {
		t.Fatal(err)
	}
	snapshotOpen := make(chan struct{})
	release := make(chan struct{})
	var unlockOnce sync.Once
	unlock := func() { unlockOnce.Do(func() { close(release) }) }
	defer idx.Close()
	defer unlock()
	usageledger.SetAfterOpenSourcesTestHook(func() {
		usageledger.SetAfterOpenSourcesTestHook(nil)
		close(snapshotOpen)
		<-release
	})
	defer usageledger.SetAfterOpenSourcesTestHook(nil)
	done := make(chan Meta, 1)
	errs := make(chan error, 1)
	go func() {
		meta, err := idx.CatchUp()
		errs <- err
		done <- meta
	}()
	<-snapshotOpen
	if err := writer.Append(added); err != nil {
		t.Fatal(err)
	}
	if err := idx.CatchUpBestEffort(); err != nil {
		t.Fatal(err)
	}
	unlock()
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	<-done
	if _, err := lookupOnly(home, "req_new"); err != nil {
		t.Fatalf("lost append after coalesced catch-up: %v", err)
	}
}

func TestCatchUpDoesNotCountTableOnSteadyState(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	var counts int
	SetCountTableTestHook(func() { counts++ })
	defer SetCountTableTestHook(nil)
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	if counts != 0 {
		t.Fatalf("fresh v2 counted table %d times", counts)
	}
	if err := writeSchemaVersion(home, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	if counts != 1 {
		t.Fatalf("v1 migration counts=%d", counts)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	if counts != 1 {
		t.Fatalf("noop catch-up recounted counts=%d", counts)
	}
	if err := l.Append([]byte(`{"requestId":"req_2","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	meta, err := CatchUp(home)
	if err != nil || meta.IndexedRows != 2 {
		t.Fatalf("suffix=%#v err=%v", meta, err)
	}
	if counts != 1 {
		t.Fatalf("suffix catch-up recounted counts=%d", counts)
	}
	if err := l.Append([]byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5.6","status":201}`)); err != nil {
		t.Fatal(err)
	}
	dup, err := CatchUp(home)
	if err != nil || dup.IndexedRows != 2 {
		t.Fatalf("duplicate=%#v err=%v", dup, err)
	}
	if counts != 1 {
		t.Fatalf("duplicate catch-up recounted counts=%d", counts)
	}
}

func TestRebuildRepairsCorruptSQLite(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, dbName), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := Rebuild(home)
	if err != nil || meta.SchemaVersion != 3 || meta.IndexedRows != 1 || !meta.CaughtUp || meta.RebuildRequired {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	if _, err := Lookup(home, "req_1"); err != nil {
		t.Fatal(err)
	}
}

func TestRebuildRepairsIncompatibleSchema(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, dbName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE requests (legacy TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	meta, err := Rebuild(home)
	if err != nil || meta.IndexedRows != 1 || meta.RebuildRequired {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	if _, err := Lookup(home, "req_1"); err != nil {
		t.Fatal(err)
	}
}

func TestRebuildDoesNotSucceedOnCorruptLedger(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_old","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "usage", "segments"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "usage", "segments", "00000001.jsonl"), []byte(`{"requestId":"req_1","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "usage", "segments", "00000003.jsonl"), []byte(`{"requestId":"req_3","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := Rebuild(home)
	if err == nil {
		t.Fatalf("expected ledger rebuild failure meta=%#v", meta)
	}
	if _, lookErr := lookupOnly(home, "req_old"); lookErr != nil {
		t.Fatalf("failed rebuild destroyed previous index: %v", lookErr)
	}
}

func TestLookupHidesStaleRebuildRequiredRow(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_old","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	if err := writeMetaKV(home, map[string]string{"indexed_offset": "999", "rebuild_required": "1"}); err != nil {
		t.Fatal(err)
	}
	_, err := Lookup(home, "req_old")
	if !errors.Is(err, ErrRebuildRequired) {
		t.Fatalf("served untrusted row err=%v", err)
	}
}

func TestLookupMissingHealthyIDIsNotExist(t *testing.T) {
	home := t.TempDir()
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	_, err := Lookup(home, "req_missing")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err=%v", err)
	}
}

func TestLookupTransientFailureDoesNotProveAbsence(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_new","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Lookup(home, "req_1"); err != nil {
		t.Fatalf("trusted prefix lookup failed: %v", err)
	}
	_, err = Lookup(home, "req_missing")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("absence treated as 404 err=%v", err)
	}
}

func TestMalformedCheckpointRequiresRebuild(t *testing.T) {
	cases := []map[string]string{
		{"schema_version": "garbage"},
		{"indexed_offset": "garbage"},
		{"indexed_offset": "-1"},
		{"rebuild_required": "maybe"},
	}
	for _, kv := range cases {
		kv := kv
		t.Run(fmtKV(kv), func(t *testing.T) {
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","status":200}`+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := CatchUp(home); err != nil {
				t.Fatal(err)
			}
			if err := writeMetaKV(home, kv); err != nil {
				t.Fatal(err)
			}
			_, err := CatchUp(home)
			if !errors.Is(err, ErrRebuildRequired) {
				t.Fatalf("kv=%v err=%v", kv, err)
			}
			st := Status(home)
			if !st.RebuildRequired {
				t.Fatalf("status not rebuild-required kv=%v status=%#v", kv, st)
			}
		})
	}
}

func fmtKV(kv map[string]string) string {
	for key, value := range kv {
		return key + "=" + value
	}
	return "empty"
}

func TestMissingVersionWithRowsRequiresRebuild(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, dbName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM schema_meta WHERE key = 'schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = CatchUp(home)
	if !errors.Is(err, ErrRebuildRequired) {
		t.Fatalf("err=%v", err)
	}
}

func writeSchemaVersion(home string, version int) error {
	return writeMetaKV(home, map[string]string{"schema_version": strconv.Itoa(version)})
}

func writeMetaKV(home string, pairs map[string]string) error {
	db, err := sql.Open("sqlite", filepath.Join(home, dbName))
	if err != nil {
		return err
	}
	defer db.Close()
	for key, value := range pairs {
		if _, err := db.Exec(`INSERT OR REPLACE INTO schema_meta(key, value) VALUES (?, ?)`, key, value); err != nil {
			return err
		}
	}
	return nil
}
