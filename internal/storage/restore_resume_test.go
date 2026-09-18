package storage

import (
	"context"
	"database/sql"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"
)

func sqlOpen(home string) (*sql.DB, error) {
	return sql.Open("sqlite", filepath.Join(home, "state_5.sqlite"))
}

func TestRestoreRetriesAfterPartialFailure(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	writeArchived(t, home, "b.jsonl", "bbbb", time.Unix(200, 0))
	writeArchived(t, home, "c.jsonl", "cccc", time.Unix(300, 0))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest}); err != nil {
		t.Fatal(err)
	}
	listed, _ := ListTrash(home)
	id := listed.Entries[0].ID
	root := mustRoot(t, home)
	man, err := loadManifest(root, id)
	if err != nil {
		t.Fatal(err)
	}
	first := man.Files[0]
	src := filepath.Join(home, filepath.FromSlash(path.Join(trashDirName, id, filesDirName, first.RelPath)))
	dest := filepath.Join(home, filepath.FromSlash(first.RelPath))
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(src, dest); err != nil {
		t.Fatal(err)
	}
	man.Files[0].Phase = "restored"
	man.Status = "partial"
	if err := writeManifest(root, man); err != nil {
		t.Fatal(err)
	}
	second, err := Restore(context.Background(), home, id, time.Second)
	if err != nil || !second.OK {
		t.Fatalf("retry=%+v err=%v", second, err)
	}
	if second.AlreadyRestored < 1 || second.Count != 2 {
		t.Fatalf("already=%d count=%d", second.AlreadyRestored, second.Count)
	}
	for _, name := range []string{"a.jsonl", "b.jsonl", "c.jsonl"} {
		if _, err := os.Stat(filepath.Join(home, "archived_sessions", name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestRestoreRetriesAfterAbort(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	writeArchived(t, home, "b.jsonl", "bbbb", time.Unix(200, 0))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest}); err != nil {
		t.Fatal(err)
	}
	listed, _ := ListTrash(home)
	id := listed.Entries[0].ID
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	first, err := Restore(ctx, home, id, time.Second)
	if err == nil || first.OK || asError(err).Code != CodeRestoreWorkerAborted {
		t.Fatalf("abort should fail: %+v err=%v", first, err)
	}
	second, err := Restore(context.Background(), home, id, time.Second)
	if err != nil || !second.OK {
		t.Fatalf("retry after abort=%+v err=%v", second, err)
	}
}

func TestRestoreRetriesAfterTimeoutAndAbort(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	writeArchived(t, home, "b.jsonl", "bbbb", time.Unix(200, 0))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest}); err != nil {
		t.Fatal(err)
	}
	listed, _ := ListTrash(home)
	id := listed.Entries[0].ID
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	first, err := Restore(expired, home, id, DefaultRestoreTimeout)
	if err == nil || first.OK {
		t.Fatalf("timeout should fail: %+v err=%v", first, err)
	}
	second, err := Restore(context.Background(), home, id, time.Second)
	if err != nil || !second.OK {
		t.Fatalf("retry after timeout=%+v err=%v", second, err)
	}
}

func TestRestoreRetriesDBAfterFilesMoved(t *testing.T) {
	home := t.TempDir()
	src := writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	writeArchived(t, home, "b.jsonl", "bbbb", time.Unix(200, 0))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest}); err != nil {
		t.Fatal(err)
	}
	listed, _ := ListTrash(home)
	id := listed.Entries[0].ID
	root := mustRoot(t, home)
	man, err := loadManifest(root, id)
	if err != nil {
		t.Fatal(err)
	}
	for i, file := range man.Files {
		srcPath := filepath.Join(home, filepath.FromSlash(path.Join(trashDirName, id, filesDirName, file.RelPath)))
		dest := filepath.Join(home, filepath.FromSlash(file.RelPath))
		if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(srcPath, dest); err != nil {
			t.Fatal(err)
		}
		man.Files[i].Phase = "restored"
	}
	man.Status = "partial"
	man.Threads = []map[string]any{{"id": "restored-a", "rollout_path": src}}
	if err := writeManifest(root, man); err != nil {
		t.Fatal(err)
	}
	writeThreads(t, home, filepath.Join(home, "seed.jsonl"))
	db, err := sqlOpen(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_restore BEFORE INSERT ON threads BEGIN SELECT RAISE(ABORT, 'fail'); END`); err != nil {
		t.Fatal(err)
	}
	first, err := Restore(context.Background(), home, id, time.Second)
	if err == nil || first.OK {
		_ = db.Close()
		t.Fatalf("db fail should block: %+v err=%v", first, err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_restore`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	second, err := Restore(context.Background(), home, id, time.Second)
	if err != nil || !second.OK {
		t.Fatalf("retry db reconcile=%+v err=%v", second, err)
	}
	if second.Count != 0 || second.AlreadyRestored < 2 {
		t.Fatalf("expected db-only retry already=%d count=%d", second.AlreadyRestored, second.Count)
	}
}

