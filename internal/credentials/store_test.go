package credentials

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileStoreRoundTripAndNeverReturnsEmptyOnMiss(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "creds"), false)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put("slot-a", []byte("sk-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if ref.Source != SourceSecureStore || ref.ID != "slot-a" {
		t.Fatalf("ref=%#v", ref)
	}
	got, err := store.Get(ref)
	if err != nil || string(got) != "sk-secret" {
		t.Fatalf("get=%q %v", got, err)
	}
	if _, err := store.Get(Ref{ID: "missing", Source: SourceSecureStore}); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("missing=%v", err)
	}
}

func TestFileStoreEnvAndPlaintextFallbackRules(t *testing.T) {
	t.Setenv("BENES_TEST_KEY", "from-env")
	store, err := NewFileStore(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(Ref{Source: SourceEnv, Hint: "BENES_TEST_KEY"})
	if err != nil || string(got) != "from-env" {
		t.Fatalf("env=%q %v", got, err)
	}
	if _, err := store.Get(Ref{Source: SourcePlaintext, Hint: "sk-plain"}); !errors.Is(err, ErrPlaintextDisabled) {
		t.Fatalf("plaintext disabled=%v", err)
	}
	open, err := NewFileStore(t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	got, err = open.Get(Ref{Source: SourcePlaintext, Hint: "sk-plain"})
	if err != nil || string(got) != "sk-plain" {
		t.Fatalf("plaintext=%q %v", got, err)
	}
}

func TestFileStorePutThenDelete(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "creds")
	store, err := NewFileStore(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Put("gone", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ref); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gone.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted file remained: %v", err)
	}
}

func TestFileStoreListReturnsRefsWithoutSecrets(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "creds")
	store, err := NewFileStore(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put("slot-a", []byte("sk-secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put("slot-b", []byte("sk-other")); err != nil {
		t.Fatal(err)
	}
	refs, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("refs=%#v", refs)
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if ref.Source != SourceSecureStore || ref.Hint != "" {
			t.Fatalf("ref=%#v", ref)
		}
		seen[ref.ID] = true
	}
	if !seen["slot-a"] || !seen["slot-b"] {
		t.Fatalf("ids=%#v", refs)
	}
}
