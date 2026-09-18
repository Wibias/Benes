package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/harnessboard"
	"github.com/Wibias/Benes/internal/harnesspolicy"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/router"
)

type visionRuntimeProviderFunc func(context.Context, providercontract.DispatchRequest) (EventStream, error)

func (f visionRuntimeProviderFunc) Open(ctx context.Context, request providercontract.DispatchRequest) (EventStream, error) {
	return f(ctx, request)
}

type visionRuntimeStream struct {
	events []protocol.Event
	index  int
}

func (s *visionRuntimeStream) Next() (protocol.Event, error) {
	if s == nil || s.index >= len(s.events) {
		return protocol.Event{}, io.EOF
	}
	event := s.events[s.index]
	s.index++
	return event, nil
}

func (s *visionRuntimeStream) Close() error { return nil }

func newVisionRuntimeHandler(t *testing.T, globalEnabled bool, providers map[string]Provider, models []catalog.Model) *handler {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	root := map[string]any{
		"visionSidecar": map[string]any{
			"model":   "vision/describer",
			"backend": "vision_describe",
			"enabled": globalEnabled,
		},
	}
	raw, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	h := &handler{providers: providers, catalogModels: models, configPath: configPath}
	t.Cleanup(func() { forgetHarnessSidecarRuntime(h) })
	return h
}

func visionImageDispatch(model string) providercontract.DispatchRequest {
	return providercontract.DispatchRequest{Parsed: protocol.ParsedRequest{
		Source:          protocol.RequestSourceChatCompletions,
		ModelID:         model,
		UpstreamModelID: model,
		Raw:             json.RawMessage(`{"model":"` + model + `","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,YQ=="}}]}]}`),
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser,
			Content: []protocol.ContentPart{
				{Type: protocol.ContentText, Text: "what is shown?"},
				{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,YQ=="},
			},
		}}},
	}}
}

func visionRuntimeHasImage(request protocol.ParsedRequest) bool {
	for _, message := range request.Context.Messages {
		for _, part := range message.Content {
			if part.Type == protocol.ContentImage || strings.TrimSpace(part.ImageURL) != "" {
				return true
			}
		}
	}
	return false
}

func visionRuntimeDescription(request protocol.ParsedRequest) string {
	for _, message := range request.Context.Messages {
		for _, part := range message.Content {
			if part.Type == protocol.ContentText && strings.Contains(part.Text, "[Benes image description]") {
				return part.Text
			}
		}
	}
	return ""
}

func visionDescriptionProvider(calls *int, description string, openErr error) Provider {
	return visionRuntimeProviderFunc(func(_ context.Context, request providercontract.DispatchRequest) (EventStream, error) {
		(*calls)++
		if !visionRuntimeHasImage(request.Parsed) {
			return nil, errors.New("vision describer did not receive the original image")
		}
		if openErr != nil {
			return nil, openErr
		}
		return &visionRuntimeStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: description},
			{Type: protocol.EventDone},
		}}, nil
	})
}

func acceptedVisionHarnessContext(id string) context.Context {
	return context.WithValue(context.Background(), harnessIdentityContextKey{}, harnessRequestIdentity{ID: id, Status: harnessIdentityAccepted})
}

func setVisionOverride(h *handler, id string, activation harnesspolicy.Activation) {
	harnessSidecarRuntimeFor(h).Replace(map[string]harnessboard.Settings{
		id: {Sidecars: &harnesspolicy.Overrides{Vision: &activation}},
	})
}

func TestVisionRuntimeNativeAndUnknownCandidatesPreserveOriginalImage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		vision catalog.CapabilityState
	}{
		{name: "native", vision: catalog.CapabilityTrue},
		{name: "unknown", vision: catalog.CapabilityUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sidecarCalls := 0
			targetCalls := 0
			var got protocol.ParsedRequest
			providers := map[string]Provider{
				"target": visionRuntimeProviderFunc(func(_ context.Context, request providercontract.DispatchRequest) (EventStream, error) {
					targetCalls++
					got = protocol.CloneParsedRequest(request.Parsed)
					return nil, nil
				}),
				"vision": visionDescriptionProvider(&sidecarCalls, "described", nil),
			}
			h := newVisionRuntimeHandler(t, true, providers, []catalog.Model{
				{ID: "target/model", Vision: tc.vision},
				{ID: "vision/describer", Vision: catalog.CapabilityTrue},
			})
			resolved := h.resolveProvider(router.Route{Provider: "target", Model: "model"})
			if _, err := resolved.Provider.Open(context.Background(), visionImageDispatch("target/model")); err != nil {
				t.Fatal(err)
			}
			if targetCalls != 1 || sidecarCalls != 0 {
				t.Fatalf("targetCalls=%d sidecarCalls=%d", targetCalls, sidecarCalls)
			}
			if !visionRuntimeHasImage(got) {
				t.Fatal("candidate without proven text-only capability lost the original image")
			}
			if len(got.Raw) == 0 {
				t.Fatal("native/unknown pass-through unexpectedly discarded preserved raw request")
			}
		})
	}
}

