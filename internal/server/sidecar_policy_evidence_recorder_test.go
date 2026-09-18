package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/harnessboard"
	"github.com/Wibias/Benes/internal/harnessidentity"
	"github.com/Wibias/Benes/internal/harnesspolicy"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
)

// The evidence is only useful if it reaches the existing usage record at the
// emission point, after policy resolution, sidecar execution, and any late
// identity update have all happened.
func TestRequestUsageRecordCarriesExecutedSidecarPolicyEvidence(t *testing.T) {
	home := t.TempDir()
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"search result","sources":[]}`))
	}))
	t.Cleanup(sidecar.Close)

	override := harnesspolicy.ActivationEnabled
	row := harnessboard.DefaultSettings()
	row.Sidecars = &harnesspolicy.Overrides{WebSearch: &override}
	if _, err := harnessboard.PutSettings(home, "opencode", row); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"hostname":"127.0.0.1","port":18080,"webSearchSidecar":{"enabled":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	usagePath := filepath.Join(home, "usage.jsonl")
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"p": &harnessSearchProvider{}},
		ConfigPath:     configPath,
		UsageLogPath:   usagePath,
		WebSearch: map[string]websearch.Config{
			"p": {
				ProviderID:    "p",
				Endpoint:      sidecar.URL,
				AuthClass:     "key",
				Wire:          "openai-responses",
				AllowedModels: []string{"m"},
				Enabled:       true,
				APIKey:        "sidecar-key",
				HTTPClient:    sidecar.Client(),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"p/m","store":false,"stream":false,"tools":[{"type":"web_search"}]}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set("X-Request-ID", "req_sidecar_policy_evidence")
	req.Header.Set(harnessidentity.Header, "opencode")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	recorded := readUsageLog(t, usagePath)
	for _, want := range []string{
		`"sidecarPolicy"`,
		`"identity":{"status":"accepted","harnessId":"opencode"}`,
		`"webSearch":{"configured":{"enabled":true,"source":"harness_override"},"resolution":"sidecar_enabled","ran":true}`,
		`"vision":{"configured":{"enabled":true,"source":"global"},"resolution":"unknown","ran":false}`,
	} {
		if !strings.Contains(recorded, want) {
			t.Fatalf("route decision missing %s: %s", want, recorded)
		}
	}
}

// A request can bind its identity before the Harness selector is known, or
// resolve it again late. The later update must finalise identity without losing
// sidecar policy or execution evidence already recorded for the request.
func TestSidecarPolicyEvidenceLateIdentityKeepsRecordedDecisions(t *testing.T) {
	home := t.TempDir()
	h := testHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     filepath.Join(home, "config.json"),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req = bindHarnessIdentity(req)
	diag := newDiagnosticsRecorder("req_late", "req_late", time.Now())
	diag.bindSidecarPolicyEvidence(h, req.Context())
	ctx := withDiagnosticsRecorder(req.Context(), diag)

	evidence := sidecarPolicyEvidenceFor(ctx)
	if evidence == nil {
		t.Fatal("expected request-scoped sidecar evidence")
	}
	evidence.recordWebSearchDecision(harnesspolicy.Decision{Enabled: true, Source: harnesspolicy.SourceHarnessOverride})
	evidence.markWebSearchRan()
	evidence.recordVisionDecision(harnesspolicy.Decision{Enabled: false, Source: harnesspolicy.SourceGlobal})

	snapshot := diag.finalSidecarPolicyEvidence(harnessRequestIdentity{ID: "opencode", Status: harnessIdentityAccepted})
	if snapshot == nil {
		t.Fatal("expected terminal evidence snapshot")
	}
	if snapshot.Identity.Status != harnessIdentityAccepted || snapshot.Identity.HarnessID != "opencode" {
		t.Fatalf("late identity not resolved: %#v", snapshot.Identity)
	}
	if snapshot.WebSearch.Resolution != sidecarResolutionEnabled || !snapshot.WebSearch.Ran {
		t.Fatalf("late identity dropped web search evidence: %#v", snapshot.WebSearch)
	}
	if snapshot.Vision.Resolution != sidecarResolutionPolicyDisabled || snapshot.Vision.Ran {
		t.Fatalf("late identity dropped vision evidence: %#v", snapshot.Vision)
	}

	decision := usageRouteDecision(requestTelemetryRecord{SidecarPolicy: snapshot})
	if decision.SidecarPolicy == nil {
		t.Fatal("usage route decision dropped sidecar policy evidence")
	}
	if decision.SidecarPolicy.Identity.HarnessID != "opencode" {
		t.Fatalf("usage identity = %#v", decision.SidecarPolicy.Identity)
	}
	if decision.SidecarPolicy.WebSearch.Resolution != string(sidecarResolutionEnabled) || !decision.SidecarPolicy.WebSearch.Ran {
		t.Fatalf("usage web search = %#v", decision.SidecarPolicy.WebSearch)
	}
	if decision.SidecarPolicy.Vision.Resolution != string(sidecarResolutionPolicyDisabled) || decision.SidecarPolicy.Vision.Ran {
		t.Fatalf("usage vision = %#v", decision.SidecarPolicy.Vision)
	}
}

func TestSidecarPolicyEvidenceSnapshotIsImmutableAfterEmission(t *testing.T) {
	recorder := newSidecarPolicyEvidenceRecorder(
		harnessRequestIdentity{ID: "opencode", Status: harnessIdentityAccepted},
		configuredHarnessSidecarPolicy{},
	)
	recorder.recordVisionDecision(harnesspolicy.Decision{Enabled: true, Source: harnesspolicy.SourceGlobal})
	emitted := recorder.snapshot()

	recorder.markVisionRan()
	recorder.recordVisionFailure(sidecarFailureTransform)
	recorder.recordVisionDecision(harnesspolicy.Decision{Enabled: false, Source: harnesspolicy.SourceUnsupported})

	if emitted.Vision.Ran || emitted.Vision.Failure != sidecarFailureNone {
		t.Fatalf("emitted snapshot mutated: %#v", emitted.Vision)
	}
	if emitted.Vision.Resolution != sidecarResolutionEnabled {
		t.Fatalf("emitted resolution mutated: %#v", emitted.Vision)
	}
}

// Policy resolution, pre-dispatch sidecar work, and usage emission can touch the
// recorder from different goroutines within one request.
func TestSidecarPolicyEvidenceRecorderSurvivesConcurrentUpdates(t *testing.T) {
	recorder := newSidecarPolicyEvidenceRecorder(
		harnessRequestIdentity{Status: harnessIdentityMissing},
		configuredHarnessSidecarPolicy{},
	)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recorder.recordWebSearchDecision(harnesspolicy.Decision{Enabled: true, Source: harnesspolicy.SourceGlobal})
			recorder.recordVisionDecision(harnesspolicy.Decision{Enabled: true, Source: harnesspolicy.SourceGlobal})
			recorder.markWebSearchRan()
			recorder.markVisionRan()
			recorder.recordIdentity(harnessRequestIdentity{ID: "opencode", Status: harnessIdentityAccepted})
			_ = recorder.snapshot()
		}()
	}
	wg.Wait()

	got := recorder.snapshot()
	if !got.WebSearch.Ran || !got.Vision.Ran {
		t.Fatalf("concurrent execution evidence lost: %#v", got)
	}
	if got.WebSearch.Resolution != sidecarResolutionEnabled || got.Vision.Resolution != sidecarResolutionEnabled {
		t.Fatalf("concurrent decisions lost: %#v", got)
	}
	if got.Identity.HarnessID != "opencode" {
		t.Fatalf("concurrent identity lost: %#v", got.Identity)
	}
}

// A request without an accepted identity must never keep a Harness id, even if
// one is offered late.
func TestSidecarPolicyEvidenceUnacceptedIdentityKeepsNoHarnessID(t *testing.T) {
	recorder := newSidecarPolicyEvidenceRecorder(
		harnessRequestIdentity{Status: harnessIdentityMissing},
		configuredHarnessSidecarPolicy{},
	)
	recorder.recordIdentity(harnessRequestIdentity{ID: "opencode", Status: harnessIdentityMissing})
	got := recorder.snapshot()
	if got.Identity.Status != harnessIdentityMissing || got.Identity.HarnessID != "" {
		t.Fatalf("missing identity kept a harness id: %#v", got.Identity)
	}
}
