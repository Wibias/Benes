package storage

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func writeSentinel(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertUnchanged(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != want {
		t.Fatalf("outside mutated path=%s data=%q err=%v", path, raw, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if e.Name() != filepath.Base(path) && e.Name() != filepath.Base(filepath.Dir(path)) {
			continue
		}
	}
}

func TestCleanupRefusesTrashSymlinkToOutside(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	sentinel := writeSentinel(t, outside, "keep.txt", "keep-bytes")
	writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	if err := os.Symlink(outside, filepath.Join(home, ".trash")); err != nil {
		t.Skip("symlinks not available")
	}
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest})
	if err == nil {
		t.Fatal("cleanup followed .trash symlink")
	}
	assertUnchanged(t, sentinel, "keep-bytes")
	outsideEntries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(outsideEntries) != 1 || outsideEntries[0].Name() != "keep.txt" {
		t.Fatalf("outside dir mutated: %+v", outsideEntries)
	}
	if _, err := os.Stat(filepath.Join(home, "archived_sessions", "a.jsonl")); err != nil {
		t.Fatal("source should remain")
	}
}

func TestCleanupRefusesTrashJunctionToOutside(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	home := t.TempDir()
	outside := t.TempDir()
	sentinel := writeSentinel(t, outside, "keep.txt", "keep-bytes")
	writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	if err := makeJunction(filepath.Join(home, ".trash"), outside); err != nil {
		t.Skip(err)
	}
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest})
	if err == nil {
		t.Fatal("cleanup followed .trash junction")
	}
	assertUnchanged(t, sentinel, "keep-bytes")
	outsideEntries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(outsideEntries) != 1 {
		t.Fatalf("outside dir mutated: %+v", outsideEntries)
	}
}

func TestRestoreRefusesDestinationParentSymlink(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	sentinel := writeSentinel(t, outside, "keep.txt", "keep-bytes")
	writeArchived(t, home, "nested.jsonl", "aaaa", time.Now())
	src := filepath.Join(home, "archived_sessions", "nested.jsonl")
	sub := filepath.Join(home, "archived_sessions", "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(sub, "nested.jsonl")
	if err := os.Rename(src, nested); err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, sub); err != nil {
		t.Skip("symlinks not available")
	}
	listed, _ := ListTrash(home)
	_, err = Restore(context.Background(), home, listed.Entries[0].ID, time.Second)
	if err == nil {
		t.Fatal("restore followed dest parent symlink")
	}
	assertUnchanged(t, sentinel, "keep-bytes")
}

func TestRestoreRefusesDestinationParentJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows only")
	}
	home := t.TempDir()
	outside := t.TempDir()
	sentinel := writeSentinel(t, outside, "keep.txt", "keep-bytes")
	writeArchived(t, home, "a.jsonl", "aaaa", time.Now())
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest}); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(home, "archived_sessions", "gone")
	if err := os.MkdirAll(filepath.Dir(sub), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := makeJunction(filepath.Join(home, "archived_sessions", "sub"), outside); err != nil {
		t.Skip(err)
	}
	listed, _ := ListTrash(home)
	man, err := loadManifest(mustRoot(t, home), listed.Entries[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	man.Files[0].RelPath = "archived_sessions/sub/a.jsonl"
	if err := writeManifest(mustRoot(t, home), man); err != nil {
		t.Fatal(err)
	}
	_, err = Restore(context.Background(), home, listed.Entries[0].ID, time.Second)
	if err == nil {
		t.Fatal("restore followed dest parent junction")
	}
	assertUnchanged(t, sentinel, "keep-bytes")
}

func TestCleanupParentReplacedByLinkBeforeRename(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows junction replacement")
	}
	home := t.TempDir()
	outside := t.TempDir()
	sentinel := writeSentinel(t, outside, "keep.txt", "keep-bytes")
	writeArchived(t, home, "a.jsonl", "aaaa", time.Unix(100, 0))
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ExecuteCleanup(home, CleanupRequest{
		Percent: 100,
		Mode:    ModeQuarantine,
		Digest:  preview.Digest,
		BeforeRename: func() {
			arch := filepath.Join(home, "archived_sessions")
			tmp := arch + ".real"
			_ = os.Rename(arch, tmp)
			_ = makeJunction(arch, outside)
		},
	})
	if err == nil {
		t.Fatal("cleanup renamed through replaced parent")
	}
	assertUnchanged(t, sentinel, "keep-bytes")
}

func TestWriteConfinedDoesNotFollowTmpSymlink(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	sentinel := writeSentinel(t, outside, "keep.txt", "keep-bytes")
	id := "1700000000100-dddddddd"
	dir := filepath.Join(home, ".trash", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sentinel, filepath.Join(dir, "manifest.json.tmp")); err != nil {
		t.Skip("symlinks not available")
	}
	root := mustRoot(t, home)
	man := Manifest{
		ID: id, Epoch: id, Mode: ModeQuarantine, Status: "moving",
		Files:     []ManifestFile{{RelPath: "archived_sessions/a.jsonl", Bytes: 1, Phase: "planned"}},
		FileCount: 1, Bytes: 1,
	}
	if err := writeManifest(root, man); err != nil {
		assertUnchanged(t, sentinel, "keep-bytes")
		return
	}
	assertUnchanged(t, sentinel, "keep-bytes")
	loaded, err := loadManifest(root, id)
	if err != nil || loaded.ID != id {
		t.Fatalf("manifest not written safely: %+v err=%v", loaded, err)
	}
}

func TestLoadManifestDoesNotFollowLeafSymlink(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	id := "1700000000101-eeeeeeee"
	outsideMan := filepath.Join(outside, "manifest.json")
	body := []byte(`{"id":"1700000000101-eeeeeeee","epoch":"1700000000101-eeeeeeee","mode":"quarantine","status":"complete","files":[{"relPath":"archived_sessions/a.jsonl","bytes":4}],"fileCount":1,"bytes":4}`)
	if err := os.WriteFile(outsideMan, body, 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".trash", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideMan, filepath.Join(dir, "manifest.json")); err != nil {
		t.Skip("symlinks not available")
	}
	listed, err := ListTrash(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Entries) != 0 {
		t.Fatalf("followed outside manifest: %+v", listed.Entries)
	}
	if len(listed.RecoveryNeeded) == 0 {
		t.Fatal("expected recoveryNeeded for leaf symlink manifest")
	}
	_, err = Restore(context.Background(), home, id, time.Second)
	if err == nil {
		t.Fatal("restore used outside manifest")
	}
	_ = ReconcileTrash(home)
	raw, err := os.ReadFile(outsideMan)
	if err != nil || string(raw) != string(body) {
		t.Fatalf("outside manifest mutated: %s err=%v", raw, err)
	}
	listed, err = ListTrash(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Entries) != 0 {
		t.Fatalf("reconcile adopted outside manifest: %+v", listed.Entries)
	}
}

func TestRestoreNoReplaceWhenDestAppearsBeforeMove(t *testing.T) {
	home := t.TempDir()
	src := writeArchived(t, home, "a.jsonl", "orig", time.Now())
	preview, err := PreviewPercent(home, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteCleanup(home, CleanupRequest{Percent: 100, Mode: ModeQuarantine, Digest: preview.Digest}); err != nil {
		t.Fatal(err)
	}
	listed, err := ListTrash(home)
	if err != nil || len(listed.Entries) != 1 {
		t.Fatalf("trash=%+v err=%v", listed, err)
	}
	id := listed.Entries[0].ID
	result, err := restore(context.Background(), home, id, time.Second, nil, func() error {
		return os.WriteFile(src, []byte("newer"), 0o600)
	})
	if err == nil || asError(err).Code != CodeDestExists {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	raw, err := os.ReadFile(src)
	if err != nil || string(raw) != "newer" {
		t.Fatalf("destination clobbered: %s err=%v", raw, err)
	}
	trashSrc := filepath.Join(home, ".trash", id, "files", "archived_sessions", "a.jsonl")
	if _, err := os.Stat(trashSrc); err != nil {
		t.Fatalf("trash source lost: %v", err)
	}
	if result.OK {
		t.Fatal("restore reported success")
	}
}

func mustRoot(t *testing.T, home string) Root {
	t.Helper()
	root, err := OpenRoot(home)
	if err != nil {
		t.Fatal(err)
	}
	return root
}
