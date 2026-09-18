package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/harnessboard"
	"github.com/Wibias/Benes/internal/harnesspolicy"
)

// Issue #257 retired the Claude-specific nested sidecar owners
// `claudeCode.webSearchSidecar` and `claudeCode.visionSidecar`. A config.json
// written before that change may still carry the blocks, but no supported
// surface may read, accept, project, or persist them as sidecar authority.
//
// These guards assert ownership on the active surfaces: the global config roots
// and their effective projection, the Claude Code settings DTO, the Sidecar
// settings projection, the Harness settings document, and the Harness settings
// endpoint. They deliberately do not ban the literal strings — historical
// references in design documents, the implementation plan, migration tests, and
// compatibility fixtures stay legal and none of them make these tests fail.
var retiredClaudeNestedSidecarFields = []string{"webSearchSidecar", "visionSidecar"}

func retiredClaudeNestedSidecarConfig(rootEnabled, nestedEnabled bool) string {
	return fmt.Sprintf(`{
  "hostname": "127.0.0.1",
  "port": 18080,
  "webSearchSidecar": {"enabled": %t, "backend": "root-search", "model": "root-search-model"},
  "visionSidecar": {"enabled": %t, "backend": "root-vision", "model": "root-vision-model"},
  "claudeCode": {
    "enabled": true,
    "webSearchSidecar": {"enabled": %t, "backend": "retired-search", "model": "retired-search-model"},
    "visionSidecar": {"enabled": %t, "backend": "retired-vision", "model": "retired-vision-model"}
  }
}`, rootEnabled, rootEnabled, nestedEnabled, nestedEnabled)
}

func retiredClaudeNestedSidecarHandler(t *testing.T, configJSON string) *handler {
	t.Helper()
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     claudeCodeConfigPath(t, configJSON),
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	attachHandlerClose(t, h)
	raw := h.(*handler)
	t.Cleanup(func() { forgetHarnessSidecarRuntime(raw) })
	return raw
}

func harnessSettingsPUT(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/harnesses/settings", strings.NewReader(body))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// Configured policy authority lives in the global sidecar sections at the
// config root. Both directions are asserted so the guard fails whether the
// retired nesting tries to switch a modality on or off.
func TestRetiredClaudeNestedSidecarsDoNotOwnConfiguredPolicy(t *testing.T) {
	cases := []struct {
		name          string
		rootEnabled   bool
		nestedEnabled bool
	}{
		{"retired nesting cannot activate a disabled global sidecar", false, true},
		{"retired nesting cannot deactivate an enabled global sidecar", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := retiredClaudeNestedSidecarHandler(t, retiredClaudeNestedSidecarConfig(tc.rootEnabled, tc.nestedEnabled))

			if got := raw.webSearchSidecarEnabled(); got != tc.rootEnabled {
				t.Fatalf("webSearchSidecarEnabled=%v want the root value %v", got, tc.rootEnabled)
			}

			configured := raw.configuredHarnessSidecarPolicy("opencode")
			if configured.WebSearch.Enabled != tc.rootEnabled || configured.WebSearch.Source != harnesspolicy.SourceGlobal {
				t.Fatalf("web search configured=%#v want enabled=%v source=global", configured.WebSearch, tc.rootEnabled)
			}
			if configured.Vision.Enabled != tc.rootEnabled || configured.Vision.Source != harnesspolicy.SourceGlobal {
				t.Fatalf("vision configured=%#v want enabled=%v source=global", configured.Vision, tc.rootEnabled)
			}

			view := raw.sidecarSettingsView()
			webSearch, _ := view["webSearch"].(map[string]any)
			vision, _ := view["vision"].(map[string]any)
			if webSearch["enabled"] != tc.rootEnabled || vision["enabled"] != tc.rootEnabled {
				t.Fatalf("sidecar settings view enabled web=%v vision=%v want %v", webSearch["enabled"], vision["enabled"], tc.rootEnabled)
			}
			// Model and backend are root-owned too: the retired block must not
			// redirect which model a sidecar describes with.
			if webSearch["model"] != "root-search-model" || webSearch["backend"] != "root-search" {
				t.Fatalf("web search model and backend must come from the root section: %v", webSearch)
			}
			if vision["model"] != "root-vision-model" || vision["backend"] != "root-vision" {
				t.Fatalf("vision model and backend must come from the root section: %v", vision)
			}
		})
	}
}

// The Claude Code settings contract owns Claude launch behavior, not sidecar
// activation. Its DTO must not project the retired owners, a request naming
// them must not be accepted or written, and a supported save must not
// materialize them.
func TestRetiredClaudeNestedSidecarsAreNotASupportedSettingsContract(t *testing.T) {
	configPath := claudeCodeConfigPath(t, retiredClaudeNestedSidecarConfig(true, true))
	h := claudeCodeContractHandlerAt(t, configPath, nil)

	got := claudeCodeGET(t, h)
	for _, field := range retiredClaudeNestedSidecarFields {
		if value, ok := got[field]; ok {
			t.Fatalf("Claude Code settings DTO projected retired owner %q: %v", field, value)
		}
	}
	if nested, ok := got["claudeCode"]; ok {
		t.Fatalf("Claude Code settings DTO must stay flat, got nested block %v", nested)
	}

	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range retiredClaudeNestedSidecarFields {
		rr := claudeCodeDo(t, h, http.MethodPut, fmt.Sprintf(`{"%s":{"backend":"openai"}}`, field))
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), `"invalid_body"`) {
			t.Fatalf("PUT %s must stay unsupported: status=%d body=%s", field, rr.Code, rr.Body.String())
		}
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("a retired nested sidecar PUT must not rewrite config.json")
	}

	freshPath := claudeCodeConfigPath(t, `{"hostname":"127.0.0.1","port":18080,"claudeCode":{"enabled":true}}`)
	fresh := claudeCodeContractHandlerAt(t, freshPath, nil)
	if rr := claudeCodeDo(t, fresh, http.MethodPut, `{"authMode":"proxy"}`); rr.Code != http.StatusOK {
		t.Fatalf("supported put status=%d body=%s", rr.Code, rr.Body.String())
	}
	raw, err := os.ReadFile(freshPath)
	if err != nil {
		t.Fatal(err)
	}
	var disk map[string]any
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	block, _ := disk["claudeCode"].(map[string]any)
	for _, field := range retiredClaudeNestedSidecarFields {
		if value, ok := block[field]; ok {
			t.Fatalf("supported Claude Code save materialized retired owner %q: %v", field, value)
		}
	}
	if block["authMode"] != "proxy" {
		t.Fatalf("supported save did not apply: %v", block)
	}
}

