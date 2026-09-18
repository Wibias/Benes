package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func writeArchived(t *testing.T, home, name, body string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(home, "archived_sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPreviewAndCleanupQuarantine(t *testing.T) {
	home := t.TempDir()
	old := time.Unix(1_700_000_000, 0)
	writeArchived(t, home, "a.jsonl", "aaaa", old)
	writeArchived(t, home, "b.jsonl", "bbbbbbbb", old.Add(time.Hour))
	writeArchived(t, home, "c.jsonl", "cc", old.Add(2*time.Hour))
	preview, err := PreviewPercent(home, 25)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Count != 1 || preview.Candidates[0].RelPath != "archived_sessions/a.jsonl" {
		t.Fatalf("preview=%+v", preview)
	}
	result, err := ExecuteCleanup(home, CleanupRequest{Percent: 25, Mode: ModeQuarantine, Digest: preview.Digest})
	if err != nil || !result.OK || result.Count != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(home, "archived_sessions", "a.jsonl")); !os.IsNotExist(err) {
		t.Fatal("source should have moved")
	}
	listed, err := ListTrash(home)
	if err != nil || len(listed.Entries) != 1 || listed.Entries[0].FileCount != 1 {
		t.Fatalf("trash=%+v err=%v", listed, err)
	}
}

func TestInvalidDigestAndMode(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "a", time.Now())
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: "nope"}); err == nil {
		t.Fatal("expected stale/invalid")
	}
	if _, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: "shuffle", Digest: preview.Digest}); err == nil {
		t.Fatal("expected invalid mode")
	}
	if _, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: ""}); err == nil {
		t.Fatal("expected invalid digest")
	}
}

func TestStalePreviewWhenCandidateRemovedOrChanged(t *testing.T) {
	home := t.TempDir()
	path := writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed-size"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest})
	if err == nil || asError(err).Code != CodeStalePreview {
		t.Fatalf("size change: %v", err)
	}
	preview, err = PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, err = ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest})
	if err == nil || asError(err).Code != CodeStalePreview {
		t.Fatalf("removed: %v", err)
	}
}

func TestStalePreviewWhenSetChanges(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "b.jsonl", "b", time.Unix(200, 0))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	writeArchived(t, home, "a.jsonl", "a", time.Unix(100, 0))
	_, err = ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest})
	if err == nil || asError(err).Code != CodeStalePreview {
		t.Fatalf("set change: %v", err)
	}
}

func TestReferencedHistoryCheckedAtExecute(t *testing.T) {
	home := t.TempDir()
	path := writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	writeThreads(t, home, path)
	_, err = ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModePermanent, Digest: preview.Digest})
	if err == nil || asError(err).Code != CodeReferencedHistory {
		t.Fatalf("expected referenced_history, got %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("referenced file was deleted")
	}
}

func TestAlreadyReferencedExcludedFromPreview(t *testing.T) {
	home := t.TempDir()
	path := writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	writeArchived(t, home, "b.jsonl", "bbbb", time.Unix(200, 0))
	writeThreads(t, home, path)
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Count != 1 || preview.Candidates[0].RelPath != "archived_sessions/b.jsonl" {
		t.Fatalf("preview=%+v", preview)
	}
}

func TestPermanentDeleteCreatesNoTrashEntry(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Now())
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModePermanent, Digest: preview.Digest})
	if err != nil || !result.OK || result.TrashDir != "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	listed, err := ListTrash(home)
	if err != nil || len(listed.Entries) != 0 {
		t.Fatalf("trash=%+v", listed)
	}
}

func TestPartialRenameFailureKeepsManifest(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	writeArchived(t, home, "b.jsonl", "bbbb", time.Unix(200, 0))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	rename := func(oldpath, newpath string) error {
		n++
		if n > 1 {
			return os.ErrPermission
		}
		return os.Rename(oldpath, newpath)
	}
	result, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest, Rename: rename})
	if err == nil || asError(err).Code != CodeFSFailed || result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.Count < 1 || result.TrashDir == "" {
		t.Fatalf("partial not recorded: %+v", result)
	}
	listed, err := ListTrash(home)
	if err != nil || len(listed.Entries) != 1 || listed.Entries[0].FileCount < 1 {
		t.Fatalf("trash=%+v", listed)
	}
}

