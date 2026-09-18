package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReferenceAppearsBeforeFirstRename(t *testing.T) {
	home := t.TempDir()
	path := writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	writeThreads(t, home, filepath.Join(home, "unrelated.jsonl"))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExecuteCleanup(home, CleanupRequest{
		Percent: 100,
		Mode:    ModePermanent,
		Digest:  preview.Digest,
		AfterLock: func(lock *stateLock) {
			if lock == nil {
				t.Fatal("missing state lock")
			}
			if err := lock.InsertThread("live-a", path); err != nil {
				t.Fatal(err)
			}
		},
	})
	if err == nil || asError(err).Code != CodeReferencedHistory {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("file removed")
	}
	if !threadExists(t, home, "live-a") {
		t.Fatal("thread row was destroyed")
	}
	if !threadExists(t, home, "t1") {
		t.Fatal("unrelated thread row was destroyed")
	}
}

func TestReferenceAppearsBeforeLockDuringExecute(t *testing.T) {
	home := t.TempDir()
	path := writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExecuteCleanup(home, CleanupRequest{
		Percent: 100,
		Mode:    ModeQuarantine,
		Digest:  preview.Digest,
		BeforeLock: func() {
			writeThreads(t, home, path)
		},
	})
	if err == nil || asError(err).Code != CodeReferencedHistory {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("file removed")
	}
	if !threadExists(t, home, "t1") {
		t.Fatal("thread row was destroyed")
	}
}

func TestReferenceAppearsBetweenFileMutations(t *testing.T) {
	home := t.TempDir()
	a := writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	b := writeArchived(t, home, "b.jsonl", "bbbb", time.Unix(200, 0))
	writeThreads(t, home, filepath.Join(home, "unrelated.jsonl"))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExecuteCleanup(home, CleanupRequest{
		Percent: 100,
		Mode:    ModePermanent,
		Digest:  preview.Digest,
		AfterMove: func(lock *stateLock, moved classifiedFile) {
			if filepath.Base(moved.RelPath) != "a.jsonl" {
				return
			}
			if lock == nil || lock.conn == nil {
				writeThreads(t, home, b)
				return
			}
			_ = lock.InsertThread("live-b", b)
		},
	})
	if err == nil || asError(err).Code != CodeReferencedHistory {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(b); err != nil {
		t.Fatal("referenced file was destroyed")
	}
	if !threadExists(t, home, "live-b") {
		t.Fatal("new thread row was destroyed")
	}
	if !threadExists(t, home, "t1") {
		t.Fatal("unrelated thread row was destroyed")
	}
	_ = a
}

func threadExists(t *testing.T, home, id string) bool {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(home, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM threads WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}
