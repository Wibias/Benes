package managedfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyReportsDriftAfterUserEditsCommittedArtifact(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage("config.toml", []byte("owned\n")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != OwnershipCurrent {
		t.Fatalf("before edit: %s", got)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != OwnershipDrifted {
		t.Fatalf("after edit: %s", got)
	}
}

func TestBeginRefusesDriftedHome(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage("config.toml", []byte("owned\n")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := coord.Begin(context.Background(), "codex"); err == nil {
		t.Fatal("drifted home accepted a new write")
	}
}

func TestBeginReassertAllowsDriftedHomeThenClearsOnCommit(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := coord.Begin(context.Background(), "zcode")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage("config.json", []byte("owned\n")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("peer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != OwnershipDrifted {
		t.Fatalf("after peer edit: %s", got)
	}
	tx, err = coord.BeginReassert(context.Background(), "zcode")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage("config.json", []byte("reasserted\n")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != OwnershipCurrent {
		t.Fatalf("after reassert: %s", got)
	}
	got, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "reasserted\n" {
		t.Fatalf("artifact=%q", got)
	}
}

func TestMissingCommittedArtifactIsDrift(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage("config.toml", []byte("owned\n")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(home, "config.toml")); err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != OwnershipDrifted {
		t.Fatalf("missing artifact: %s", got)
	}
}
