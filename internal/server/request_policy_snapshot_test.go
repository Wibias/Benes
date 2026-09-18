package server

import (
	"context"
	"fmt"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/requestpolicy"
)

type serviceTierCaptureProvider struct {
	events []protocol.Event
	err    error
	// onOpen runs while the attempt is opening, so a test can change live settings
	// between request admission and the provider boundary.
	onOpen     func()
	dispatches []providercontract.DispatchRequest
}

func (p *serviceTierCaptureProvider) Open(_ context.Context, dispatch providercontract.DispatchRequest) (EventStream, error) {
	p.dispatches = append(p.dispatches, dispatch)
	if p.onOpen != nil {
		p.onOpen()
	}
	if p.err != nil {
		return nil, p.err
	}
	return &sliceStream{events: append([]protocol.Event(nil), p.events...)}, nil
}

func (p *serviceTierCaptureProvider) admittedTiers() []string {
	out := make([]string, 0, len(p.dispatches))
	for _, dispatch := range p.dispatches {
		out = append(out, dispatch.ConfiguredServiceTier)
	}
	return out
}

// A live settings change while one logical request is in flight must not reach that
// request: the fallback member of a combo keeps the policy admitted at admission, and
// Diagnostics reports the same admitted value the provider boundary uses.
func TestComboAttemptsKeepAdmittedServiceTierAcrossSettingsChange(t *testing.T) {
	defer func() { _ = requestpolicy.Publish(requestpolicy.Policy{}) }()
	publishRequestPolicy(t, requestpolicy.Policy{ServiceTier: "priority"})

	first := &serviceTierCaptureProvider{
		err: fmt.Errorf("Google GenerateContent returned HTTP 503"),
		onOpen: func() {
			publishRequestPolicy(t, requestpolicy.Policy{ServiceTier: "flex"})
		},
	}
	second := &serviceTierCaptureProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google": first, "openai-apikey": second},
		Combos: []Combo{{
			ID: "fast",
			Targets: []ComboTarget{
				{ProviderID: "google", Model: "gemini-flash", Protocol: "google"},
				{ProviderID: "openai-apikey", Model: "gpt-5.4", Protocol: "openai-chat"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postOK(t, h, "/v1/responses", `{"model":"combo/fast","store":false,"stream":true}`, "corr-admitted-tier")

	if got := first.admittedTiers(); len(got) != 1 || got[0] != "priority" {
		t.Fatalf("first attempt admitted tiers=%v", got)
	}
	if got := second.admittedTiers(); len(got) != 1 || got[0] != "priority" {
		t.Fatalf("fallback member adopted the new live setting: admitted tiers=%v", got)
	}
	detail := serviceTierTelemetryDetail(t, h)
	if got := detail["configuredServiceTier"]; got != "priority" {
		t.Fatalf("configuredServiceTier=%#v want the admitted %q", got, "priority")
	}
}

// An explicit client tier stays the requested evidence and still outranks the admitted
// configured tier, while the admitted tier remains visible as its own fact.
func TestAdmittedServiceTierKeepsExplicitClientTierDistinct(t *testing.T) {
	defer func() { _ = requestpolicy.Publish(requestpolicy.Policy{}) }()
	publishRequestPolicy(t, requestpolicy.Policy{ServiceTier: "priority"})

	provider := &serviceTierCaptureProvider{
		events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}},
		onOpen: func() {
			publishRequestPolicy(t, requestpolicy.Policy{ServiceTier: "default"})
		},
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": provider}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postOK(t, h, "/v1/responses", `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true,"service_tier":"flex"}`, "corr-explicit-tier")

	if len(provider.dispatches) != 1 {
		t.Fatalf("opens=%d", len(provider.dispatches))
	}
	dispatch := provider.dispatches[0]
	if dispatch.ConfiguredServiceTier != "priority" {
		t.Fatalf("admitted tier=%q want %q", dispatch.ConfiguredServiceTier, "priority")
	}
	if dispatch.Parsed.Options.ServiceTier == nil || *dispatch.Parsed.Options.ServiceTier != "flex" {
		t.Fatalf("explicit client tier=%#v", dispatch.Parsed.Options.ServiceTier)
	}
	detail := serviceTierTelemetryDetail(t, h)
	if got := detail["requestedServiceTier"]; got != "flex" {
		t.Fatalf("requestedServiceTier=%#v", got)
	}
	if got := detail["configuredServiceTier"]; got != "priority" {
		t.Fatalf("configuredServiceTier=%#v", got)
	}
}

// Settings changes still take effect live: a request admitted after the change carries
// the new policy, so the fix narrows scope to in-flight requests only.
func TestServiceTierAdmissionAffectsOnlyFutureRequests(t *testing.T) {
	defer func() { _ = requestpolicy.Publish(requestpolicy.Policy{}) }()
	publishRequestPolicy(t, requestpolicy.Policy{ServiceTier: "priority"})

	provider := &serviceTierCaptureProvider{
		events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "hello"}, {Type: protocol.EventDone}},
		onOpen: func() {
			publishRequestPolicy(t, requestpolicy.Policy{ServiceTier: "flex"})
		},
	}
	h, err := NewHandler(Options{DataPlaneToken: "local-secret", Providers: map[string]Provider{"openai-apikey": provider}})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	postOK(t, h, "/v1/responses", `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "corr-before-change")
	postOK(t, h, "/v1/responses", `{"model":"openai-apikey/gpt-5.6","store":false,"stream":true}`, "corr-after-change")

	if got := provider.admittedTiers(); len(got) != 2 || got[0] != "priority" || got[1] != "flex" {
		t.Fatalf("admitted tiers=%v want [priority flex]", got)
	}
	detail := serviceTierTelemetryDetail(t, h)
	if got := detail["configuredServiceTier"]; got != "flex" {
		t.Fatalf("latest request configuredServiceTier=%#v want %q", got, "flex")
	}
}
