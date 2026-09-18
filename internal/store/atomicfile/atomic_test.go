package atomicfile

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteReplacesFileWithoutPartialContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new content"), Options{Mode: 0o600}); err != nil {
		t.Fatalf("Write(): %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new content" {
		t.Fatalf("content=%q", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode=%#o", info.Mode().Perm())
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".benes.") && strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

func TestWritePreservesResolvableSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on some Windows runners")
	}
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(realDir, "config.json")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "config.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := Write(link, []byte("new"), Options{Mode: 0o600}); err != nil {
		t.Fatalf("Write(): %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("write replaced the symlink instead of its resolved target")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Fatalf("target=%q", got)
	}
}

func TestWriteRejectsDanglingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on some Windows runners")
	}
	root := t.TempDir()
	link := filepath.Join(root, "config.json")
	if err := os.Symlink(filepath.Join(root, "missing"), link); err != nil {
		t.Fatal(err)
	}
	err := Write(link, []byte("secret"), Options{Mode: 0o600})
	if err == nil {
		t.Fatal("Write() accepted dangling symlink")
	}
	info, statErr := os.Lstat(link)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("dangling symlink was replaced")
	}
}

func TestReadBoundedRejectsOversizedFileWithoutReturningPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("123456"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := ReadBounded(path, 5)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err=%v want ErrTooLarge", err)
	}
	if data != nil {
		t.Fatalf("data=%q want nil", data)
	}
}

func TestReadBoundedReadsExactLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := ReadBounded(path, 5)
	if err != nil {
		t.Fatalf("ReadBounded(): %v", err)
	}
	if !bytes.Equal(data, []byte("12345")) {
		t.Fatalf("data=%q", data)
	}
}

func TestWriteFailureScrubsTemporarySecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	sentinel := errors.New("publish failed")
	err := writeWithOps(path, []byte("TOP-SECRET"), Options{Mode: 0o600}, ops{
		openTemp:     os.OpenFile,
		rename:       func(string, string) error { return sentinel },
		remove:       func(string) error { return errors.New("locked") },
		openDir:      os.Open,
		lstat:        os.Lstat,
		evalSymlinks: filepath.EvalSymlinks,
	})
	var residual *ResidualError
	if !errors.As(err, &residual) {
		t.Fatalf("err=%T %v, want ResidualError", err, err)
	}
	content, readErr := os.ReadFile(residual.TempPath)
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		t.Fatal(readErr)
	}
	if bytes.Contains(content, []byte("TOP-SECRET")) {
		t.Fatalf("secret remained in residual temp: %q", content)
	}
}

func TestWriteHardenFailureScrubsBeforePublish(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	err := Write(path, []byte("SECRET"), Options{
		Mode:   0o600,
		Harden: func(string) error { return errors.New("acl failed") },
	})
	if err == nil || !strings.Contains(err.Error(), "harden") {
		t.Fatalf("err=%v, want harden failure", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("destination exists after harden failure: %v", statErr)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".benes.") {
			content, _ := os.ReadFile(filepath.Join(dir, entry.Name()))
			if bytes.Contains(content, []byte("SECRET")) {
				t.Fatalf("secret remained after harden failure: %q", content)
			}
		}
	}
}

func TestWriteReportsPublishedDurabilityFailureSeparately(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory fsync is not used on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	sentinel := errors.New("directory unavailable")
	err := writeWithOps(path, []byte("committed"), Options{Mode: 0o600}, ops{
		openTemp:     os.OpenFile,
		rename:       os.Rename,
		remove:       os.Remove,
		openDir:      func(string) (*os.File, error) { return nil, sentinel },
		lstat:        os.Lstat,
		evalSymlinks: filepath.EvalSymlinks,
	})
	var durability *DurabilityError
	if !errors.As(err, &durability) {
		t.Fatalf("err=%T %v, want DurabilityError", err, err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "committed" {
		t.Fatalf("published content=%q", got)
	}
}
