package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/requestpolicy"
)

// The diagnostics request telemetry record is the request/route decision evidence an
// operator inspects to verify that the canonical request policy took effect. The tier
// Benes configured for the request must be visible there and must stay distinct from
// the tier the client asked for and the tier the provider reported back.
func TestRequestTelemetryRecordsConfiguredServiceTier(t *testing.T) {
	defer publishRequestPolicy(t, requestpolicy.Policy{})
	publishRequestPolicy(t, requestpolicy.Policy{ServiceTier: "priority"})

	h := newServiceTierHandler(t, "default")
	postOK(t, h, "/v1/responses", `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "corr-configured-tier")
	detail := serviceTierTelemetryDetail(t, h)

	if got := detail["configuredServiceTier"]; got != "priority" {
		t.Fatalf("configuredServiceTier=%#v want %q; detail=%s", got, "priority", mustJSON(detail))
	}
	if got, ok := detail["requestedServiceTier"]; ok {
		t.Fatalf("client omitted service_tier but telemetry reported requestedServiceTier=%#v", got)
	}
	if got := detail["responseServiceTier"]; got != "default" {
		t.Fatalf("responseServiceTier=%#v want provider-reported %q; detail=%s", got, "default", mustJSON(detail))
	}
}

func TestRequestTelemetryKeepsConfiguredAndRequestedServiceTiersDistinct(t *testing.T) {
	defer publishRequestPolicy(t, requestpolicy.Policy{})
	publishRequestPolicy(t, requestpolicy.Policy{ServiceTier: "priority"})

	h := newServiceTierHandler(t, "flex")
	postOK(t, h, "/v1/responses", `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true,"service_tier":"flex"}`, "corr-client-tier")
	detail := serviceTierTelemetryDetail(t, h)

	if got := detail["requestedServiceTier"]; got != "flex" {
		t.Fatalf("requestedServiceTier=%#v want %q; detail=%s", got, "flex", mustJSON(detail))
	}
	if got := detail["configuredServiceTier"]; got != "priority" {
		t.Fatalf("configuredServiceTier=%#v want %q; detail=%s", got, "priority", mustJSON(detail))
	}
	if got := detail["responseServiceTier"]; got != "flex" {
		t.Fatalf("responseServiceTier=%#v want provider-reported %q; detail=%s", got, "flex", mustJSON(detail))
	}
}

func TestRequestTelemetryOmitsConfiguredServiceTierWhenPolicyUnset(t *testing.T) {
	defer publishRequestPolicy(t, requestpolicy.Policy{})
	publishRequestPolicy(t, requestpolicy.Policy{})

	h := newServiceTierHandler(t, "default")
	postOK(t, h, "/v1/responses", `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "corr-unset-tier")
	detail := serviceTierTelemetryDetail(t, h)

	if got, ok := detail["configuredServiceTier"]; ok {
		t.Fatalf("unset policy reported configuredServiceTier=%#v; detail=%s", got, mustJSON(detail))
	}
	if got, ok := detail["requestedServiceTier"]; ok {
		t.Fatalf("client omitted service_tier but telemetry reported requestedServiceTier=%#v", got)
	}
}

func publishRequestPolicy(t *testing.T, policy requestpolicy.Policy) {
	t.Helper()
	if err := requestpolicy.Publish(policy); err != nil {
		t.Fatalf("publish request policy: %v", err)
	}
}

func newServiceTierHandler(t *testing.T, reportedTier string) http.Handler {
	t.Helper()
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": &fakeProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 4, OutputTokens: 2, ServiceTier: reportedTier}},
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	return attachHandlerClose(t, h)
}

func serviceTierTelemetryDetail(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	row := latestResponses(getDiagnosticsList(t, h, "/api/diagnostics/requests"))
	if row.RequestID == "" {
		t.Fatal("no Responses request telemetry row")
	}
	rr := diagnosticsGET(t, h, "/api/diagnostics/requests/"+row.RequestID)
	if rr.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", rr.Code, rr.Body.String())
	}
	var detail map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	return detail
}
