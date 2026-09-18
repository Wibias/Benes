package server

import (
	"encoding/json"
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

func TestHarnessSidecarCapabilityRegistryIsExplicitAndFailClosed(t *testing.T) {
	t.Parallel()

	stampable := map[string]bool{
		"codex": true, "dsh": true, "opencode": true, "hermes": true, "openclaw": true, "grok": true,
	}
	if len(harnessSidecarRegistry) != len(harnessboard.IDs) {
		t.Fatalf("registry has %d rows, want %d", len(harnessSidecarRegistry), len(harnessboard.IDs))
	}
	for _, id := range harnessboard.IDs {
		caps, ok := harnessSidecarCapabilitiesFor(id)
		if !ok {
			t.Fatalf("missing registry row for %q", id)
		}
		if err := validateHarnessSidecarCapabilities(id, caps); err != nil {
			t.Fatalf("registry row %q invalid: %v", id, err)
		}
		if caps.IdentityStampable != stampable[id] {
			t.Fatalf("identityStampable[%q]=%v want=%v", id, caps.IdentityStampable, stampable[id])
		}
		if !caps.IdentityStampable && (caps.WebSearch || caps.Vision) {
			t.Fatalf("non-stampable %q exposes modality capability: %#v", id, caps)
		}
	}
	if _, ok := harnessSidecarCapabilitiesFor("not-a-harness"); ok {
		t.Fatal("unknown harness must fail closed")
	}

	if err := validateHarnessSidecarCapabilities("opencode", harnessSidecarCapabilities{IdentityStampable: true, WebSearch: true}); err != nil {
		t.Fatalf("web-only capability must be representable: %v", err)
	}
	if err := validateHarnessSidecarCapabilities("opencode", harnessSidecarCapabilities{IdentityStampable: true, Vision: true}); err != nil {
		t.Fatalf("vision-only capability must be representable: %v", err)
	}
	if err := validateHarnessSidecarCapabilities("opencode", harnessSidecarCapabilities{WebSearch: true}); err == nil {
		t.Fatal("modality capability without identity stamping must be rejected")
	}
	if err := validateHarnessSidecarCapabilities("not-a-harness", harnessSidecarCapabilities{IdentityStampable: true}); err == nil {
		t.Fatal("unknown registry id must be rejected")
	}
}

func TestHarnessSidecarRuntimeSnapshotsOnlyOverrides(t *testing.T) {
	t.Parallel()

	enabled := harnesspolicy.ActivationEnabled
	runtime := newHarnessSidecarRuntime(map[string]harnessboard.Settings{
		"opencode": {
			Sidecars: &harnesspolicy.Overrides{WebSearch: &enabled},
		},
	}, nil)
	if !runtime.Valid() {
		t.Fatal("runtime should be valid")
	}
	got, ok := runtime.Overrides("opencode")
	if !ok || got.WebSearch == nil || *got.WebSearch != harnesspolicy.ActivationEnabled {
		t.Fatalf("startup overrides=%#v ok=%v", got, ok)
	}

	// Returned values must not be aliases into the immutable snapshot.
	*got.WebSearch = harnesspolicy.ActivationDisabled
	gotAgain, ok := runtime.Overrides("opencode")
	if !ok || gotAgain.WebSearch == nil || *gotAgain.WebSearch != harnesspolicy.ActivationEnabled {
		t.Fatalf("snapshot was mutated through returned value: %#v", gotAgain)
	}

	runtime.Replace(map[string]harnessboard.Settings{"opencode": harnessboard.DefaultSettings()})
	if _, ok := runtime.Overrides("opencode"); ok {
		t.Fatal("clearing the last override must remove the runtime entry")
	}
}

func TestHandlerHarnessSidecarSnapshotLoadsWithoutMakingCorruptionFatal(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"hostname":"127.0.0.1","port":18080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	enabled := harnesspolicy.ActivationEnabled
	row := harnessboard.DefaultSettings()
	row.Sidecars = &harnesspolicy.Overrides{Vision: &enabled}
	if _, err := harnessboard.PutSettings(home, "opencode", row); err != nil {
		t.Fatal(err)
	}

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
	runtime := harnessSidecarRuntimeFor(raw)
	got, ok := runtime.Overrides("opencode")
	if !ok || got.Vision == nil || *got.Vision != harnesspolicy.ActivationEnabled {
		t.Fatalf("handler snapshot=%#v ok=%v", got, ok)
	}

	if err := os.WriteFile(harnessboard.SettingsPath(home), []byte(`{"clients":{"opencode":{"sidecars":{"vision":"inherit"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	corruptHandler, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.5"}},
	})
	if err != nil {
		t.Fatalf("corrupt optional Harness policy must not take down the proxy: %v", err)
	}
	attachHandlerClose(t, corruptHandler)
	corrupt := corruptHandler.(*handler)
	t.Cleanup(func() { forgetHarnessSidecarRuntime(corrupt) })
	corruptRuntime := harnessSidecarRuntimeFor(corrupt)
	if corruptRuntime.Valid() {
		t.Fatal("corrupt Harness settings must mark the runtime snapshot invalid")
	}
	if _, ok := corruptRuntime.Overrides("opencode"); ok {
		t.Fatal("corrupt Harness settings must not publish untrusted overrides")
	}
}

func TestConfiguredHarnessSidecarPolicyReadsGlobalStateLive(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "hostname":"127.0.0.1",
  "port":18080,
  "webSearchSidecar":{"enabled":true},
  "visionSidecar":{"enabled":true}
}`), 0o600); err != nil {
		t.Fatal(err)
	}

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
	if !configured.WebSearch.Enabled || configured.WebSearch.Source != harnesspolicy.SourceGlobal {
		t.Fatalf("initial web configured=%#v", configured.WebSearch)
	}
	if !configured.Vision.Enabled || configured.Vision.Source != harnesspolicy.SourceGlobal {
		t.Fatalf("initial vision configured=%#v", configured.Vision)
	}

	globalOff := httptest.NewRequest(http.MethodPut, "/api/sidecar-settings", strings.NewReader(`{"webSearch":{"enabled":false},"vision":{"enabled":false}}`))
	globalOff.Host = "127.0.0.1"
	globalOffRR := httptest.NewRecorder()
	raw.ServeHTTP(globalOffRR, globalOff)
	if globalOffRR.Code != http.StatusOK {
		t.Fatalf("global toggle status=%d body=%s", globalOffRR.Code, globalOffRR.Body.String())
	}
	configured = raw.configuredHarnessSidecarPolicy("opencode")
	if configured.WebSearch.Enabled || configured.WebSearch.Source != harnesspolicy.SourceGlobal {
		t.Fatalf("web global change not live: %#v", configured.WebSearch)
	}
	if configured.Vision.Enabled || configured.Vision.Source != harnesspolicy.SourceGlobal {
		t.Fatalf("vision global change not live: %#v", configured.Vision)
	}

	harnessOn := httptest.NewRequest(http.MethodPut, "/api/harnesses/settings", strings.NewReader(`{"clientId":"opencode","sidecars":{"webSearch":"enabled","vision":"enabled"}}`))
	harnessOn.Host = "127.0.0.1"
	harnessOnRR := httptest.NewRecorder()
	raw.ServeHTTP(harnessOnRR, harnessOn)
	if harnessOnRR.Code != http.StatusOK {
		t.Fatalf("harness override status=%d body=%s", harnessOnRR.Code, harnessOnRR.Body.String())
	}
	configured = raw.configuredHarnessSidecarPolicy("opencode")
	if !configured.WebSearch.Enabled || configured.WebSearch.Source != harnesspolicy.SourceHarnessOverride {
		t.Fatalf("web harness override=%#v", configured.WebSearch)
	}
	if !configured.Vision.Enabled || configured.Vision.Source != harnesspolicy.SourceHarnessOverride {
		t.Fatalf("vision harness override=%#v", configured.Vision)
	}

	clear := httptest.NewRequest(http.MethodPut, "/api/harnesses/settings", strings.NewReader(`{"clientId":"opencode","sidecars":{"webSearch":null,"vision":null}}`))
	clear.Host = "127.0.0.1"
	clearRR := httptest.NewRecorder()
	raw.ServeHTTP(clearRR, clear)
	if clearRR.Code != http.StatusOK {
		t.Fatalf("clear override status=%d body=%s", clearRR.Code, clearRR.Body.String())
	}
	configured = raw.configuredHarnessSidecarPolicy("opencode")
	if configured.WebSearch.Enabled || configured.WebSearch.Source != harnesspolicy.SourceGlobal || configured.Vision.Enabled || configured.Vision.Source != harnesspolicy.SourceGlobal {
		t.Fatalf("clear did not resume current global state: %#v", configured)
	}
}

func TestHarnessesGETProjectsServerSidecarPolicy(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	if err := os.MkdirAll(filepath.Join(xdg, "opencode", "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"hostname":"127.0.0.1","port":18080,"webSearchSidecar":{"enabled":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	harnessboard.UserHome = func() (string, error) { return home, nil }
	harnessboard.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	harnessboard.ListProcesses = func() ([]harnessboard.Process, error) { return nil, nil }
	t.Cleanup(harnessboard.ResetHooks)

	enabled := harnesspolicy.ActivationEnabled
	row := harnessboard.DefaultSettings()
	row.Sidecars = &harnesspolicy.Overrides{WebSearch: &enabled}
	if _, err := harnessboard.PutSettings(home, "opencode", row); err != nil {
		t.Fatal(err)
	}

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
	req := httptest.NewRequest(http.MethodGet, "/api/harnesses", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var payload struct {
		Clients []struct {
			ClientID      string `json:"clientId"`
			SidecarPolicy struct {
				Capabilities harnessSidecarCapabilities `json:"capabilities"`
				Configured   struct {
					WebSearch configuredHarnessSidecar `json:"webSearch"`
					Vision    configuredHarnessSidecar `json:"vision"`
				} `json:"configured"`
			} `json:"sidecarPolicy"`
		} `json:"clients"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, client := range payload.Clients {
		if client.ClientID != "opencode" {
			continue
		}
		if !client.SidecarPolicy.Capabilities.IdentityStampable || !client.SidecarPolicy.Capabilities.WebSearch || !client.SidecarPolicy.Capabilities.Vision {
			t.Fatalf("unexpected opencode capabilities: %#v", client.SidecarPolicy.Capabilities)
		}
		if !client.SidecarPolicy.Configured.WebSearch.Enabled || client.SidecarPolicy.Configured.WebSearch.Source != harnesspolicy.SourceHarnessOverride {
			t.Fatalf("configured web projection=%#v", client.SidecarPolicy.Configured.WebSearch)
		}
		if !client.SidecarPolicy.Configured.Vision.Enabled || client.SidecarPolicy.Configured.Vision.Source != harnesspolicy.SourceGlobal {
			t.Fatalf("configured vision projection=%#v", client.SidecarPolicy.Configured.Vision)
		}
		return
	}
	t.Fatal("missing opencode projection")
}
