package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/harnesspolicy"
	"github.com/Wibias/Benes/internal/requesthistory"
)

func TestSidecarPolicyEvidenceRecordsAcceptedIdentityConfigurationAndExecution(t *testing.T) {
	recorder := newSidecarPolicyEvidenceRecorder(
		harnessRequestIdentity{ID: "opencode", Status: harnessIdentityAccepted},
		configuredHarnessSidecarPolicy{
			WebSearch: configuredHarnessSidecar{Enabled: true, Source: harnesspolicy.SourceHarnessOverride},
			Vision:    configuredHarnessSidecar{Enabled: false, Source: harnesspolicy.SourceGlobal},
		},
	)

	recorder.recordWebSearchDecision(harnesspolicy.Decision{
		Enabled: true,
		Source:  harnesspolicy.SourceHarnessOverride,
	})
	recorder.markWebSearchRan()
	recorder.recordVisionDecision(harnesspolicy.Decision{
		Enabled: false,
		Source:  harnesspolicy.SourceGlobal,
	})

	got := recorder.snapshot()
	if got.Identity.Status != harnessIdentityAccepted || got.Identity.HarnessID != "opencode" {
		t.Fatalf("identity = %#v", got.Identity)
	}
	if got.WebSearch.Configured.Source != harnesspolicy.SourceHarnessOverride || !got.WebSearch.Configured.Enabled {
		t.Fatalf("web search configured = %#v", got.WebSearch.Configured)
	}
	if got.WebSearch.Resolution != sidecarResolutionEnabled || !got.WebSearch.Ran {
		t.Fatalf("web search evidence = %#v", got.WebSearch)
	}
	if got.Vision.Configured.Source != harnesspolicy.SourceGlobal || got.Vision.Configured.Enabled {
		t.Fatalf("vision configured = %#v", got.Vision.Configured)
	}
	if got.Vision.Resolution != sidecarResolutionPolicyDisabled || got.Vision.Ran {
		t.Fatalf("vision evidence = %#v", got.Vision)
	}
}

