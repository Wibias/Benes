package servicectl

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/managedfs"
)

func TestPublishUnitWritesThroughTheCoordinator(t *testing.T) {
	dir := t.TempDir()
	body := []byte("[Unit]\nDescription=Benes\n")
	if err := PublishUnit(context.Background(), dir, "benes.service", body); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "benes.service"))
	if err != nil || string(got) != string(body) {
		t.Fatalf("unit=%q err=%v", got, err)
	}
	coord, err := managedfs.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != managedfs.OwnershipCurrent {
		t.Fatalf("classify=%s", got)
	}
}

func TestPublishUnitRefusesASymlinkArtifact(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(target, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "benes.service")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink not available")
	}
	if err := PublishUnit(context.Background(), dir, "benes.service", []byte("[Unit]\n")); err == nil {
		t.Fatal("expected symlink refusal")
	}
}

func TestPublishUnitIsNoopWhenBytesMatch(t *testing.T) {
	dir := t.TempDir()
	body := []byte("[Unit]\nDescription=Benes\n")
	if err := PublishUnit(context.Background(), dir, "benes.service", body); err != nil {
		t.Fatal(err)
	}
	coord, err := managedfs.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := coord.Classify()
	if err := PublishUnit(context.Background(), dir, "benes.service", body); err != nil {
		t.Fatal(err)
	}
	if coord.Classify() != first {
		t.Fatalf("classify changed on identical rewrite")
	}
}
