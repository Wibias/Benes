package credentials

import (
	"errors"
	"testing"
)

func TestDeleteUnreferencedRemovesOnlyOwnedUnusedSecret(t *testing.T) {
	store := NewOSStore(&memBackend{})
	keep, err := store.Put("keep", []byte("sk-keep"))
	if err != nil {
		t.Fatal(err)
	}
	gone, err := store.Put("gone", []byte("sk-gone"))
	if err != nil {
		t.Fatal(err)
	}
	live := []Ref{keep}
	if err := DeleteUnreferenced(store, gone, live); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(gone); !errors.Is(err, ErrCredentialUnavailable) {
		t.Fatalf("unreferenced secret remained: %v", err)
	}
	got, err := store.Get(keep)
	if err != nil || string(got) != "sk-keep" {
		t.Fatalf("live secret=%q err=%v", got, err)
	}
}

func TestDeleteUnreferencedRefusesStillReferencedSecret(t *testing.T) {
	store := NewOSStore(&memBackend{})
	ref, err := store.Put("live", []byte("sk-live"))
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteUnreferenced(store, ref, []Ref{ref}); !errors.Is(err, ErrStillReferenced) {
		t.Fatalf("err=%v", err)
	}
	got, err := store.Get(ref)
	if err != nil || string(got) != "sk-live" {
		t.Fatalf("deleted live secret=%q err=%v", got, err)
	}
}