// Per-Harness sidecar policy has exactly one persisted owner: the `sidecars`
// field of a Harness row. Neither retired-shaped nesting in harnesses.json nor
// a retired-shaped patch through the settings endpoint may create, change, or
// clear an override.
func TestHarnessSidecarPolicyKeepsASingleOwnerField(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"hostname":"127.0.0.1","port":18080,"webSearchSidecar":{"enabled":false},"visionSidecar":{"enabled":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := `{"clients":{"opencode":{"autoDetect":true,"autoApply":true,"retainSnapshot":true,"allowRestart":false,"webSearchSidecar":"enabled","visionSidecar":"enabled"}}}`
	if err := os.WriteFile(harnessboard.SettingsPath(home), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	settings, err := harnessboard.LoadSettings(home)
	if err != nil {
		t.Fatal(err)
	}
	row, ok := settings["opencode"]
	if !ok || row.Sidecars != nil {
		t.Fatalf("retired nesting must not decode into a Harness sidecar override: %#v", row)
	}
	if _, ok := newHarnessSidecarRuntime(settings, nil).Overrides("opencode"); ok {
		t.Fatal("retired nesting must not reach the runtime override snapshot")
	}

	// The handler must use this home so the Harness settings document is the one
	// written above.
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	attachHandlerClose(t, h)
	raw := h.(*handler)
	t.Cleanup(func() { forgetHarnessSidecarRuntime(raw) })
	configured := raw.configuredHarnessSidecarPolicy("opencode")
	if configured.WebSearch.Enabled || configured.WebSearch.Source != harnesspolicy.SourceGlobal {
		t.Fatalf("retired nesting must not own web search: %#v", configured.WebSearch)
	}
	if configured.Vision.Enabled || configured.Vision.Source != harnesspolicy.SourceGlobal {
		t.Fatalf("retired nesting must not own vision: %#v", configured.Vision)
	}

	// Positive control: the one supported field is still a working override, so
	// the assertions above are not vacuous.
	if rr := harnessSettingsPUT(t, raw, `{"clientId":"opencode","sidecars":{"webSearch":"enabled"}}`); rr.Code != http.StatusOK {
		t.Fatalf("supported sidecar patch status=%d body=%s", rr.Code, rr.Body.String())
	}
	configured = raw.configuredHarnessSidecarPolicy("opencode")
	if !configured.WebSearch.Enabled || configured.WebSearch.Source != harnesspolicy.SourceHarnessOverride {
		t.Fatalf("supported sidecar patch must own web search: %#v", configured.WebSearch)
	}
	if configured.Vision.Source != harnesspolicy.SourceGlobal {
		t.Fatalf("a web search patch must not own vision: %#v", configured.Vision)
	}

	// Retired names stay unusable inside the single supported owner.
	if rr := harnessSettingsPUT(t, raw, `{"clientId":"opencode","sidecars":{"visionSidecar":"enabled"}}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("retired owner inside sidecars status=%d body=%s", rr.Code, rr.Body.String())
	}
	// A retired top-level nesting is not a supported patch and must not move state.
	if rr := harnessSettingsPUT(t, raw, `{"clientId":"opencode","visionSidecar":"enabled"}`); rr.Code != http.StatusOK {
		t.Fatalf("unrelated top-level field status=%d body=%s", rr.Code, rr.Body.String())
	}
	configured = raw.configuredHarnessSidecarPolicy("opencode")
	if !configured.WebSearch.Enabled || configured.WebSearch.Source != harnesspolicy.SourceHarnessOverride {
		t.Fatalf("retired top-level patch must not change web search: %#v", configured.WebSearch)
	}
	if configured.Vision.Source != harnesspolicy.SourceGlobal {
		t.Fatalf("retired top-level patch must not own vision: %#v", configured.Vision)
	}

	storedRaw, err := os.ReadFile(harnessboard.SettingsPath(home))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(storedRaw, &doc); err != nil {
		t.Fatal(err)
	}
	clients, _ := doc["clients"].(map[string]any)
	stored, _ := clients["opencode"].(map[string]any)
	for _, field := range retiredClaudeNestedSidecarFields {
		if value, ok := stored[field]; ok {
			t.Fatalf("persisted Harness row grew retired owner %q: %v", field, value)
		}
	}
	sidecars, _ := stored["sidecars"].(map[string]any)
	if len(sidecars) != 1 || sidecars["webSearch"] != "enabled" {
		t.Fatalf("persisted Harness sidecars=%v want only webSearch=enabled", stored["sidecars"])
	}
}