func TestRestoreUnfinishedDestExistsStaysConflict(t *testing.T) {
	home := t.TempDir()
	src := writeArchived(t, home, "a.jsonl", "orig", time.Now())
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("newer"), 0o600); err != nil {
		t.Fatal(err)
	}
	listed, _ := ListTrash(home)
	first, err := Restore(context.Background(), home, listed.Entries[0].ID, time.Second)
	if err == nil || asError(err).Code != CodeDestExists {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := Restore(context.Background(), home, listed.Entries[0].ID, time.Second)
	if err == nil || asError(err).Code != CodeDestExists {
		t.Fatalf("retry=%+v err=%v", second, err)
	}
}

func TestRestoreCompletedRetryIsMissingTrash(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Now())
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest}); err != nil {
		t.Fatal(err)
	}
	listed, _ := ListTrash(home)
	id := listed.Entries[0].ID
	first, err := Restore(context.Background(), home, id, time.Second)
	if err != nil || !first.OK {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := Restore(context.Background(), home, id, time.Second)
	if err == nil || asError(err).Code != CodeMissingTrash {
		t.Fatalf("completed retry=%+v err=%v", second, err)
	}
}

func TestPartialPermanentDeleteLeavesRecoverableRemainder(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	writeArchived(t, home, "b.jsonl", "bbbb", time.Unix(200, 0))
	writeArchived(t, home, "c.jsonl", "cccc", time.Unix(300, 0))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	result, err := ExecuteCleanup(home, CleanupRequest{
		Percent: 100,
		Mode:    ModePermanent,
		Digest:  preview.Digest,
		Remove: func(path string) error {
			n++
			if n > 1 {
				return os.ErrPermission
			}
			return os.Remove(path)
		},
	})
	if err == nil || result.OK {
		t.Fatalf("expected partial permanent: %+v err=%v", result, err)
	}
	listed, err := ListTrash(home)
	if err != nil || len(listed.Entries) != 1 {
		t.Fatalf("partial permanent must remain listable: %+v err=%v", listed, err)
	}
	restored, err := Restore(context.Background(), home, listed.Entries[0].ID, time.Second)
	if err != nil || !restored.OK {
		t.Fatalf("remaining files should restore: %+v err=%v", restored, err)
	}
	if restored.Count < 1 {
		t.Fatalf("expected surviving files restored, got %+v", restored)
	}
}

func TestListTrashReportsCorruptManifest(t *testing.T) {
	home := t.TempDir()
	id := "1700000000099-aaaaaaaa"
	dir := filepath.Join(home, ".trash", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	listed, err := ListTrash(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Entries) != 0 {
		t.Fatalf("entries=%+v", listed.Entries)
	}
	if len(listed.RecoveryNeeded) == 0 {
		t.Fatal("expected recoveryNeeded")
	}
}

func TestListTrashMissingManifestReportsRecovery(t *testing.T) {
	home := t.TempDir()
	id := "1700000000098-bbbbbbbb"
	dir := filepath.Join(home, ".trash", id, "files", "archived_sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.jsonl"), []byte("aaaa"), 0o600); err != nil {
		t.Fatal(err)
	}
	listed, err := ListTrash(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Entries) != 0 {
		t.Fatalf("entries=%+v", listed.Entries)
	}
	if len(listed.RecoveryNeeded) == 0 {
		t.Fatal("missing manifest must be recoveryNeeded, not empty")
	}
}

func TestListTrashValidManifestWithoutJournalIsVisible(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Now())
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest}); err != nil {
		t.Fatal(err)
	}
	listed, err := ListTrash(home)
	if err != nil || len(listed.Entries) != 1 {
		t.Fatalf("list=%+v err=%v", listed, err)
	}
	journal := filepath.Join(home, ".trash", listed.Entries[0].ID, "journal.jsonl")
	_ = os.Remove(journal)
	listed, err = ListTrash(home)
	if err != nil || len(listed.Entries) != 1 {
		t.Fatalf("manifest without journal vanished: %+v err=%v", listed, err)
	}
}

func TestQuarantineFreedBytesAreZero(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Now())
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest})
	if err != nil || !result.OK {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.FreedBytes != 0 {
		t.Fatalf("quarantine claimed freedBytes=%d", result.FreedBytes)
	}
	if result.RemovedArchivedBytes != 4 && result.Bytes != 4 {
		t.Fatalf("removed bytes=%d", result.Bytes)
	}
}
