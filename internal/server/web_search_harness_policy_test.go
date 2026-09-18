package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/harnessboard"
	"github.com/Wibias/Benes/internal/harnesspolicy"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
)

func TestHarnessWebSearchPolicyMatrix(t *testing.T) {
	t.Parallel()

	newHandler := func(t *testing.T, global bool, override *harnesspolicy.Activation, withCandidate bool) *handler {
		t.Helper()
		home := t.TempDir()
		configPath := filepath.Join(home, "config.json")
		raw := []byte(`{"hostname":"127.0.0.1","port":18080,"webSearchSidecar":{"enabled":false}}`)
		if global {
			raw = []byte(`{"hostname":"127.0.0.1","port":18080,"webSearchSidecar":{"enabled":true}}`)
		}
		if err := os.WriteFile(configPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		h := &handler{configPath: configPath}
		if withCandidate {
			h.webSearch = map[string]websearch.Config{
				"p": {
					ProviderID:    "p",
					Endpoint:      "https://example.invalid/search",
					AuthClass:     "key",
					Wire:          "openai-responses",
					ModelID:       "m",
					AllowedModels: []string{"m"},
					Enabled:       true,
					APIKey:        "test-key",
					HTTPClient:    &http.Client{},
				},
			}
		}
		row := harnessboard.DefaultSettings()
		if override != nil {
			row.Sidecars = &harnesspolicy.Overrides{WebSearch: override}
		}
		runtime := newHarnessSidecarRuntime(map[string]harnessboard.Settings{"opencode": row}, nil)
		harnessSidecarRuntimes.Store(h, runtime)
		t.Cleanup(func() { forgetHarnessSidecarRuntime(h) })
		return h
	}
	ctxFor := func(id string) context.Context {
		if id == "" {
			return context.Background()
		}
		return context.WithValue(context.Background(), harnessIdentityContextKey{}, harnessRequestIdentity{ID: id, Status: harnessIdentityAccepted})
	}

	enabled := harnesspolicy.ActivationEnabled
	disabled := harnesspolicy.ActivationDisabled
	tests := []struct {
		name         string
		global       bool
		override     *harnesspolicy.Activation
		identity     string
		candidate    bool
		nativeProven bool
		wantEnabled  bool
		wantNative   bool
		wantSource   harnesspolicy.Source
		wantClient   bool
		wantErr      bool
	}{
		{name: "no identity inherits global on", global: true, candidate: true, wantEnabled: true, wantSource: harnesspolicy.SourceGlobal, wantClient: true},
		{name: "no identity inherits global off", global: false, candidate: true, wantSource: harnesspolicy.SourceGlobal, wantErr: true},
		{name: "harness enable beats global off", global: false, override: &enabled, identity: "opencode", candidate: true, wantEnabled: true, wantSource: harnesspolicy.SourceHarnessOverride, wantClient: true},
		{name: "harness disable beats global on", global: true, override: &disabled, identity: "opencode", candidate: true, wantSource: harnesspolicy.SourceHarnessOverride, wantErr: true},
		{name: "native beats harness disable", global: true, override: &disabled, identity: "opencode", candidate: true, nativeProven: true, wantEnabled: true, wantNative: true, wantSource: harnesspolicy.SourceRequestNative},
		{name: "enabled without proven sidecar fails closed", global: false, override: &enabled, identity: "opencode", candidate: false, wantSource: harnesspolicy.SourceUnsupported, wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			h := newHandler(t, test.global, test.override, test.candidate)
			decision, client, err := h.webSearchClientForRequest(ctxFor(test.identity), "p", "m", test.nativeProven)
			if (err != nil) != test.wantErr {
				t.Fatalf("err=%v wantErr=%v decision=%#v", err, test.wantErr, decision)
			}
			if test.wantErr && !errors.Is(err, websearch.ErrUnsupportedSelection) && test.candidate {
				t.Fatalf("unexpected policy error: %v", err)
			}
			if decision.Enabled != test.wantEnabled || decision.Native != test.wantNative || decision.Source != test.wantSource {
				t.Fatalf("decision=%#v want enabled=%v native=%v source=%q", decision, test.wantEnabled, test.wantNative, test.wantSource)
			}
			if (client != nil) != test.wantClient {
				t.Fatalf("client=%v wantClient=%v", client, test.wantClient)
			}
		})
	}
}

func TestWebSearchCapabilityRegistryMatchesNormalizedPaths(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"codex", "dsh", "opencode", "hermes", "openclaw", "grok"} {
		caps, ok := harnessSidecarCapabilitiesFor(id)
		if !ok || !caps.IdentityStampable || !caps.WebSearch {
			t.Fatalf("%s web-search capability=%#v ok=%v", id, caps, ok)
		}
	}
	for _, id := range []string{"claude-desktop", "claude", "pi", "prime", "omp", "kimi", "gajae", "mcode"} {
		caps, ok := harnessSidecarCapabilitiesFor(id)
		if !ok {
			t.Fatalf("missing registry row for %s", id)
		}
		if caps.WebSearch {
			t.Fatalf("%s must remain web-search unsupported without a proven normalized path", id)
		}
	}
}
