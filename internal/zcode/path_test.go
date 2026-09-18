package zcode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPathUsesAbsoluteDataDirOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", dir)
	got, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "v2", "config.json")
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestConfigPathRejectsRelativeDataDir(t *testing.T) {
	t.Setenv("ZCODE_DATA_DIR", "relative/zcode")
	if _, err := ConfigPath(); err == nil {
		t.Fatal("relative override accepted")
	}
}

func TestConfigPathDefaultsUnderUserHome(t *testing.T) {
	t.Setenv("ZCODE_DATA_DIR", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".zcode", "v2", "config.json")
	if got != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}
