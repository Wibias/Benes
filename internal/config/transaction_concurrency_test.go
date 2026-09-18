package config

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestConfigTransactionConcurrentStoresPreserveSiblingWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses"}}}`)

	storeA := NewTransactionStore(path, 1<<20)
	storeB := NewTransactionStore(path, 1<<20)
	txA, err := storeA.Begin()
	if err != nil {
		t.Fatalf("begin A: %v", err)
	}
	txB, err := storeB.Begin()
	if err != nil {
		t.Fatalf("begin B: %v", err)
	}
	if err := txA.Set(JSONPath("providers", "foo"), rawJSON(t, map[string]any{"adapter": "openai-chat"})); err != nil {
		t.Fatalf("set A: %v", err)
	}
	if err := txB.Set(JSONPath("providers", "bar"), rawJSON(t, map[string]any{"adapter": "anthropic"})); err != nil {
		t.Fatalf("set B: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := txA.Commit()
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := txB.Commit()
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent commit: %v", err)
		}
	}

	root := readConfigObject(t, path)
	providers := decodeRawObject(t, root["providers"])
	if providers["foo"] == nil || providers["bar"] == nil || providers["openai"] == nil {
		t.Fatalf("concurrent sibling write lost: %s", root["providers"])
	}
}

func TestConfigTransactionWholeMapReplaceConflictsWithConcurrentChildAdd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses"}}}`)

	store := NewTransactionStore(path, 1<<20)
	replace, err := store.Begin()
	if err != nil {
		t.Fatalf("begin replace: %v", err)
	}
	child, err := store.Begin()
	if err != nil {
		t.Fatalf("begin child: %v", err)
	}
	if err := replace.Set(JSONPath("providers"), rawJSON(t, map[string]any{})); err != nil {
		t.Fatalf("queue replace: %v", err)
	}
	if err := child.Set(JSONPath("providers", "extra"), rawJSON(t, map[string]any{"adapter": "anthropic"})); err != nil {
		t.Fatalf("queue child: %v", err)
	}
	if _, err := child.Commit(); err != nil {
		t.Fatalf("commit child: %v", err)
	}
	if _, err := replace.Commit(); !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("expected whole-map conflict, got %v", err)
	}
}

func TestConfigTransactionMissingFileStartsFromFactoryDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store := NewTransactionStore(path, 1<<20)
	tx, err := store.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tx.Set(JSONPath("corsAllowOrigins"), rawJSON(t, []string{"https://example.com"})); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	root := readConfigObject(t, path)
	providers := decodeRawObject(t, root["providers"])
	if providers["openai"] == nil {
		t.Fatal("factory default provider was lost")
	}
	var defaultProvider string
	if err := json.Unmarshal(root["defaultProvider"], &defaultProvider); err != nil || defaultProvider != "openai" {
		t.Fatalf("factory default provider selector lost: %q (%v)", defaultProvider, err)
	}
}

func TestConfigTransactionRejectsDirectManagedMetadataMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses"}}}`)
	store := NewTransactionStore(path, 1<<20)
	tx, err := store.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tx.Set(JSONPath(managedMetadataKey), rawJSON(t, map[string]string{"/x": "spoof"})); !errors.Is(err, ErrConfigReservedPath) {
		t.Fatalf("expected reserved metadata rejection, got %v", err)
	}
}
