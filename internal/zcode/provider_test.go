package zcode

import (
	"strings"
	"testing"
)

func TestProviderFragmentIsOpenAICompatibleChatLoopbackWithoutARealKey(t *testing.T) {
	got, err := ProviderFragment("http://127.0.0.1:1455")
	if err != nil {
		t.Fatal(err)
	}
	if got["kind"] != "openai-compatible" {
		t.Fatalf("kind=%v", got["kind"])
	}
	base, _ := got["baseURL"].(string)
	if base != "http://127.0.0.1:1455/v1" {
		t.Fatalf("baseURL=%q", base)
	}
	key, _ := got["apiKey"].(string)
	if key == "" || strings.HasPrefix(key, "sk-") || strings.Contains(strings.ToLower(key), "secret") {
		t.Fatalf("apiKey=%q", key)
	}
}

func TestProviderFragmentRejectsNonLoopbackOrigin(t *testing.T) {
	if _, err := ProviderFragment("https://proxy.example"); err == nil {
		t.Fatal("non-loopback origin accepted")
	}
}
