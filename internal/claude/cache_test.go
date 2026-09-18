package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteGatewayModelCacheFiltersAndOmitsSecrets(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteGatewayModelCache("http://127.0.0.1:18080", []GatewayModel{
		{ID: "claude-benes-native--gpt-5.5", DisplayName: "GPT"},
		{ID: "gpt-5.5"},
		{ID: "sk-secret"},
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "claude-benes-native--gpt-5.5") {
		t.Fatalf("%s", text)
	}
	if strings.Contains(text, "sk-secret") || strings.Contains(text, `"gpt-5.5"`) {
		t.Fatalf("unexpected: %s", text)
	}
	if filepath.Base(filepath.Dir(path)) != "cache" {
		t.Fatalf("path=%s", path)
	}
}
