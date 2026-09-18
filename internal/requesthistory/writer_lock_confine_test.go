package requesthistory

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAcquireWriterRefusesLockLeafSymlink(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	external := filepath.Join(outside, "external.lock")
	if err := os.WriteFile(external, []byte("keep-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(external)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, writerLockName)
	if err := os.Symlink(external, link); err != nil {
		if runtime.GOOS == "windows" {
			if err := makeWriterJunction(link, outside); err != nil {
				t.Skip(err)
			}
		} else {
			t.Skip(err)
		}
	}
	lock, err := acquireWriter(home)
	if err == nil {
		lock.Close()
		t.Fatal("acquireWriter followed lock leaf reparse")
	}
	after, err := os.Stat(external)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() != before.Size() || after.ModTime() != before.ModTime() {
		t.Fatalf("external lock target mutated")
	}
	raw, err := os.ReadFile(external)
	if err != nil || string(raw) != "keep-me" {
		t.Fatalf("external content mutated %q err=%v", raw, err)
	}
}

func TestAcquireWriterRefusesAncestorReparse(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "home")
	if err := os.Symlink(outside, nested); err != nil {
		if runtime.GOOS == "windows" {
			if err := makeWriterJunction(nested, outside); err != nil {
				t.Skip(err)
			}
		} else {
			t.Skip(err)
		}
	}
	lock, err := acquireWriter(nested)
	if err == nil {
		lock.Close()
		t.Fatal("acquireWriter followed ancestor reparse")
	}
	raw, err := os.ReadFile(sentinel)
	if err != nil || string(raw) != "keep" {
		t.Fatalf("external mutated %q err=%v", raw, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "keep.txt" {
		t.Fatalf("outside dir mutated: %+v", entries)
	}
}

func makeWriterJunction(link, target string) error {
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	return cmd.Run()
}
