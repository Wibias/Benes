package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/harnessidentity"
)

func TestParseHarnessIdentityFailClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []string
		id     string
		status harnessIdentityStatus
	}{
		{name: "missing", status: harnessIdentityMissing},
		{name: "accepted", values: []string{"opencode"}, id: "opencode", status: harnessIdentityAccepted},
		{name: "blank", values: []string{"   "}, status: harnessIdentityRejected},
		{name: "unknown", values: []string{"not-a-harness"}, status: harnessIdentityRejected},
		{name: "known but not stampable", values: []string{"pi"}, status: harnessIdentityRejected},
		{name: "duplicate", values: []string{"opencode", "opencode"}, status: harnessIdentityRejected},
		{name: "ambiguous comma", values: []string{"opencode,grok"}, status: harnessIdentityRejected},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := parseHarnessIdentity(test.values)
			if got.ID != test.id || got.Status != test.status {
				t.Fatalf("parseHarnessIdentity(%q)=%#v want id=%q status=%q", test.values, got, test.id, test.status)
			}
		})
	}
}

func TestBindHarnessIdentityUsesTypedContextAndStripsHeader(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req.Header.Set(harnessidentity.Header, "opencode")
	bound := bindHarnessIdentity(req)
	if got := bound.Header.Get(harnessidentity.Header); got != "" {
		t.Fatalf("raw Harness selector leaked after boundary parse: %q", got)
	}
	identity := harnessIdentityFromContext(bound.Context())
	if identity.Status != harnessIdentityAccepted || identity.ID != "opencode" {
		t.Fatalf("typed identity=%#v", identity)
	}

	snapshot := (&handler{}).snapshotForwardHeaders(bound.Header)
	if got := snapshot.Get(harnessidentity.Header); got != "" {
		t.Fatalf("provider forward headers contain Harness selector: %q", got)
	}
}

func TestInvalidHarnessIdentityDoesNotRejectDataPlaneRequest(t *testing.T) {
	t.Parallel()

	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Host = "127.0.0.1"
	req.Header.Set("Authorization", "Bearer local-secret")
	req.Header.Set(harnessidentity.Header, "not-a-harness")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := req.Header.Get(harnessidentity.Header); got != "" {
		t.Fatalf("invalid selector was not stripped: %q", got)
	}
	identity := harnessIdentityFromContext(req.Context())
	if identity.Status != harnessIdentityRejected || identity.ID != "" {
		t.Fatalf("rejected identity=%#v", identity)
	}
}

func TestHarnessIdentityHeaderIsCorsAllowed(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodOptions, "/v1/responses", nil)
	got := allowedRequestHeaders(req, false)
	if !strings.Contains(strings.ToLower(got), strings.ToLower(harnessidentity.Header)) {
		t.Fatalf("CORS allowed headers missing %s: %s", harnessidentity.Header, got)
	}
}
