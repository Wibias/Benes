package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeTransactionConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func readConfigObject(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return root
}

func rawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test value: %v", err)
	}
	return data
}

func decodeRawObject(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode object: %v", err)
	}
	return value
}

func decodeRawStrings(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var value []string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode strings: %v", err)
	}
	return value
}

func TestConfigTransactionExplicitDeleteSurvivesStaleBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses"}},"featureA":true,"featureC":"old"}`)

	store := NewTransactionStore(path, 1<<20)
	tx, err := store.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tx.Delete(JSONPath("featureA")); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	root := readConfigObject(t, path)
	if _, exists := root["featureA"]; exists {
		t.Fatal("explicitly deleted field was resurrected")
	}
}

func TestConfigTransactionPreservesFieldAddedAfterBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses"}},"featureC":"old"}`)

	store := NewTransactionStore(path, 1<<20)
	stale, err := store.Begin()
	if err != nil {
		t.Fatalf("begin stale: %v", err)
	}

	newer, err := store.Begin()
	if err != nil {
		t.Fatalf("begin newer: %v", err)
	}
	if err := newer.Set(JSONPath("featureB"), rawJSON(t, "new")); err != nil {
		t.Fatalf("set B: %v", err)
	}
	if _, err := newer.Commit(); err != nil {
		t.Fatalf("commit newer: %v", err)
	}

	if err := stale.Set(JSONPath("featureC"), rawJSON(t, "changed")); err != nil {
		t.Fatalf("set C: %v", err)
	}
	if _, err := stale.Commit(); err != nil {
		t.Fatalf("commit stale unrelated mutation: %v", err)
	}

	root := readConfigObject(t, path)
	var b, c string
	if err := json.Unmarshal(root["featureB"], &b); err != nil || b != "new" {
		t.Fatalf("new field lost: %s (%v)", root["featureB"], err)
	}
	if err := json.Unmarshal(root["featureC"], &c); err != nil || c != "changed" {
		t.Fatalf("stale writer mutation missing: %s (%v)", root["featureC"], err)
	}
}

func TestConfigTransactionConflictsOnSameLogicalField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses"}},"mode":"a"}`)

	store := NewTransactionStore(path, 1<<20)
	first, err := store.Begin()
	if err != nil {
		t.Fatalf("begin first: %v", err)
	}
	second, err := store.Begin()
	if err != nil {
		t.Fatalf("begin second: %v", err)
	}

	if err := first.Set(JSONPath("mode"), rawJSON(t, "b")); err != nil {
		t.Fatalf("set first: %v", err)
	}
	if _, err := first.Commit(); err != nil {
		t.Fatalf("commit first: %v", err)
	}
	if err := second.Set(JSONPath("mode"), rawJSON(t, "c")); err != nil {
		t.Fatalf("set second: %v", err)
	}
	if _, err := second.Commit(); !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestConfigTransactionMergesConcurrentSiblingMapEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses"}}}`)

	store := NewTransactionStore(path, 1<<20)
	a, err := store.Begin()
	if err != nil {
		t.Fatalf("begin a: %v", err)
	}
	b, err := store.Begin()
	if err != nil {
		t.Fatalf("begin b: %v", err)
	}
	if err := a.Set(JSONPath("providers", "foo"), rawJSON(t, map[string]any{"adapter": "openai-chat"})); err != nil {
		t.Fatalf("set foo: %v", err)
	}
	if err := b.Set(JSONPath("providers", "bar"), rawJSON(t, map[string]any{"adapter": "anthropic"})); err != nil {
		t.Fatalf("set bar: %v", err)
	}
	if _, err := a.Commit(); err != nil {
		t.Fatalf("commit a: %v", err)
	}
	if _, err := b.Commit(); err != nil {
		t.Fatalf("commit b: %v", err)
	}

	root := readConfigObject(t, path)
	providers := decodeRawObject(t, root["providers"])
	if providers["foo"] == nil || providers["bar"] == nil || providers["openai"] == nil {
		t.Fatalf("sibling merge lost provider: %s", root["providers"])
	}
}

func TestConfigTransactionExplicitEmptyCollectionIsAValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses"}},"corsAllowOrigins":["https://example.com"]}`)

	store := NewTransactionStore(path, 1<<20)
	tx, err := store.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tx.Set(JSONPath("corsAllowOrigins"), rawJSON(t, []string{})); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	root := readConfigObject(t, path)
	if got := decodeRawStrings(t, root["corsAllowOrigins"]); got == nil || len(got) != 0 {
		t.Fatalf("expected explicit empty list, got %#v", got)
	}
}