func TestVisionRuntimeTextOnlyCandidateTransformsBeforeTargetDispatch(t *testing.T) {
	sidecarCalls := 0
	targetCalls := 0
	var got protocol.ParsedRequest
	providers := map[string]Provider{
		"target": visionRuntimeProviderFunc(func(_ context.Context, request providercontract.DispatchRequest) (EventStream, error) {
			targetCalls++
			got = protocol.CloneParsedRequest(request.Parsed)
			return nil, nil
		}),
		"vision": visionDescriptionProvider(&sidecarCalls, "a tiny red square", nil),
	}
	h := newVisionRuntimeHandler(t, true, providers, []catalog.Model{
		{ID: "target/model", Vision: catalog.CapabilityFalse},
		{ID: "vision/describer", Vision: catalog.CapabilityTrue},
	})
	resolved := h.resolveProvider(router.Route{Provider: "target", Model: "model"})
	if _, err := resolved.Provider.Open(context.Background(), visionImageDispatch("target/model")); err != nil {
		t.Fatal(err)
	}
	if targetCalls != 1 || sidecarCalls != 1 {
		t.Fatalf("targetCalls=%d sidecarCalls=%d", targetCalls, sidecarCalls)
	}
	if visionRuntimeHasImage(got) {
		t.Fatal("text-only candidate received a raw image after successful sidecar transform")
	}
	if len(got.Raw) != 0 {
		t.Fatalf("transformed dispatch retained stale raw request: %s", string(got.Raw))
	}
	if text := visionRuntimeDescription(got); !strings.Contains(text, "a tiny red square") {
		t.Fatalf("description=%q", text)
	}
}

func TestVisionRuntimeDisabledOrFailedSidecarNeverCallsTextOnlyTarget(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		targetCalls := 0
		sidecarCalls := 0
		providers := map[string]Provider{
			"target": visionRuntimeProviderFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
				targetCalls++
				return nil, nil
			}),
			"vision": visionDescriptionProvider(&sidecarCalls, "unused", nil),
		}
		h := newVisionRuntimeHandler(t, false, providers, []catalog.Model{
			{ID: "target/model", Vision: catalog.CapabilityFalse},
			{ID: "vision/describer", Vision: catalog.CapabilityTrue},
		})
		resolved := h.resolveProvider(router.Route{Provider: "target", Model: "model"})
		_, err := resolved.Provider.Open(context.Background(), visionImageDispatch("target/model"))
		var policyErr *visionPreDispatchError
		if !errors.As(err, &policyErr) {
			t.Fatalf("err=%T %v, want typed vision pre-dispatch error", err, err)
		}
		if targetCalls != 0 || sidecarCalls != 0 {
			t.Fatalf("targetCalls=%d sidecarCalls=%d", targetCalls, sidecarCalls)
		}
	})

	t.Run("transform failure", func(t *testing.T) {
		targetCalls := 0
		sidecarCalls := 0
		providers := map[string]Provider{
			"target": visionRuntimeProviderFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
				targetCalls++
				return nil, nil
			}),
			"vision": visionDescriptionProvider(&sidecarCalls, "", errors.New("vision backend unavailable")),
		}
		h := newVisionRuntimeHandler(t, true, providers, []catalog.Model{
			{ID: "target/model", Vision: catalog.CapabilityFalse},
			{ID: "vision/describer", Vision: catalog.CapabilityTrue},
		})
		resolved := h.resolveProvider(router.Route{Provider: "target", Model: "model"})
		_, err := resolved.Provider.Open(context.Background(), visionImageDispatch("target/model"))
		var policyErr *visionPreDispatchError
		if !errors.As(err, &policyErr) {
			t.Fatalf("err=%T %v, want typed vision pre-dispatch error", err, err)
		}
		if targetCalls != 0 || sidecarCalls != 1 {
			t.Fatalf("targetCalls=%d sidecarCalls=%d", targetCalls, sidecarCalls)
		}
	})
}

