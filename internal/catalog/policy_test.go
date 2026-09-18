package catalog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestPolicyFromRootReadsCatalogAuthorities(t *testing.T) {
	policy := PolicyFromRoot(json.RawMessage(`{
		"retainModels":{"openai":{"gpt-5.6":true}},
		"modelContextWindows":{"openai":{"gpt-5.6":{"tokens":200000,"mode":"override"}}},
		"autoCompactTokenLimits":{"openai":{"gpt-5.6":1000}}
	}`))
	if !policy.RetainModels["openai"]["gpt-5.6"] || policy.ModelContextWindows["openai"]["gpt-5.6"].Tokens != 200000 {
		t.Fatalf("policy=%#v", policy)
	}
	if policy.AutoCompactTokenLimits["openai"]["gpt-5.6"] != 1000 {
		t.Fatalf("compact=%#v", policy.AutoCompactTokenLimits)
	}
}

func TestPolicyStoreConcurrentPartialUpdatesPreserveSiblingsAndClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"providers":{"openai":{"adapter":"openai-responses"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	txs := config.NewTransactionStore(path, 1<<20)
	store := PolicyStore{Transactions: txs, NativeOpenAIModelIDs: map[string]struct{}{"gpt-5.6": {}, "gpt-5.5": {}}}

	a, err := store.Begin()
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetContextWindow("openai", "gpt-5.6", 1000000, WindowOverride); err != nil {
		t.Fatal(err)
	}
	if err := b.SetContextWindow("openai", "gpt-5.5", 400000, WindowFallback); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Commit(); err != nil {
		t.Fatal(err)
	}

	clear, err := store.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := clear.ClearContextWindow("openai", "gpt-5.5"); err != nil {
		t.Fatal(err)
	}
	if _, err := clear.Commit(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	var byProvider map[string]map[string]ContextPolicyValue
	if err := json.Unmarshal(root["modelContextWindows"], &byProvider); err != nil {
		t.Fatal(err)
	}
	if byProvider["openai"]["gpt-5.6"].Tokens != 1000000 {
		t.Fatalf("sibling lost: %s", data)
	}
	if _, exists := byProvider["openai"]["gpt-5.5"]; exists {
		t.Fatalf("clear tombstone failed: %s", data)
	}
}

func TestPolicyStoreRejectsUnknownOrQualifiedNativeOpenAIModelKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"providers":{"openai":{"adapter":"openai-responses"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := PolicyStore{Transactions: config.NewTransactionStore(path, 1<<20), NativeOpenAIModelIDs: map[string]struct{}{"gpt-5.6": {}}}
	tx, err := store.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.SetAutoCompactLimit("openai", "account/gpt-5.6", 50000); !errors.Is(err, ErrInvalidNativeModelID) {
		t.Fatalf("qualified id accepted: %v", err)
	}
	if err := tx.SetAutoCompactLimit("openai", "prototype-model", 50000); !errors.Is(err, ErrInvalidNativeModelID) {
		t.Fatalf("unknown id accepted: %v", err)
	}
	if err := tx.SetAutoCompactLimit("openai", "gpt-5.6", 50000); err != nil {
		t.Fatalf("known exact id rejected: %v", err)
	}
}
