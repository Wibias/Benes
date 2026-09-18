package grok

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectSkipsMissingHome(t *testing.T) {
	result := Inject(23100, []Model{{ID: "gpt-4o"}}, InjectOptions{GrokHome: filepath.Join(t.TempDir(), "missing")})
	if !result.OK || result.Changed || result.SkippedReason != "no-grok-home" {
		t.Fatalf("%#v", result)
	}
}

func TestInjectWritesAndStripsLoopbackBlock(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("default = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := Inject(18080, []Model{{ID: "gpt-4o"}}, InjectOptions{GrokHome: home, Hostname: "127.0.0.1"})
	if !result.OK || !result.Changed {
		t.Fatalf("%#v", result)
	}
	body, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, BeginMarker) || !strings.Contains(text, "gpt-4o") || !strings.Contains(text, loopbackKey) {
		t.Fatalf("body=%s", text)
	}
	if !strings.Contains(text, `"X-Benes-Surface" = "grok"`) {
		t.Fatalf("missing declared Grok surface header: %s", text)
	}
	if strings.Contains(text, "sk-") || strings.Contains(strings.ToLower(text), "secret") {
		t.Fatalf("secret leaked: %s", text)
	}
	stripped := Strip(InjectOptions{GrokHome: home})
	if !stripped.OK || !stripped.Changed {
		t.Fatalf("%#v", stripped)
	}
	after, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), BeginMarker) {
		t.Fatalf("fence remained: %s", after)
	}
}

func TestReadStatusParsesFenceWithoutSecrets(t *testing.T) {
	home := t.TempDir()
	result := Inject(18080, []Model{{ID: "gpt-4o", ContextWindow: 128000}}, InjectOptions{GrokHome: home, Hostname: "127.0.0.1"})
	if !result.OK {
		t.Fatalf("%#v", result)
	}
	status := ReadStatus(InjectOptions{GrokHome: home})
	if !status.Present || status.BaseURL != "http://127.0.0.1:18080/v1" || len(status.Models) != 1 || status.Models[0].ID != "gpt-4o" {
		t.Fatalf("%#v", status)
	}
	if strings.Contains(status.ConfigPath, "api_key") {
		t.Fatalf("path=%s", status.ConfigPath)
	}
}

// The writer has one input: the catalogue it is handed. There is no third argument that could
// narrow it, which is what makes "Grok registers the Benes model set" structural rather than a
// promise the callers have to keep.
func TestInjectWritesEveryCataloguedModel(t *testing.T) {
	home := t.TempDir()
	models := []Model{{ID: "openai/gpt-5.6-sol", ContextWindow: 1050000}, {ID: "xai/grok-4.6", ContextWindow: 256000}}
	if result := Inject(23100, models, InjectOptions{GrokHome: home, Hostname: "127.0.0.1"}); !result.OK {
		t.Fatalf("%#v", result)
	}
	body, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, model := range models {
		if !strings.Contains(text, `model = "`+model.ID+`"`) {
			t.Fatalf("missing %s in %s", model.ID, text)
		}
	}
	status := ReadStatus(InjectOptions{GrokHome: home})
	if got := RegistrationFor(models, status, "http://127.0.0.1:23100/v1"); !got.Current || got.Registered != 2 || got.Catalogue != 2 {
		t.Fatalf("%#v", got)
	}
}

func TestInjectSkipsNonLoopback(t *testing.T) {
	home := t.TempDir()
	result := Inject(23100, []Model{{ID: "gpt-4o"}}, InjectOptions{GrokHome: home, Hostname: "10.0.0.2"})
	if !result.OK || result.SkippedReason != "non-loopback" || result.Changed {
		t.Fatalf("%#v", result)
	}
}
