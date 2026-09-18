package zcode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/managedfs"
)

func TestApplyWritesOwnedFragmentThroughCoordinator(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", dir)
	if err := Apply(context.Background(), "http://127.0.0.1:1455"); err != nil {
		t.Fatal(err)
	}
	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
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
	if owned["kind"] != "openai-compatible" {
		t.Fatalf("owned=%#v", owned)
	}
	if owned["baseURL"] != "http://127.0.0.1:1455/v1" {
		t.Fatalf("baseURL=%v", owned["baseURL"])
	}
	coord, err := managedfs.New(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := coord.Classify(); got != managedfs.OwnershipCurrent {
		t.Fatalf("ownership=%s", got)
	}
}

func TestApplyPreservesZaiLoginAndOtherProviders(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", dir)
	home := filepath.Join(dir, "v2")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := []byte(`{"login":{"vendor":"zai"},"provider":{"zai":{"kind":"openai","apiKey":"stay"}}}`)
	if err := os.WriteFile(filepath.Join(home, "config.json"), existing, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), "http://localhost:8080"); err != nil {
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
	login, _ := doc["login"].(map[string]any)
	if login["vendor"] != "zai" {
		t.Fatalf("login=%#v", login)
	}
	providers, _ := doc["provider"].(map[string]any)
	zai, _ := providers["zai"].(map[string]any)
	if zai["apiKey"] != "stay" {
		t.Fatalf("zai=%#v", zai)
	}
	owned, _ := providers["benes"].(map[string]any)
	if owned["kind"] != "openai-compatible" {
		t.Fatalf("owned=%#v", owned)
	}
}

func TestApplyRefusesUnparseableConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", dir)
	home := filepath.Join(dir, "v2")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "config.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), "http://127.0.0.1:1"); err == nil {
		t.Fatal("unparseable config accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{broken" {
		t.Fatalf("file mutated: %q", got)
	}
}

func TestApplyRejectsNonLoopbackOrigin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ZCODE_DATA_DIR", dir)
	if err := Apply(context.Background(), "https://proxy.example"); err == nil {
		t.Fatal("non-loopback origin accepted")
	}
	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("config written on rejected origin: %v", err)
	}
}
