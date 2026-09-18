package storage

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"
)

func TestCrashAfterRenameBeforeProgressStillRecoverable(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExecuteCleanup(home, CleanupRequest{
		Percent: 100,
		Mode:    ModeQuarantine,
		Digest:  preview.Digest,
		AfterRename: func(moved int) error {
			if moved == 1 {
				return errors.New("crash before persist")
			}
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected persist failure")
	}
	listed, err := ListTrash(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Entries) == 0 && len(listed.RecoveryNeeded) == 0 {
		t.Fatal("moved file not discoverable after crash")
	}
	id := ""
	if len(listed.Entries) > 0 {
		id = listed.Entries[0].ID
	} else {
		id = listed.RecoveryNeeded[0].ID
	}
	if id == "" {
		t.Fatal("no trash id")
	}
	if err := ReconcileTrash(home); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(home, filepath.FromSlash(path.Join(trashDirName, id, filesDirName, "archived_sessions/a.jsonl")))
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("quarantined file missing: %v", err)
	}
}

func TestCrashAfterDBCommitKeepsThreadSnapshot(t *testing.T) {
	home := t.TempDir()
	src := writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	writeThreads(t, home, src)
	root := mustRoot(t, home)
	files, _, err := listArchived(root)
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%+v err=%v", files, err)
	}
	lock, err := lockState(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	_, err = applyCleanup(root, lock, files, CleanupRequest{
		Mode: ModeQuarantine,
		AfterLock: func(lock *stateLock) {
			if err := lock.DeleteThreadIDs([]string{"t1"}); err != nil {
				t.Fatal(err)
			}
		},
		AfterCommit: func() error {
			return errors.New("crash after commit")
		},
	})
	if err == nil {
		t.Fatal("expected crash after commit")
	}
	listed, err := ListTrash(home)
	if err != nil || len(listed.Entries) == 0 {
		t.Fatalf("trash missing after crash: %+v err=%v", listed, err)
	}
	id := listed.Entries[0].ID
	man, err := loadManifest(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(man.Threads) == 0 {
		t.Fatal("thread snapshot missing from durable manifest")
	}
	srcTrash := filepath.Join(home, filepath.FromSlash(path.Join(trashDirName, id, filesDirName, "archived_sessions/a.jsonl")))
	if _, err := os.Stat(srcTrash); err != nil {
		t.Fatalf("quarantined file missing: %v", err)
	}
	if threadExists(t, home, "t1") {
		t.Fatal("thread row should already be deleted")
	}
	first, err := Restore(context.Background(), home, id, time.Second)
	if err != nil || !first.OK {
		t.Fatalf("restore after crash=%+v err=%v", first, err)
	}
	if !threadExists(t, home, "t1") {
		t.Fatal("thread row was not restored")
	}
	second, err := Restore(context.Background(), home, id, time.Second)
	if err == nil || asError(err).Code != CodeMissingTrash {
		t.Fatalf("repeated restore=%+v err=%v", second, err)
	}
	if !threadExists(t, home, "t1") {
		t.Fatal("repeated recovery destroyed thread row")
	}
}

func TestProgressWriteFailureLeavesPlannedManifest(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Now())
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExecuteCleanup(home, CleanupRequest{
		Percent: 100,
		Mode:    ModeQuarantine,
		Digest:  preview.Digest,
		AfterPersist: func(moved int) error {
			return errors.New("journal failed")
		},
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	listed, err := ListTrash(home)
	if err != nil || (len(listed.Entries) == 0 && len(listed.RecoveryNeeded) == 0) {
		t.Fatalf("list=%+v err=%v", listed, err)
	}
}
