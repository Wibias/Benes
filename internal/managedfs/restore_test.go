package managedfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreReplacesCurrentArtifactFromHistory(t *testing.T) {
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
	if err := coord.Restore(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || string(got) != "first\n" {
		t.Fatalf("restored=%q err=%v", got, err)
	}
	if got := coord.Classify(); got != OwnershipCurrent {
		t.Fatalf("ownership=%s", got)
	}
}

func TestRestoreAcceptsADriftedHome(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != OwnershipDrifted {
		t.Fatalf("pre-restore=%s", got)
	}
	if err := coord.Restore(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || string(got) != "first\n" {
		t.Fatalf("restored=%q err=%v", got, err)
	}
	if got := coord.Classify(); got != OwnershipCurrent {
		t.Fatalf("ownership=%s", got)
	}
}

func TestRestoreRefusesMissingGeneration(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := coord.Restore(context.Background(), 1); err == nil {
		t.Fatal("missing history accepted")
	}
}
