package managedfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCoordinatorAdoptsEmptyHomeAndRefusesForeignMetadata(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != OwnershipNone {
		t.Fatalf("empty=%s", got)
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != OwnershipCurrent {
		t.Fatalf("committed=%s", got)
	}

	foreign := t.TempDir()
	if err := os.WriteFile(filepath.Join(foreign, metadataName), []byte(`{"version":1,"generation":1,"state":"foreign","client":"other"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fc, err := New(foreign)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fc.Begin(context.Background(), "codex"); err != ErrForeignHome {
		t.Fatalf("foreign=%v", err)
	}
}

func TestCoordinatorRecoversStaleLockWithoutStealingALiveWriter(t *testing.T) {
	home := t.TempDir()
	lockPath := filepath.Join(home, lockName)
	if err := os.WriteFile(lockPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-3 * time.Minute)
	if err := os.Chtimes(lockPath, stale, stale); err != nil {
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
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestCoordinatorSerializesOverlappingWriters(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	first, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := coord.Begin(ctx, "codex"); err == nil {
		t.Fatal("overlapping writer was allowed")
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	second, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestEnableRestoresADisabledHomeForLaterWrites(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
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
	if err := coord.Enable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != OwnershipCurrent {
		t.Fatalf("enabled=%s", got)
	}
	again, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := again.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestEnableRefusesHomesThatWereNeverManaged(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := coord.Enable(context.Background()); !errors.Is(err, ErrIndeterminate) {
		t.Fatalf("enable empty=%v", err)
	}
}

func TestRecoverDropsInterruptedJournalAndAllowsANewWrite(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, journalName), []byte(`{"version":1,"generation":2,"state":"pending","client":"codex"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := coord.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, journalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("interrupted journal remained")
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != OwnershipCurrent {
		t.Fatalf("after recover=%s", got)
	}
}

func TestTxnStagesArtifactsBeforePublishingGeneration(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage("config.toml", []byte("model = \"gpt-5.6\"\n")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Stage("catalog.json", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "config.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("staged artifact published before commit")
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || string(cfg) != "model = \"gpt-5.6\"\n" {
		t.Fatalf("config=%q err=%v", cfg, err)
	}
	cat, err := os.ReadFile(filepath.Join(home, "catalog.json"))
	if err != nil || string(cat) != `{"ok":true}` {
		t.Fatalf("catalog=%q err=%v", cat, err)
	}
	rec, err := readRecord(filepath.Join(home, metadataName))
	if err != nil || rec.State != "committed" || rec.Generation != 1 {
		t.Fatalf("record=%#v err=%v", rec, err)
	}
}

func TestTxnRejectsUnsafeArtifactNames(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.Stage("../escape.toml", []byte("x")); err == nil {
		t.Fatal("path escape accepted")
	}
}

func TestDisableSurvivesAndBlocksLaterBegin(t *testing.T) {
	home := t.TempDir()
	coord, err := New(home)
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
	if got := coord.Classify(); got != OwnershipDisabled {
		t.Fatalf("disabled=%s", got)
	}
	if _, err := coord.Begin(context.Background(), "codex"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("begin after disable=%v", err)
	}
	again, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Classify(); got != OwnershipDisabled {
		t.Fatalf("restart=%s", got)
	}
}

func TestAdoptPromotesLegacyMetadataWithoutInventingNativeState(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, metadataName), []byte(`{"version":1,"generation":4,"state":"legacy","client":"codex"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	baseline := []byte("model = \"gpt-5.6\"\n")
	if err := os.WriteFile(filepath.Join(home, "config.toml"), baseline, 0o600); err != nil {
		t.Fatal(err)
	}
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != OwnershipLegacy {
		t.Fatalf("legacy=%s", got)
	}
	if err := coord.Adopt(context.Background(), "codex"); err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != OwnershipCurrent {
		t.Fatalf("adopted=%s", got)
	}
	got, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || string(got) != string(baseline) {
		t.Fatalf("baseline mutated: %q err=%v", got, err)
	}
	rec, err := readRecord(filepath.Join(home, metadataName))
	if err != nil || rec.State != "committed" || rec.Generation != 4 {
		t.Fatalf("record=%#v err=%v", rec, err)
	}
}

func TestAdoptRefusesUserOwnedAndIndeterminateHomes(t *testing.T) {
	userHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(userHome, "config.toml"), []byte("x=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	uc, err := New(userHome)
	if err != nil {
		t.Fatal(err)
	}
	if err := uc.Adopt(context.Background(), "codex"); !errors.Is(err, ErrForeignHome) && !errors.Is(err, ErrIndeterminate) {
		t.Fatalf("user home adopt=%v", err)
	}

	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, metadataName), []byte(`{"version":1,"state":"weird"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	bc, err := New(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := bc.Adopt(context.Background(), "codex"); !errors.Is(err, ErrIndeterminate) {
		t.Fatalf("indeterminate adopt=%v", err)
	}
}
