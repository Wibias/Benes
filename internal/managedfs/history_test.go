package managedfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCommitSnapshotsReplacedArtifactIntoHistory(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage("config.toml", []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage("config.toml", []byte("second\n")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(home, historyName, "1", "config.toml"))
	if err != nil || string(got) != "first\n" {
		t.Fatalf("history=%q err=%v", got, err)
	}
	current, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || string(current) != "second\n" {
		t.Fatalf("current=%q err=%v", current, err)
	}
}

func TestCommitDoesNotSnapshotIdenticalRewrite(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage("config.toml", []byte("same\n")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage("config.toml", []byte("same\n")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, historyName)); !os.IsNotExist(err) {
		t.Fatal("history written for identical bytes")
	}
}
