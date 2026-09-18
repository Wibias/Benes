package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScrubPersistedSecretsDropsPlaintextWhenRefPresent(t *testing.T) {
	root := map[string]json.RawMessage{
		"providers": json.RawMessage(`{
			"openai-apikey":{"adapter":"openai-chat","baseUrl":"https://api.openai.com/v1","apiKey":"sk-live","credentialRef":{"id":"openai-apikey","source":"secure-store"}}
		}`),
	}
	if err := scrubPersistedSecrets(root); err != nil {
		t.Fatal(err)
	}
	raw := string(root["providers"])
	if strings.Contains(raw, "sk-live") || strings.Contains(raw, `"apiKey"`) {
		t.Fatalf("plaintext remained: %s", raw)
	}
	if !strings.Contains(raw, `"credentialRef"`) {
		t.Fatalf("ref dropped: %s", raw)
	}
}

func TestScrubPersistedSecretsLeavesPlaintextWithoutRef(t *testing.T) {
	root := map[string]json.RawMessage{
		"providers": json.RawMessage(`{"chat":{"adapter":"openai-chat","baseUrl":"https://example.com/v1","apiKey":"sk-keep"}}`),
	}
	if err := scrubPersistedSecrets(root); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(root["providers"]), "sk-keep") {
		t.Fatal("explicit plaintext fallback was removed")
	}
}

func TestConfigTransactionScrubsProviderKeyAfterRefPublish(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeTransactionConfig(t, path, `{"providers":{"openai-apikey":{"adapter":"openai-chat","baseUrl":"https://api.openai.com/v1","apiKey":"sk-live"}}}`)
	store := NewTransactionStore(path, 1<<20)
	tx, err := store.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Set(JSONPath("providers", "openai-apikey", "credentialRef"), rawJSON(t, map[string]string{"id": "openai-apikey", "source": "secure-store"})); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-live") {
		t.Fatalf("committed config leaked key: %s", data)
	}
	if !strings.Contains(string(data), `"credentialRef"`) {
		t.Fatalf("ref missing: %s", data)
	}
}
