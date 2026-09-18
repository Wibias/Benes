package managedfs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStageRefusesSymlinkArtifact(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside.toml")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "config.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks not permitted")
	}
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.Stage("config.toml", []byte("owned")); !errors.Is(err, ErrSymlink) {
		t.Fatalf("err=%v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "secret" {
		t.Fatalf("outside file mutated: %q %v", got, err)
	}
}

func TestCommitRefusesWhenArtifactChangesAfterStage(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage("config.toml", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user-edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "user-edit" {
		t.Fatalf("user edit clobbered: %q %v", got, err)
	}
}

func TestCommitSkipsRewriteWhenBytesAlreadyMatch(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte("same"), 0o600); err != nil {
		t.Fatal(err)
	}
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage("config.toml", []byte("same")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, []byte("same")) {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if got := coord.Classify(); got != OwnershipCurrent {
		t.Fatalf("ownership=%s", got)
	}
}