func TestConfigTransactionManualDiskEditUsesSameConflictRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses"}},"mode":"a","note":"old"}`)

	store := NewTransactionStore(path, 1<<20)
	tx, err := store.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses"}},"mode":"manual","note":"old","future":7}`)
	if err := tx.Set(JSONPath("mode"), rawJSON(t, "benes")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := tx.Commit(); !errors.Is(err, ErrConfigConflict) {
		t.Fatalf("expected manual same-field conflict, got %v", err)
	}

	unrelated, err := store.Begin()
	if err != nil {
		t.Fatalf("begin unrelated: %v", err)
	}
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses"}},"mode":"manual","note":"old","future":8}`)
	if err := unrelated.Set(JSONPath("note"), rawJSON(t, "benes")); err != nil {
		t.Fatalf("set note: %v", err)
	}
	if _, err := unrelated.Commit(); err != nil {
		t.Fatalf("unrelated manual edit should merge: %v", err)
	}
	root := readConfigObject(t, path)
	var future int
	if err := json.Unmarshal(root["future"], &future); err != nil || future != 8 {
		t.Fatalf("manual future field lost: %s (%v)", root["future"], err)
	}
}

func TestConfigTransactionPreservesUnknownFutureFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses","futureProviderFlag":{"v":2}}},"futureRoot":{"nested":true}}`)

	store := NewTransactionStore(path, 1<<20)
	tx, err := store.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tx.Set(JSONPath("defaultProvider"), rawJSON(t, "openai")); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	root := readConfigObject(t, path)
	if root["futureRoot"] == nil {
		t.Fatal("unknown root field was lost")
	}
	providers := decodeRawObject(t, root["providers"])
	openai := decodeRawObject(t, providers["openai"])
	if openai["futureProviderFlag"] == nil {
		t.Fatal("unknown nested field was lost")
	}
}

func TestConfigTransactionManagedDeleteRequiresExactOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses"}},"codex":{"managed":{"value":1},"managed-user":{"value":2}}}`)

	store := NewTransactionStore(path, 1<<20)
	seed, err := store.Begin()
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	if err := seed.SetOwned(JSONPath("codex", "managed"), rawJSON(t, map[string]any{"value": 1}), "codex-auth"); err != nil {
		t.Fatalf("set owned: %v", err)
	}
	if _, err := seed.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}

	wrong, err := store.Begin()
	if err != nil {
		t.Fatalf("begin wrong: %v", err)
	}
	if err := wrong.DeleteOwned(JSONPath("codex", "managed"), "other-owner"); err != nil {
		t.Fatalf("queue wrong delete: %v", err)
	}
	if _, err := wrong.Commit(); !errors.Is(err, ErrConfigOwnership) {
		t.Fatalf("expected ownership error, got %v", err)
	}

	owned, err := store.Begin()
	if err != nil {
		t.Fatalf("begin owned: %v", err)
	}
	if err := owned.DeleteOwned(JSONPath("codex", "managed"), "codex-auth"); err != nil {
		t.Fatalf("queue owned delete: %v", err)
	}
	if _, err := owned.Commit(); err != nil {
		t.Fatalf("commit owned delete: %v", err)
	}
	root := readConfigObject(t, path)
	codex := decodeRawObject(t, root["codex"])
	if _, exists := codex["managed"]; exists {
		t.Fatal("owned fragment still exists")
	}
	if codex["managed-user"] == nil {
		t.Fatal("similarly named user field was deleted")
	}
}

func TestConfigTransactionValidationFailureDoesNotPublish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{"providers":{"openai":{"adapter":"openai-responses"}},"corsAllowOrigins":["https://example.com"]}`
	writeTransactionConfig(t, path, original)

	store := NewTransactionStore(path, 1<<20)
	tx, err := store.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tx.Set(JSONPath("corsAllowOrigins"), rawJSON(t, []string{""})); err != nil {
		t.Fatalf("set invalid value: %v", err)
	}
	if _, err := tx.Commit(); err == nil {
		t.Fatal("expected validation failure")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != original {
		t.Fatalf("validation failure published data: %s", data)
	}
}

func TestConfigTransactionRevisionAdvancesOnlyAfterCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{"openai":{"adapter":"openai-responses"}}}`)

	store := NewTransactionStore(path, 1<<20)
	tx, err := store.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	before := tx.BaselineRevision()
	if before == "" {
		t.Fatal("missing baseline revision")
	}
	if err := tx.Set(JSONPath("defaultProvider"), rawJSON(t, "openai")); err != nil {
		t.Fatalf("set: %v", err)
	}
	after, err := tx.Commit()
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if after == before || after == "" {
		t.Fatalf("revision did not advance: before=%q after=%q", before, after)
	}

	next, err := store.Begin()
	if err != nil {
		t.Fatalf("begin next: %v", err)
	}
	if next.BaselineRevision() != after {
		t.Fatalf("durable revision mismatch: got %q want %q", next.BaselineRevision(), after)
	}
}
