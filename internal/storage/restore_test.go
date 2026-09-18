package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRestorePutsThreadRowsBack(t *testing.T) {
	home := t.TempDir()
	src := writeArchived(t, home, "a.jsonl", "aaaa", time.Now())
	writeThreads(t, home, src)
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Count != 0 {
		t.Fatalf("referenced file was selected: %+v", preview)
	}
	writeArchived(t, home, "b.jsonl", "bbbb", time.Unix(50, 0))
	preview, err = PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Count != 1 {
		t.Fatalf("expected orphan archive, got %+v", preview)
	}
	result, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest})
	if err != nil || !result.OK {
		t.Fatalf("cleanup=%+v err=%v", result, err)
	}
	listed, _ := ListTrash(home)
	if _, err := Restore(context.Background(), home, listed.Entries[0].ID, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	home := t.TempDir()
	src := writeArchived(t, home, "a.jsonl", "aaaa", time.Now())
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest})
	if err != nil || !result.OK {
		t.Fatalf("cleanup=%+v err=%v", result, err)
	}
	listed, err := ListTrash(home)
	if err != nil || len(listed.Entries) != 1 {
		t.Fatalf("trash=%+v", listed)
	}
	restored, err := Restore(context.Background(), home, listed.Entries[0].ID, time.Second)
	if err != nil || !restored.OK || restored.Count != 1 {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
	raw, err := os.ReadFile(src)
	if err != nil || string(raw) != "aaaa" {
		t.Fatalf("restored file=%s err=%v", raw, err)
	}
	listed, err = ListTrash(home)
	if err != nil || len(listed.Entries) != 0 {
		t.Fatalf("trash after restore=%+v", listed)
	}
}

func TestRestoreDestExistsPreservesDestination(t *testing.T) {
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
	_, err = Restore(context.Background(), home, listed.Entries[0].ID, time.Second)
	if err == nil || asError(err).Code != CodeDestExists {
		t.Fatalf("expected dest_exists, got %v", err)
	}
	raw, _ := os.ReadFile(src)
	if string(raw) != "newer" {
		t.Fatalf("destination overwritten: %s", raw)
	}
}

func TestRestoreRejectsAbsoluteManifestPath(t *testing.T) {
	home := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	id := "1700000000001-bbbbbbbb"
	dir := filepath.Join(home, ".trash", id)
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"id":"` + id + `","epoch":"` + id + `","mode":"quarantine","files":[{"relPath":"` + filepath.ToSlash(outside) + `","bytes":4}]}`)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Restore(context.Background(), home, id, time.Second)
	if err == nil {
		t.Fatal("accepted absolute path")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("outside mutated")
	}
}

func TestRestoreTimeout(t *testing.T) {
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
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	result, err := Restore(ctx, home, listed.Entries[0].ID, DefaultRestoreTimeout)
	if err == nil || asError(err).Code != CodeRestoreWorkerTimeout {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.OK {
		t.Fatal("timeout reported success")
	}
}

func TestRestoreAbort(t *testing.T) {
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
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := Restore(ctx, home, listed.Entries[0].ID, time.Second)
	if err == nil || asError(err).Code != CodeRestoreWorkerAborted && asError(err).Code != CodeRestoreWorkerTimeout {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.OK {
		t.Fatal("abort reported success")
	}
}

func TestConcurrentRestoreSerialized(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Now())
	engine := NewEngine()
	preview, err := engine.Preview(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Cleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest}); err != nil {
		t.Fatal(err)
	}
	listed, _ := engine.ListTrash(home)
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := engine.Restore(context.Background(), home, listed.Entries[0].ID)
			errCh <- err
		}()
	}
	var ok, busy int
	for i := 0; i < 2; i++ {
		err := <-errCh
		if err == nil {
			ok++
			continue
		}
		code := asError(err).Code
		if code == CodeStorageMutationBusy || code == CodeMissingTrash {
			busy++
			continue
		}
		t.Fatalf("unexpected %v", err)
	}
	if ok != 1 {
		t.Fatalf("ok=%d busy=%d", ok, busy)
	}
}

func TestCleanupBlockedWhileRestoreHeld(t *testing.T) {
	home := t.TempDir()
	writeArchived(t, home, "a.jsonl", "aaaa", time.Now())
	engine := NewEngine()
	if !engine.Lock.TryAcquire("restore") {
		t.Fatal("lock")
	}
	preview, err := engine.Preview(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Cleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest})
	engine.Lock.Release()
	if err == nil || asError(err).Code != CodeRestorePendingOverlap && asError(err).Code != CodeStorageMutationBusy {
		t.Fatalf("err=%v", err)
	}
}

func TestLockReleasedAfterFailure(t *testing.T) {
	home := t.TempDir()
	engine := NewEngine()
	_, err := engine.Cleanup(home, CleanupRequest{Percent: 100, Mode: "nope", Digest: "abcd"})
	if err == nil {
		t.Fatal("expected failure")
	}
	if engine.Lock.Holder() != "" {
		t.Fatal("lock held after failure")
	}
}
