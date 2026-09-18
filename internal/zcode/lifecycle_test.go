package zcode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/managedfs"
)

func TestRemoveProviderDeletesOnlyBenes(t *testing.T) {
	existing := []byte(`{"login":{"vendor":"zai"},"provider":{"zai":{"kind":"openai"},"benes":{"kind":"anthropic"}}}`)
	got, err := RemoveProvider(existing)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	login, _ := doc["login"].(map[string]any)
	if login["vendor"] != "zai" {
		t.Fatalf("login=%#v", login)
	}
	providers, _ := doc["provider"].(map[string]any)
	if _, ok := providers["benes"]; ok {
		t.Fatal("owned fragment survived")
	}
	zai, _ := providers["zai"].(map[string]any)
	if zai["kind"] != "openai" {
		t.Fatalf("unrelated provider=%#v", providers["zai"])
	}
}

func TestRemoveProviderRejectsUnparseableDocument(t *testing.T) {
	if _, err := RemoveProvider([]byte("{broken")); err == nil {
		t.Fatal("unparseable document accepted")
	}
}

func TestDisableRemovesOwnedFragmentAndMarksCoordinatorOff(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", dir)
	home := filepath.Join(dir, "v2")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"login":{"vendor":"zai"},"provider":{"zai":{"kind":"openai"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	models := []catalog.Model{{
		ID:           "openai/gpt-5.6",
		Context:      catalog.ContextWindow{Tokens: 100000, Source: catalog.ContextStatic},
		Availability: catalog.Availability{Selectable: true},
	}}
	if err := ApplyWithModels(context.Background(), "http://127.0.0.1:1455", models); err != nil {
		t.Fatal(err)
	}
	if err := Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if login, _ := doc["login"].(map[string]any); login["vendor"] != "zai" {
		t.Fatalf("login=%#v", doc["login"])
	}
	providers, _ := doc["provider"].(map[string]any)
	if _, ok := providers["benes"]; ok {
		t.Fatal("owned fragment still present")
	}
	if _, ok := providers["zai"]; !ok {
		t.Fatal("unrelated provider dropped")
	}
	coord, err := managedfs.New(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != managedfs.OwnershipDisabled {
		t.Fatalf("ownership=%s", got)
	}
}

func TestDisableRemovesOnlyBenesContributionAfterZCodeMetadata(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", dir)
	home := filepath.Join(dir, "v2")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"login":{"vendor":"zai"},"provider":{"zai":{"kind":"openai"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	models := []catalog.Model{{
		ID:           "openai/gpt-5.6",
		Context:      catalog.ContextWindow{Tokens: 100000, Source: catalog.ContextStatic},
		Availability: catalog.Availability{Selectable: true},
	}}
	if err := ApplyWithModels(context.Background(), "http://127.0.0.1:1455", models); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	providers, _ := doc["provider"].(map[string]any)
	owned, _ := providers["benes"].(map[string]any)
	selectors, _ := owned["models"].(map[string]any)
	row, _ := selectors["openai/gpt-5.6"].(map[string]any)
	row["reasoning"] = map[string]any{"enabled": true}
	selectors["openai/gpt-5.6"] = row
	owned["models"] = selectors
	providers["benes"] = owned
	doc["provider"] = providers
	updated, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if login, _ := doc["login"].(map[string]any); login["vendor"] != "zai" {
		t.Fatalf("login=%#v", doc["login"])
	}
	providers, _ = doc["provider"].(map[string]any)
	if _, ok := providers["benes"]; ok {
		t.Fatal("owned fragment still present")
	}
	if _, ok := providers["zai"]; !ok {
		t.Fatal("unrelated provider dropped")
	}
}

func TestEnableReappliesOwnedFragmentAfterDisable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", dir)
	if err := Apply(context.Background(), "http://127.0.0.1:1455"); err != nil {
		t.Fatal(err)
	}
	if err := Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := Enable(context.Background(), "http://127.0.0.1:1455", nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "v2", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	providers, _ := doc["provider"].(map[string]any)
	owned, _ := providers["benes"].(map[string]any)
	if owned["kind"] != "openai-compatible" {
		t.Fatalf("owned=%#v", owned)
	}
	coord, err := managedfs.New(filepath.Join(dir, "v2"))
	if err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != managedfs.OwnershipCurrent {
		t.Fatalf("ownership=%s", got)
	}
}

func TestRecoverDelegatesToCoordinator(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", dir)
	if err := Apply(context.Background(), "http://127.0.0.1:9"); err != nil {
		t.Fatal(err)
	}
	if err := Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
}