func TestCrashBetweenMoveAndManifestReconciles(t *testing.T) {
	home := t.TempDir()
	root, err := OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	id, dir, err := createTrashDir(root, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "files", "archived_sessions", "a.jsonl"), []byte("aa"), 0o600); err != nil {
		if err := os.MkdirAll(filepath.Join(dir, "files", "archived_sessions"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "files", "archived_sessions", "a.jsonl"), []byte("aa"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	man := Manifest{
		ID: id, Epoch: id, Mode: ModeQuarantine, Status: "moving",
		Files:     []ManifestFile{{RelPath: "archived_sessions/a.jsonl", Bytes: 2, Phase: "planned"}},
		FileCount: 1, Bytes: 2,
	}
	if err := writeManifest(root, man); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileTrash(home); err != nil {
		t.Fatal(err)
	}
	listed, err := ListTrash(home)
	if err != nil || len(listed.Entries) != 1 || listed.Entries[0].ID != id || listed.Entries[0].FileCount != 1 {
		t.Fatalf("entries=%+v err=%v", listed, err)
	}
}

func TestHardlinkCountedOnceAndMovedTogether(t *testing.T) {
	home := t.TempDir()
	a := writeArchived(t, home, "a.jsonl", "shared", time.Unix(100, 0))
	b := filepath.Join(home, "archived_sessions", "b.jsonl")
	if err := os.Link(a, b); err != nil {
		t.Skip("hardlinks not available")
	}
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Count != 2 || preview.Bytes != 6 {
		t.Fatalf("preview count=%d bytes=%d", preview.Count, preview.Bytes)
	}
	result, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest})
	if err != nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := os.Lstat(a); !os.IsNotExist(err) {
		t.Fatal("a remains")
	}
	if _, err := os.Lstat(b); !os.IsNotExist(err) {
		t.Fatal("b remains")
	}
}

func TestReportPreviewPolicyShareArchivedClassification(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Now())
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "sessions", "live.jsonl"), []byte("live"), 0o600); err != nil {
		t.Fatal(err)
	}
	bytes, count, _, err := ArchivedBytes(home)
	if err != nil || bytes != 4 || count != 1 {
		t.Fatalf("archived=%d count=%d err=%v", bytes, count, err)
	}
	preview, err := PreviewPercent(home, 100)
	if err != nil || preview.Bytes != 4 || preview.Count != 1 {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
}

func TestLargestFileOrderDeterministic(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "b.jsonl", "1234", time.Now())
	writeArchived(t, home, "a.jsonl", "1234", time.Now())
	report, err := Scan(home)
	if err != nil {
		t.Fatal(err)
	}
	var largest []LargestEntry
	for _, bucket := range report.Buckets {
		if bucket.Key == BucketArchivedSessions {
			largest = bucket.Largest
		}
	}
	if len(largest) < 2 || largest[0].Path != "archived_sessions/a.jsonl" {
		t.Fatalf("largest=%+v", largest)
	}
}

func TestConcurrentCleanupSerialized(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Now())
	engine := NewEngine()
	preview, err := engine.Preview(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := engine.Cleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest})
			errCh <- err
		}()
	}
	var busy, ok int
	for i := 0; i < 2; i++ {
		err := <-errCh
		if err == nil {
			ok++
			continue
		}
		if asError(err).Code == CodeStorageMutationBusy || asError(err).Code == CodeStalePreview {
			busy++
			continue
		}
		t.Fatalf("unexpected %v", err)
	}
	if ok != 1 {
		t.Fatalf("ok=%d busy=%d", ok, busy)
	}
}

func writeThreads(t *testing.T, home, rollout string) {
	t.Helper()
	dbPath := filepath.Join(home, "state_5.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS threads (id TEXT PRIMARY KEY, rollout_path TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads VALUES ('t1', ?)`, rollout); err != nil {
		t.Fatal(err)
	}
}
