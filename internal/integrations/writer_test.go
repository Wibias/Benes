package integrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/export"
)

func TestApplyDisablePreservesSiblingsAndOmitsSecrets(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	detect := filepath.Join(xdg, "opencode")
	if err := os.MkdirAll(detect, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(detect, "opencode.json")
	if err := os.WriteFile(configPath, []byte(`{"provider":{"other":{"name":"keep"}},"mcp":{"a":true}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(home, "integrations"))
	in := Input{
		ClientID: "opencode",
		Home:     home,
		Env:      map[string]string{"XDG_CONFIG_HOME": xdg},
		Store:    store,
		BaseURL:  "http://127.0.0.1:18080/v1",
		Hostname: "127.0.0.1",
		Models:   []export.Model{{Namespaced: "openai-apikey/gpt-5.5", Provider: "openai-apikey", ID: "gpt-5.5", ContextWindow: 128000}},
	}
	got := Apply(in)
	if !got.OK || !got.Changed {
		t.Fatalf("%+v", got)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "sk-") || strings.Contains(text, "secret") {
		t.Fatalf("secret leaked: %s", text)
	}
	if !strings.Contains(text, `"other"`) || !strings.Contains(text, `"mcp"`) || !strings.Contains(text, export.ProviderID) {
		t.Fatalf("merge lost siblings: %s", text)
	}
	status := Status(in)
	if status.State != StateCurrent || !status.Installed {
		t.Fatalf("status=%+v", status)
	}
	disabled := Disable(in)
	if !disabled.OK || !disabled.Changed {
		t.Fatalf("disable=%+v", disabled)
	}
	raw, _ = os.ReadFile(configPath)
	text = string(raw)
	if strings.Contains(text, export.ProviderID) {
		t.Fatalf("benes block remained: %s", text)
	}
	if !strings.Contains(text, `"other"`) {
		t.Fatalf("user block removed: %s", text)
	}
}

func TestApplyRefusesUnownedBenesBlock(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	detect := filepath.Join(xdg, "opencode")
	if err := os.MkdirAll(detect, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(detect, "opencode.json")
	if err := os.WriteFile(configPath, []byte(`{"provider":{"benes":{"name":"user"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Apply(Input{
		ClientID: "opencode", Home: home, Env: map[string]string{"XDG_CONFIG_HOME": xdg},
		Store:   NewStore(filepath.Join(home, "integrations")),
		BaseURL: "http://127.0.0.1:18080/v1", Hostname: "127.0.0.1",
		Models: []export.Model{{Namespaced: "openai-apikey/gpt-5.5", Provider: "openai-apikey", ID: "gpt-5.5"}},
	})
	if got.OK || got.Reason != "conflict" {
		t.Fatalf("%+v", got)
	}
}

func TestRestoreRequiresConfirmOnDrift(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	detect := filepath.Join(xdg, "opencode")
	if err := os.MkdirAll(detect, 0o700); err != nil {
		t.Fatal(err)
	}
	store := NewStore(filepath.Join(home, "integrations"))
	in := Input{
		ClientID: "opencode", Home: home, Env: map[string]string{"XDG_CONFIG_HOME": xdg}, Store: store,
		BaseURL: "http://127.0.0.1:18080/v1", Hostname: "127.0.0.1",
		Models: []export.Model{{Namespaced: "openai-apikey/gpt-5.5", Provider: "openai-apikey", ID: "gpt-5.5"}},
	}
	applied := Apply(in)
	if !applied.OK {
		t.Fatalf("%+v", applied)
	}
	configPath := filepath.Join(detect, "opencode.json")
	if err := os.WriteFile(configPath, []byte(`{"edited":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	restored := Restore(Input{ClientID: "opencode", Home: home, Env: in.Env, Store: store, OpID: applied.OpID})
	if restored.OK || restored.Reason != "drift_requires_confirm" {
		t.Fatalf("%+v", restored)
	}
	confirmed := Restore(Input{ClientID: "opencode", Home: home, Env: in.Env, Store: store, OpID: applied.OpID, ConfirmDrift: true})
	if !confirmed.OK {
		t.Fatalf("%+v", confirmed)
	}
}

func TestNotInstalledIsRefusal(t *testing.T) {
	home := t.TempDir()
	got := Apply(Input{
		ClientID: "opencode", Home: home, Env: map[string]string{"XDG_CONFIG_HOME": filepath.Join(home, "xdg")},
		Store:   NewStore(filepath.Join(home, "integrations")),
		BaseURL: "http://127.0.0.1:18080/v1", Hostname: "127.0.0.1",
		Models: []export.Model{{Namespaced: "n/m", Provider: "n", ID: "m"}},
	})
	if got.OK || got.Reason != "not_installed" {
		t.Fatalf("%+v", got)
	}
}