func TestSidecarPolicyEvidenceResolutionCategories(t *testing.T) {
	tests := []struct {
		name string
		in   harnesspolicy.Decision
		want sidecarResolution
	}{
		{
			name: "request native",
			in: harnesspolicy.Decision{
				Enabled: true,
				Native:  true,
				Source:  harnesspolicy.SourceRequestNative,
			},
			want: sidecarResolutionRequestNative,
		},
		{
			name: "sidecar enabled",
			in: harnesspolicy.Decision{
				Enabled: true,
				Source:  harnesspolicy.SourceHarnessOverride,
			},
			want: sidecarResolutionEnabled,
		},
		{
			name: "policy disabled",
			in: harnesspolicy.Decision{
				Enabled: false,
				Source:  harnesspolicy.SourceGlobal,
			},
			want: sidecarResolutionPolicyDisabled,
		},
		{
			name: "unsupported",
			in: harnesspolicy.Decision{
				Enabled: false,
				Source:  harnesspolicy.SourceUnsupported,
			},
			want: sidecarResolutionUnsupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sidecarResolutionForDecision(tt.in); got != tt.want {
				t.Fatalf("resolution = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSidecarPolicyEvidenceRejectedAndMissingIdentityUseSafeStructuredEvidence(t *testing.T) {
	tests := []struct {
		name     string
		identity harnessRequestIdentity
		want     harnessIdentityStatus
	}{
		{name: "rejected", identity: harnessRequestIdentity{Status: harnessIdentityRejected}, want: harnessIdentityRejected},
		{name: "missing", identity: harnessRequestIdentity{Status: harnessIdentityMissing}, want: harnessIdentityMissing},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := newSidecarPolicyEvidenceRecorder(tt.identity, configuredHarnessSidecarPolicy{
				WebSearch: configuredHarnessSidecar{Enabled: true, Source: harnesspolicy.SourceGlobal},
				Vision:    configuredHarnessSidecar{Enabled: true, Source: harnesspolicy.SourceGlobal},
			})
			got := recorder.snapshot()
			if got.Identity.Status != tt.want {
				t.Fatalf("identity status = %q, want %q", got.Identity.Status, tt.want)
			}
			if got.Identity.HarnessID != "" {
				t.Fatalf("unexpected harness id %q", got.Identity.HarnessID)
			}
			if got.WebSearch.Configured.Source != harnesspolicy.SourceGlobal || got.Vision.Configured.Source != harnesspolicy.SourceGlobal {
				t.Fatalf("expected global configuration, got web=%#v vision=%#v", got.WebSearch.Configured, got.Vision.Configured)
			}
		})
	}
}

func TestSidecarPolicyEvidenceRecordsVisionTransformFailureAsCategory(t *testing.T) {
	recorder := newSidecarPolicyEvidenceRecorder(
		harnessRequestIdentity{ID: "opencode", Status: harnessIdentityAccepted},
		configuredHarnessSidecarPolicy{
			Vision: configuredHarnessSidecar{Enabled: true, Source: harnesspolicy.SourceHarnessOverride},
		},
	)
	recorder.recordVisionDecision(harnesspolicy.Decision{
		Enabled: true,
		Source:  harnesspolicy.SourceHarnessOverride,
	})
	recorder.markVisionRan()
	recorder.recordVisionFailure(sidecarFailureTransform)

	got := recorder.snapshot().Vision
	if !got.Ran {
		t.Fatal("vision sidecar should be recorded as having run")
	}
	if got.Failure != sidecarFailureTransform {
		t.Fatalf("failure = %q, want %q", got.Failure, sidecarFailureTransform)
	}
}

func TestSidecarPolicyEvidenceSerializationDoesNotLeakSensitiveRequestData(t *testing.T) {
	const (
		rejectedSelector = "rejected-harness,second-value"
		promptSentinel   = "PROMPT-SENTINEL-private-request-body"
		imageSentinel    = "data:image/png;base64,VE9QLVNFQ1JFVC1JTUFHRQ=="
		tokenSentinel    = "sk-TASK9-SUPER-SECRET"
		headerSentinel   = "X-Private-Debug: arbitrary-sensitive-header-value"
	)

	recorder := newSidecarPolicyEvidenceRecorder(
		harnessRequestIdentity{Status: harnessIdentityRejected},
		configuredHarnessSidecarPolicy{
			WebSearch: configuredHarnessSidecar{Enabled: false, Source: harnesspolicy.SourceGlobal},
			Vision:    configuredHarnessSidecar{Enabled: true, Source: harnesspolicy.SourceGlobal},
		},
	)
	recorder.recordVisionDecision(harnesspolicy.Decision{Enabled: false, Source: harnesspolicy.SourceUnsupported})

	raw, err := json.Marshal(recorder.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(raw)
	for _, forbidden := range []string{rejectedSelector, promptSentinel, imageSentinel, tokenSentinel, headerSentinel} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("serialized evidence leaked %q: %s", forbidden, serialized)
		}
	}
	if strings.Contains(serialized, "harnessId") {
		t.Fatalf("rejected identity must not serialize a harness id: %s", serialized)
	}
}

func TestRequestHistoryRouteDecisionPreservesStructuredSidecarPolicyEvidence(t *testing.T) {
	home := t.TempDir()
	line := `{"requestId":"req_sidecar","provider":"openai","model":"gpt-5","status":200,"routeDecision":{"routeKind":"direct","sidecarPolicy":{"identity":{"status":"accepted","harnessId":"opencode"},"webSearch":{"configured":{"enabled":true,"source":"harness_override"},"resolution":"sidecar_enabled","ran":true},"vision":{"configured":{"enabled":false,"source":"global"},"resolution":"policy_disabled","ran":false}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := requesthistory.CatchUp(home); err != nil {
		t.Fatal(err)
	}
	h := testHandler(t, Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     filepath.Join(home, "config.json"),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/request-history/req_sidecar/route-decision", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, body)
	}
	for _, want := range []string{
		`"sidecarPolicy"`,
		`"status":"accepted"`,
		`"harnessId":"opencode"`,
		`"source":"harness_override"`,
		`"resolution":"sidecar_enabled"`,
		`"resolution":"policy_disabled"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in body=%s", want, body)
		}
	}
}
