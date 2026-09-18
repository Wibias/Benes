package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/managedfs"
)

func TestClientApplyRefusesCodexHomeMismatch(t *testing.T) {
	home := t.TempDir()
	other := t.TempDir()
	src := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(src, []byte("x=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", other)
	var stderr bytes.Buffer
	code := run(context.Background(), []string{"client", "apply", "--home", home, "--client", "codex", "--artifact", "config.toml", "--from", src}, io.Discard, &stderr)
	if code == 0 {
		t.Fatal("mismatch accepted")
	}
	if !strings.Contains(stderr.String(), "disagree") && !strings.Contains(stderr.String(), "home") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestClientStatusPrintsOwnership(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(src, []byte("x=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), []string{"client", "apply", "--home", home, "--client", "codex", "--artifact", "config.toml", "--from", src}, io.Discard, io.Discard); code != 0 {
		t.Fatal("apply")
	}
	var stdout bytes.Buffer
	if code := run(context.Background(), []string{"client", "status", "--home", home}, &stdout, io.Discard); code != 0 {
		t.Fatalf("status exit=%d", code)
	}
	if got := stdout.String(); got != string(managedfs.OwnershipCurrent)+"\n" {
		t.Fatalf("status=%q", got)
	}
}

func TestClientAdoptPromotesLegacyMetadata(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "benes-managed.json"), []byte(`{"version":1,"generation":4,"state":"legacy","client":"codex"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), []string{"client", "adopt", "--home", home, "--client", "codex"}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("adopt exit=%d", code)
	}
	coord, err := managedfs.New(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != managedfs.OwnershipCurrent {
		t.Fatalf("adopted=%s", got)
	}
}

func TestClientRecoverDropsAnInterruptedJournal(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "benes-managed.journal"), []byte(`{"version":1,"generation":2,"state":"pending","client":"codex"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), []string{"client", "recover", "--home", home}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("recover exit=%d", code)
	}
	if _, err := os.Stat(filepath.Join(home, "benes-managed.journal")); !os.IsNotExist(err) {
		t.Fatal("journal remained")
	}
}

func TestClientApplyPublishesThroughTheCoordinator(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(src, []byte("model = \"gpt-5.6\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"client", "apply", "--home", home, "--client", "codex", "--artifact", "config.toml", "--from", src}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	coord, err := managedfs.New(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != managedfs.OwnershipCurrent {
		t.Fatalf("classify=%s", got)
	}
	got, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || string(got) != "model = \"gpt-5.6\"\n" {
		t.Fatalf("published=%q err=%v", got, err)
	}
}

func TestClientDisableAndEnableRoundTrip(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(src, []byte("model = \"gpt-5.6\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), []string{"client", "apply", "--home", home, "--client", "codex", "--artifact", "config.toml", "--from", src}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("apply exit=%d", code)
	}
	var stderr bytes.Buffer
	if code := run(context.Background(), []string{"client", "disable", "--home", home}, io.Discard, &stderr); code != 0 {
		t.Fatalf("disable exit=%d stderr=%s", code, stderr.String())
	}
	coord, err := managedfs.New(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != managedfs.OwnershipDisabled {
		t.Fatalf("disabled=%s", got)
	}
	if code := run(context.Background(), []string{"client", "enable", "--home", home}, io.Discard, io.Discard); code != 0 {
		t.Fatal("enable failed")
	}
	if got := coord.Classify(); got != managedfs.OwnershipCurrent {
		t.Fatalf("enabled=%s", got)
	}
}

func TestClientApplyRefusesADisabledHome(t *testing.T) {
	home := t.TempDir()
	coord, err := managedfs.New(home)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := coord.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(src, []byte("x=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"client", "apply", "--home", home, "--client", "codex", "--artifact", "config.toml", "--from", src}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("disabled home accepted")
	}
}

func TestClientRestoreReplaysHistoryGeneration(t *testing.T) {
	home := t.TempDir()
	first := filepath.Join(t.TempDir(), "first.toml")
	second := filepath.Join(t.TempDir(), "second.toml")
	if err := os.WriteFile(first, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), []string{"client", "apply", "--home", home, "--client", "codex", "--artifact", "config.toml", "--from", first}, io.Discard, io.Discard); code != 0 {
		t.Fatal("first apply")
	}
	if code := run(context.Background(), []string{"client", "apply", "--home", home, "--client", "codex", "--artifact", "config.toml", "--from", second}, io.Discard, io.Discard); code != 0 {
		t.Fatal("second apply")
	}
	if code := run(context.Background(), []string{"client", "restore", "--home", home, "--generation", "1"}, io.Discard, io.Discard); code != 0 {
		t.Fatal("restore")
	}
	got, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || string(got) != "first\n" {
		t.Fatalf("restored=%q err=%v", got, err)
	}
}
