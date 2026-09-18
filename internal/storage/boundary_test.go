package storage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfineRejectsTraversalAndAbs(t *testing.T) {
	root, err := OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"../outside", "..\\outside", "/tmp/x", `C:\outside`, `\\?\C:\outside`, "archived_sessions/../../etc/passwd"} {
		if _, _, err := root.ConfineRel(rel); err == nil {
			t.Fatalf("accepted %q", rel)
		}
	}
	if _, _, err := root.ConfineRel("archived_sessions/ok.jsonl"); err != nil {
		t.Fatal(err)
	}
}

func TestSymlinkToOutsideIsNotFollowedOnCleanup(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	arch := filepath.Join(home, "archived_sessions")
	if err := os.MkdirAll(arch, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(arch, "link.jsonl")
	if err := os.Symlink(secret, link); err != nil {
		t.Skip("symlinks not available")
	}
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Count != 1 {
		t.Fatalf("preview=%+v", preview)
	}
	result, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("outside target was mutated: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("symlink should have moved: %v", err)
	}
}

func TestDirectorySymlinkIsNotWalked(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "nope.jsonl"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, "archived_sessions")); err != nil {
		t.Skip("symlinks not available")
	}
	report, err := Scan(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, bucket := range report.Buckets {
		if bucket.Key == BucketArchivedSessions && bucket.FileCount != 0 {
			t.Fatalf("walked symlink dir: %+v", bucket)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "nope.jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsJunctionNotWalked(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "nope.jsonl"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "archived_sessions")
	if err := makeJunction(link, outside); err != nil {
		t.Skip(err)
	}
	report, err := Scan(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, bucket := range report.Buckets {
		if bucket.Key == BucketArchivedSessions && bucket.FileCount != 0 {
			t.Fatalf("walked junction: %+v", bucket)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "nope.jsonl")); err != nil {
		t.Fatal("outside mutated")
	}
}

func TestMaliciousTrashIDRejected(t *testing.T) {
	root, err := OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../x", "x/y", "..", "abc", "1-ZZ", ""} {
		if validTrashID(id) {
			t.Fatalf("accepted id %q", id)
		}
		if _, err := loadManifest(root, id); err == nil {
			t.Fatalf("loaded id %q", id)
		}
	}
}

func makeJunction(link, target string) error {
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func TestCorruptManifestEscapeRejected(t *testing.T) {
	home := t.TempDir()
	root, err := OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	id := "1700000000000-aaaaaaaa"
	dir := filepath.Join(home, ".trash", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"id":"` + id + `","epoch":"` + id + `","mode":"quarantine","files":[{"relPath":"../../outside","bytes":1}]}`)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadManifest(root, id); err == nil {
		t.Fatal("accepted escape path")
	}
	_, restoreErr := Restore(context.Background(), home, id, DefaultRestoreTimeout)
	if restoreErr == nil {
		t.Fatal("restore accepted escape")
	}
}
