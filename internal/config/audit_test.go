package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigTransactionRecordsRedactedMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{},"port":18080}`)
	store := NewTransactionStore(path, 1<<20)
	tx, err := store.Begin()
	if err != nil {
		t.Fatal(err)
	}
	tx.SetSource(MutationSource{Class: SourceCLI, Detail: "config set"})
	if err := tx.Set(JSONPath("providers", "openai-apikey"), rawJSON(t, map[string]any{
		"adapter": "openai-responses",
		"apiKey":  "sk-live-secret-value",
		"note":    "ok",
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	records, err := ListMutations(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records=%d", len(records))
	}
	if records[0].Source.Class != SourceCLI || records[0].Timestamp == "" || records[0].Revision == "" {
		t.Fatalf("record=%#v", records[0])
	}
	if len(records[0].Changes) != 1 || records[0].Changes[0].Path != "/providers/openai-apikey" {
		t.Fatalf("changes=%#v", records[0].Changes)
	}
	persisted, err := os.ReadFile(MutationLogPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(persisted), "sk-live-secret-value") || strings.Contains(string(persisted), "sk-live") {
		t.Fatalf("secret persisted: %s", persisted)
	}
	var after map[string]any
	if err := json.Unmarshal(records[0].Changes[0].After, &after); err != nil {
		t.Fatal(err)
	}
	if after["apiKey"] != "[redacted]" || after["note"] != "ok" {
		t.Fatalf("after=%#v", after)
	}
}

func TestConfigTransactionNoopDoesNotRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{},"port":18080}`)
	store := NewTransactionStore(path, 1<<20)
	tx, err := store.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Set(JSONPath("port"), rawJSON(t, 18080)); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	records, err := ListMutations(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("noop recorded %#v", records)
	}
	if _, err := os.Stat(MutationLogPath(path)); !os.IsNotExist(err) {
		t.Fatalf("log exists: %v", err)
	}
}

func TestFailedConfigWriteDoesNotRecordMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeTransactionConfig(t, path, `{"providers":{}}`)
	store := NewTransactionStore(path, 1<<20)
	tx, err := store.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Set(JSONPath("port"), rawJSON(t, 1)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Commit(); err == nil {
		t.Fatal("commit succeeded against a directory")
	}
	if _, err := os.Stat(MutationLogPath(path)); !os.IsNotExist(err) {
		t.Fatalf("failed write recorded a mutation: %v", err)
	}
}

func TestRedactAuditValueCatchesAuthorizationAndJWT(t *testing.T) {
	raw := rawJSON(t, map[string]any{
		"authorization": "Bearer abc",
		"nested":        map[string]any{"refreshToken": "tok", "plain": "keep"},
		"blob":          "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0In0.sig",
	})
	got := redactAuditValue(raw)
	if strings.Contains(string(got), "Bearer abc") || strings.Contains(string(got), "tok") || strings.Contains(string(got), "eyJhbGci") {
		t.Fatalf("under-redacted: %s", got)
	}
	if !strings.Contains(string(got), `"plain":"keep"`) {
		t.Fatalf("over-redacted: %s", got)
	}
}
