package usageledger

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAppendRefusesUsageSymlinkAndDoesNotCreateOutside(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, DirName)
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			if err := makeJunction(link, outside); err != nil {
				t.Skip(err)
			}
		} else {
			t.Skip(err)
		}
	}
	l := mustOpen(t, home, 1024)
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_1"}`)); err == nil {
		t.Fatal("followed usage symlink")
	}
	assertOutsideUnchanged(t, outside, sentinel, "keep")
}

func TestAppendRefusesSegmentsSymlinkAndDoesNotCreateOutside(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, DirName), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, DirName, SegmentsDir)
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			if err := makeJunction(link, outside); err != nil {
				t.Skip(err)
			}
		} else {
			t.Skip(err)
		}
	}
	l := mustOpen(t, home, 1024)
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_1"}`)); err == nil {
		t.Fatal("followed segments symlink")
	}
	assertOutsideUnchanged(t, outside, sentinel, "keep")
}

func TestAppendDoesNotFollowActiveSymlink(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "active.jsonl")
	if err := os.WriteFile(target, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := mustOpen(t, home, 1024)
	if err := os.MkdirAll(l.segmentsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, l.activePath()); err != nil {
		if runtime.GOOS == "windows" {
			if err := makeJunction(l.activePath(), outside); err != nil {
				t.Skip(err)
			}
		} else {
			t.Skip(err)
		}
	}
	if err := l.Append([]byte(`{"timestamp":1,"requestId":"req_1"}`)); err == nil {
		t.Fatal("followed active symlink")
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "outside\n" {
		t.Fatalf("external active mutated: %q err=%v", raw, err)
	}
}

func TestAppendDoesNotFollowSealedSymlink(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "00000001.jsonl")
	if err := os.WriteFile(target, []byte(`{"timestamp":1,"requestId":"req_out"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := mustOpen(t, home, 1024)
	if err := os.MkdirAll(l.segmentsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, l.segmentPath(1)); err != nil {
		if runtime.GOOS == "windows" {
			if err := makeJunction(l.segmentPath(1), outside); err != nil {
				t.Skip(err)
			}
		} else {
			t.Skip(err)
		}
	}
	if _, err := l.Status(); err == nil {
		t.Fatal("read followed sealed symlink")
	}
	if err := l.Append([]byte(`{"timestamp":2,"requestId":"req_new"}`)); err == nil {
		t.Fatal("write followed sealed symlink")
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != `{"timestamp":1,"requestId":"req_out"}`+"\n" {
		t.Fatalf("external sealed mutated: %q err=%v", raw, err)
	}
}

func TestSnapshotRefusesUsageReparseAndDoesNotReadOutside(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	plantOutsideRow(t, filepath.Join(outside, ActiveFile), "req_out")
	if err := linkOrJunction(t, filepath.Join(home, DirName), outside); err != nil {
		t.Skip(err)
	}
	assertReadDoesNotConsumeOutside(t, home, outside, "req_out")
}

func TestSnapshotRefusesSegmentsReparseAndDoesNotReadOutside(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	plantOutsideRow(t, filepath.Join(outside, formatSegmentName(1)), "req_out")
	if err := os.MkdirAll(filepath.Join(home, DirName), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := linkOrJunction(t, filepath.Join(home, DirName, SegmentsDir), outside); err != nil {
		t.Skip(err)
	}
	assertReadDoesNotConsumeOutside(t, home, outside, "req_out")
}

func TestSnapshotRefusesActiveReparseAndDoesNotReadOutside(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, ActiveFile)
	plantOutsideRow(t, target, "req_out")
	l := mustOpen(t, home, 1024)
	if err := os.MkdirAll(l.segmentsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := linkFileOrJunction(t, l.activePath(), target, outside); err != nil {
		t.Skip(err)
	}
	assertReadDoesNotConsumeOutside(t, home, outside, "req_out")
}

func TestSnapshotRefusesSealedReparseAndDoesNotReadOutside(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, formatSegmentName(1))
	plantOutsideRow(t, target, "req_out")
	l := mustOpen(t, home, 1024)
	if err := os.MkdirAll(l.segmentsPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := linkFileOrJunction(t, l.segmentPath(1), target, outside); err != nil {
		t.Skip(err)
	}
	assertReadDoesNotConsumeOutside(t, home, outside, "req_out")
}

func TestSnapshotMissingUsageDirIsEmptyNotError(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 1024)
	snap, err := l.SnapshotNewest(SnapshotLimits{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if snap.HistoryTruncated || len(snap.Lines) != 0 || snap.LogicalBytes != 0 {
		t.Fatalf("%+v", snap)
	}
	if err := l.Enumerate(func([]byte) error {
		t.Fatal("enumerated rows from an empty ledger")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func plantOutsideRow(t *testing.T, path, id string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(`{"timestamp":1,"requestId":"`+id+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertReadDoesNotConsumeOutside(t *testing.T, home, outside, leakedID string) {
	t.Helper()
	l := mustOpen(t, home, 1024)
	snap, snapErr := l.SnapshotNewest(SnapshotLimits{MaxBytes: 1 << 20})
	if snapErr == nil {
		for _, id := range idsFromLines(t, snap.Lines) {
			if id == leakedID {
				t.Fatalf("snapshot consumed outside row %s", leakedID)
			}
		}
		t.Fatal("expected snapshot to refuse reparse")
	}
	enumErr := l.Enumerate(func(line []byte) error {
		if strings.Contains(string(line), leakedID) {
			t.Fatalf("enumerate consumed outside row %s", leakedID)
		}
		return nil
	})
	if enumErr == nil {
		t.Fatal("expected enumerate to refuse reparse")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("outside directory mutated empty")
	}
}

func linkOrJunction(t *testing.T, link, target string) error {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return nil
	} else if runtime.GOOS == "windows" {
		return makeJunction(link, target)
	} else {
		return err
	}
}

func linkFileOrJunction(t *testing.T, link, fileTarget, dirTarget string) error {
	t.Helper()
	if err := os.Symlink(fileTarget, link); err == nil {
		return nil
	} else if runtime.GOOS == "windows" {
		return makeJunction(link, dirTarget)
	} else {
		return err
	}
}

func assertOutsideUnchanged(t *testing.T, outside, sentinel, want string) {
	t.Helper()
	raw, err := os.ReadFile(sentinel)
	if err != nil || string(raw) != want {
		t.Fatalf("outside sentinel mutated data=%q err=%v", raw, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(sentinel) {
		t.Fatalf("outside dir mutated: %+v", names(entries))
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func makeJunction(link, target string) error {
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	return cmd.Run()
}