func TestVisionRuntimeComboAttemptsAlwaysStartFromImmutableOriginal(t *testing.T) {
	t.Run("text-only then native", func(t *testing.T) {
		sidecarCalls := 0
		var first, second protocol.ParsedRequest
		providers := map[string]Provider{
			"text": visionRuntimeProviderFunc(func(_ context.Context, request providercontract.DispatchRequest) (EventStream, error) {
				first = protocol.CloneParsedRequest(request.Parsed)
				return nil, errors.New("HTTP 503 unavailable")
			}),
			"native": visionRuntimeProviderFunc(func(_ context.Context, request providercontract.DispatchRequest) (EventStream, error) {
				second = protocol.CloneParsedRequest(request.Parsed)
				return &visionRuntimeStream{events: []protocol.Event{{Type: protocol.EventDone}}}, nil
			}),
			"vision": visionDescriptionProvider(&sidecarCalls, "first attempt description", nil),
		}
		h := newVisionRuntimeHandler(t, true, providers, []catalog.Model{
			{ID: "text/model-a", Vision: catalog.CapabilityFalse},
			{ID: "native/model-b", Vision: catalog.CapabilityTrue},
			{ID: "vision/describer", Vision: catalog.CapabilityTrue},
		})
		walker := h.comboWalker(Combo{ID: "fallback", Targets: []ComboTarget{
			{ProviderID: "text", Model: "model-a"},
			{ProviderID: "native", Model: "model-b"},
		}})
		stream, err := walker.Open(context.Background(), visionImageDispatch("combo/fallback"))
		if err != nil {
			t.Fatal(err)
		}
		if stream != nil {
			_ = stream.Close()
		}
		if visionRuntimeHasImage(first) || visionRuntimeDescription(first) == "" {
			t.Fatalf("first attempt was not transformed: %#v", first.Context.Messages)
		}
		if !visionRuntimeHasImage(second) || visionRuntimeDescription(second) != "" {
			t.Fatalf("native fallback did not receive immutable original: %#v", second.Context.Messages)
		}
		if sidecarCalls != 1 {
			t.Fatalf("sidecarCalls=%d", sidecarCalls)
		}
	})

	t.Run("native then text-only", func(t *testing.T) {
		sidecarCalls := 0
		var first, second protocol.ParsedRequest
		providers := map[string]Provider{
			"native": visionRuntimeProviderFunc(func(_ context.Context, request providercontract.DispatchRequest) (EventStream, error) {
				first = protocol.CloneParsedRequest(request.Parsed)
				return &visionRuntimeStream{events: []protocol.Event{{Type: protocol.EventError, HTTPStatus: 503, Message: "unavailable"}}}, nil
			}),
			"text": visionRuntimeProviderFunc(func(_ context.Context, request providercontract.DispatchRequest) (EventStream, error) {
				second = protocol.CloneParsedRequest(request.Parsed)
				return &visionRuntimeStream{events: []protocol.Event{{Type: protocol.EventDone}}}, nil
			}),
			"vision": visionDescriptionProvider(&sidecarCalls, "fallback description", nil),
		}
		h := newVisionRuntimeHandler(t, true, providers, []catalog.Model{
			{ID: "native/model-a", Vision: catalog.CapabilityTrue},
			{ID: "text/model-b", Vision: catalog.CapabilityFalse},
			{ID: "vision/describer", Vision: catalog.CapabilityTrue},
		})
		walker := h.comboWalker(Combo{ID: "fallback", Targets: []ComboTarget{
			{ProviderID: "native", Model: "model-a"},
			{ProviderID: "text", Model: "model-b"},
		}})
		stream, err := walker.Open(context.Background(), visionImageDispatch("combo/fallback"))
		if err != nil {
			t.Fatal(err)
		}
		if stream != nil {
			_ = stream.Close()
		}
		if !visionRuntimeHasImage(first) {
			t.Fatalf("native first attempt lost original image: %#v", first.Context.Messages)
		}
		if visionRuntimeHasImage(second) || visionRuntimeDescription(second) == "" {
			t.Fatalf("text-only fallback was not independently transformed: %#v", second.Context.Messages)
		}
		if sidecarCalls != 1 {
			t.Fatalf("sidecarCalls=%d", sidecarCalls)
		}
	})
}

func TestVisionRuntimeHarnessOverrideChangesTextOnlyCandidateBehavior(t *testing.T) {
	for _, tc := range []struct {
		name        string
		override    harnesspolicy.Activation
		wantTarget  int
		wantSidecar int
		wantErr     bool
	}{
		{name: "on", override: harnesspolicy.ActivationEnabled, wantTarget: 1, wantSidecar: 1},
		{name: "off", override: harnesspolicy.ActivationDisabled, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			targetCalls := 0
			sidecarCalls := 0
			providers := map[string]Provider{
				"target": visionRuntimeProviderFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
					targetCalls++
					return nil, nil
				}),
				"vision": visionDescriptionProvider(&sidecarCalls, "override description", nil),
			}
			h := newVisionRuntimeHandler(t, true, providers, []catalog.Model{
				{ID: "target/model", Vision: catalog.CapabilityFalse},
				{ID: "vision/describer", Vision: catalog.CapabilityTrue},
			})
			setVisionOverride(h, "opencode", tc.override)
			resolved := h.resolveProvider(router.Route{Provider: "target", Model: "model"})
			_, err := resolved.Provider.Open(acceptedVisionHarnessContext("opencode"), visionImageDispatch("target/model"))
			if tc.wantErr && err == nil {
				t.Fatal("expected override-disabled request to fail before dispatch")
			}
			if !tc.wantErr && err != nil {
				t.Fatal(err)
			}
			if targetCalls != tc.wantTarget || sidecarCalls != tc.wantSidecar {
				t.Fatalf("targetCalls=%d sidecarCalls=%d", targetCalls, sidecarCalls)
			}
		})
	}
}
