package requesthistory

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/usageledger"
)

func TestRebuildReplaceFailureKeepsRecoverySidecar(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_old","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(home, dbName)
	before, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	journal := dest + "-journal"
	payload := []byte("recovery-sidecar-fixture")
	if err := os.WriteFile(journal, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	SetReplaceTestHook(func(tmp, dest string) error {
		return errors.New("forced replace failure")
	})
	defer SetReplaceTestHook(nil)
	if _, err := Rebuild(home); err == nil {
		t.Fatal("expected forced replace failure")
	}
	got, err := os.ReadFile(journal)
	if err != nil {
		t.Fatalf("recovery sidecar irreversibly lost: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("sidecar mutated %q", got)
	}
	after, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed install replaced destination DB")
	}
	if err := os.Remove(journal); err != nil {
		t.Fatal(err)
	}
	if _, err := lookupOnly(home, "req_old"); err != nil {
		t.Fatalf("old generation not recoverable: %v", err)
	}
	SetReplaceTestHook(nil)
	if _, err := Rebuild(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Fatalf("successful rebuild left stale journal: %v", err)
	}
}
