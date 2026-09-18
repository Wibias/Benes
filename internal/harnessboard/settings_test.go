package harnessboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/harnesspolicy"
)

func policyActivationPtr(value harnesspolicy.Activation) *harnesspolicy.Activation {
	return &value
}

func TestLoadSettingsMissingFileIsEmpty(t *testing.T) {
	got, err := LoadSettings(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestPutSettingsRoundTrip(t *testing.T) {
	home := t.TempDir()
	want := Settings{AutoDetect: false, AutoApply: true, RetainSnapshot: false, AllowRestart: true}
	got, err := PutSettings(home, "opencode", want)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("put=%#v", got)
	}
	all, err := LoadSettings(home)
	if err != nil {
		t.Fatal(err)
	}
	if all["opencode"] != want {
		t.Fatalf("loaded %#v", all["opencode"])
	}
	raw, err := os.ReadFile(SettingsPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(SettingsPath(home)) {
		t.Fatal("settings path must be absolute")
	}
	if len(raw) == 0 {
		t.Fatal("expected settings file")
	}
}

func TestLoadSettingsLegacyFileInheritsSidecars(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(SettingsPath(home), []byte(`{
  "clients": {
    "opencode": {
      "autoDetect": false,
      "autoApply": true,
      "retainSnapshot": true,
      "allowRestart": false
    }
  }
}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	all, err := LoadSettings(home)
	if err != nil {
		t.Fatal(err)
	}
	if all["opencode"].Sidecars != nil {
		t.Fatalf("legacy settings gained overrides: %#v", all["opencode"].Sidecars)
	}
}

func TestPutSettingsSidecarsRoundTrip(t *testing.T) {
	home := t.TempDir()
	want := DefaultSettings()
	want.Sidecars = &harnesspolicy.Overrides{
		WebSearch: policyActivationPtr(harnesspolicy.ActivationEnabled),
		Vision:    policyActivationPtr(harnesspolicy.ActivationDisabled),
	}

	if _, err := PutSettings(home, "opencode", want); err != nil {
		t.Fatal(err)
	}
	all, err := LoadSettings(home)
	if err != nil {
		t.Fatal(err)
	}
	got := all["opencode"]
	if got.Sidecars == nil || got.Sidecars.WebSearch == nil || got.Sidecars.Vision == nil {
		t.Fatalf("loaded sidecars=%#v", got.Sidecars)
	}
	if *got.Sidecars.WebSearch != harnesspolicy.ActivationEnabled {
		t.Fatalf("webSearch=%q", *got.Sidecars.WebSearch)
	}
	if *got.Sidecars.Vision != harnesspolicy.ActivationDisabled {
		t.Fatalf("vision=%q", *got.Sidecars.Vision)
	}
}

func TestSaveSettingsOmitsEmptySidecars(t *testing.T) {
	home := t.TempDir()
	row := DefaultSettings()
	row.Sidecars = &harnesspolicy.Overrides{}
	if err := SaveSettings(home, map[string]Settings{"opencode": row}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(SettingsPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"sidecars"`) {
		t.Fatalf("empty sidecars must be omitted: %s", raw)
	}
}

func TestLoadSettingsRejectsInvalidSidecarActivation(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(SettingsPath(home), []byte(`{
  "clients": {
    "opencode": {
      "autoDetect": true,
      "autoApply": true,
      "retainSnapshot": true,
      "allowRestart": false,
      "sidecars": {"webSearch": "inherit"}
    }
  }
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSettings(home); err == nil {
		t.Fatal("expected invalid persisted activation to make settings unreadable")
	}
}

func TestPutSettingsPreservesOtherHarnessSidecars(t *testing.T) {
	home := t.TempDir()
	opencode := DefaultSettings()
	opencode.Sidecars = &harnesspolicy.Overrides{WebSearch: policyActivationPtr(harnesspolicy.ActivationDisabled)}
	codex := DefaultSettings()
	codex.Sidecars = &harnesspolicy.Overrides{Vision: policyActivationPtr(harnesspolicy.ActivationEnabled)}
	if err := SaveSettings(home, map[string]Settings{"opencode": opencode, "codex": codex}); err != nil {
		t.Fatal(err)
	}

	opencode.AutoDetect = false
	if _, err := PutSettings(home, "opencode", opencode); err != nil {
		t.Fatal(err)
	}
	all, err := LoadSettings(home)
	if err != nil {
		t.Fatal(err)
	}
	if all["codex"].Sidecars == nil || all["codex"].Sidecars.Vision == nil || *all["codex"].Sidecars.Vision != harnesspolicy.ActivationEnabled {
		t.Fatalf("codex sidecars changed: %#v", all["codex"].Sidecars)
	}
}

func TestPutSettingsRejectsUnknownClient(t *testing.T) {
	if _, err := PutSettings(t.TempDir(), "not-a-client", DefaultSettings()); err == nil {
		t.Fatal("expected error")
	}
}
