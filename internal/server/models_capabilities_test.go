package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/router"
)

func TestModelsAdvertisesStableEffectiveCapabilities(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{
			"chat":      &fakeProvider{},
			"responses": &fakeProvider{},
			"unknown":   &fakeProvider{},
		},
		CatalogModels: []catalog.Model{
			{
				ID: "chat/plain", Context: catalog.ContextWindow{Tokens: 32000, Source: catalog.ContextCap},
				APITypes: []string{"chat_completions"}, ToolUse: catalog.CapabilityFalse, Streaming: catalog.CapabilityTrue, Reasoning: catalog.CapabilityFalse, Vision: catalog.CapabilityFalse,
				Availability: catalog.Availability{Selectable: true},
			},
			{
				ID: "responses/reasoner", Context: catalog.ContextWindow{Tokens: 64000, Source: catalog.ContextOperator},
				APITypes: []string{"responses", "chat_completions"}, ToolUse: catalog.CapabilityTrue, Streaming: catalog.CapabilityTrue,
				Reasoning: catalog.CapabilityTrue, Vision: catalog.CapabilityTrue, ReasoningEfforts: []string{"low", "high"},
				Availability: catalog.Availability{Selectable: true},
			},
			{
				ID: "unknown/model", Context: catalog.ContextWindow{Tokens: 128000, Source: catalog.ContextConservativeDefault},
				Availability: catalog.Availability{Selectable: true},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]any{}
	for _, row := range body.Data {
		id, _ := row["id"].(string)
		byID[id] = row
	}

	plain := byID["chat/plain"]
	assertStringSlice(t, plain["api_types"], []string{"chat_completions"})
	plainCaps := requireCapabilities(t, plain)
	if plainCaps["context_length"] != float64(32000) || plainCaps["supports_tool_use"] != false || plainCaps["supports_streaming"] != true || plainCaps["supports_reasoning"] != false || plainCaps["supports_vision"] != false {
		t.Fatalf("plain capabilities=%#v", plainCaps)
	}
	assertStringSlice(t, plainCaps["output_modalities"], []string{"text"})
	assertStringSlice(t, plainCaps["input_modalities"], []string{"text"})

	reasoner := byID["responses/reasoner"]
	assertStringSlice(t, reasoner["api_types"], []string{"responses", "chat_completions"})
	reasonCaps := requireCapabilities(t, reasoner)
	if reasonCaps["context_length"] != float64(64000) || reasonCaps["supports_tool_use"] != true || reasonCaps["supports_streaming"] != true || reasonCaps["supports_reasoning"] != true || reasonCaps["supports_vision"] != true {
		t.Fatalf("reasoning capabilities=%#v", reasonCaps)
	}
	assertStringSlice(t, reasonCaps["input_modalities"], []string{"text", "image"})
	assertStringSlice(t, reasonCaps["output_modalities"], []string{"text"})
	assertStringSlice(t, reasonCaps["reasoning_effort"], []string{"low", "high"})

	unknown := byID["unknown/model"]
	if _, ok := unknown["api_types"]; ok {
		t.Fatalf("unknown row fabricated api_types: %#v", unknown)
	}
	unknownCaps := requireCapabilities(t, unknown)
	if unknownCaps["context_length"] != float64(128000) {
		t.Fatalf("unknown context=%#v", unknownCaps)
	}
	assertStringSlice(t, unknownCaps["output_modalities"], []string{"text"})
	for _, key := range []string{"supports_tool_use", "supports_streaming", "supports_reasoning", "supports_vision", "input_modalities", "reasoning_effort"} {
		if _, ok := unknownCaps[key]; ok {
			t.Fatalf("unknown row fabricated %s: %#v", key, unknownCaps)
		}
	}
}

func TestModelsLogicalOpenAIAliasPreservesCapabilitiesWithoutDuplicateIdentity(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{
		"providers":{
			"openai":{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward","defaultAccess":"api"},
			"openai-apikey":{"adapter":"openai-responses","baseUrl":"https://api.openai.com/v1/responses","apiKey":"test-key"}
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{
			"openai":        &fakeProvider{},
			"openai-apikey": &fakeProvider{},
		},
		CatalogModels: []catalog.Model{
			{
				ID: "openai/gpt-5.6", Context: catalog.ContextWindow{Tokens: 128000, Source: catalog.ContextDiscovered},
				APITypes: []string{"responses"}, ToolUse: catalog.CapabilityFalse, Streaming: catalog.CapabilityTrue,
				Availability: catalog.Availability{Selectable: true},
			},
			{
				ID: "openai-apikey/gpt-5.6", Context: catalog.ContextWindow{Tokens: 64000, Source: catalog.ContextCap},
				APITypes: []string{"responses", "chat_completions"}, ToolUse: catalog.CapabilityTrue, Streaming: catalog.CapabilityTrue,
				Reasoning: catalog.CapabilityTrue, Vision: catalog.CapabilityTrue, ReasoningEfforts: []string{"low", "high"},
				Availability: catalog.Availability{Selectable: true},
			},
		},
		ConfigPath: configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("alias projection created duplicate physical identities: %#v", body.Data)
	}
	row := body.Data[0]
	if row["id"] != "openai/gpt-5.6" || row["owned_by"] != "openai" {
		t.Fatalf("row=%#v", row)
	}
	assertStringSlice(t, row["api_types"], []string{"responses", "chat_completions"})
	caps := requireCapabilities(t, row)
	if caps["context_length"] != float64(64000) || caps["supports_tool_use"] != true || caps["supports_streaming"] != true || caps["supports_reasoning"] != true || caps["supports_vision"] != true {
		t.Fatalf("aliased capabilities=%#v", caps)
	}
	assertStringSlice(t, caps["reasoning_effort"], []string{"low", "high"})
	assertStringSlice(t, caps["input_modalities"], []string{"text", "image"})
}

func TestModelsRouteAliasDoesNotDuplicateCapabilityIdentity(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google-antigravity": &fakeProvider{}},
		Aliases: router.AliasTable{
			Providers:       map[string]struct{}{"google-antigravity": {}},
			ProviderByAlias: map[string]string{"agy": "google-antigravity"},
			ModelByProvider: map[string]map[string]string{
				"google-antigravity": {"opus": "claude-opus-5"},
			},
		},
		CatalogModels: []catalog.Model{{
			ID: "google-antigravity/claude-opus-5", Context: catalog.ContextWindow{Tokens: 200000, Source: catalog.ContextDiscovered},
			APITypes: []string{"responses", "chat_completions", "anthropic_messages"}, ToolUse: catalog.CapabilityUnknown, Streaming: catalog.CapabilityUnknown,
			Reasoning: catalog.CapabilityTrue, Vision: catalog.CapabilityTrue, ReasoningEfforts: []string{"low", "medium", "high"},
			Availability: catalog.Availability{Selectable: true},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("route aliases created duplicate model identities: %#v", body.Data)
	}
	row := body.Data[0]
	if row["id"] != "google-antigravity/claude-opus-5" || row["owned_by"] != "google-antigravity" {
		t.Fatalf("row=%#v", row)
	}
	assertStringSlice(t, row["api_types"], []string{"responses", "chat_completions", "anthropic_messages"})
	caps := requireCapabilities(t, row)
	if caps["context_length"] != float64(200000) || caps["supports_reasoning"] != true || caps["supports_vision"] != true {
		t.Fatalf("canonical capabilities changed by route alias: %#v", caps)
	}
	if _, ok := caps["supports_tool_use"]; ok {
		t.Fatalf("alias fabricated tool-use certainty: %#v", caps)
	}
	if _, ok := caps["supports_streaming"]; ok {
		t.Fatalf("alias fabricated streaming certainty: %#v", caps)
	}
}

func requireCapabilities(t *testing.T, row map[string]any) map[string]any {
	t.Helper()
	caps, ok := row["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities=%#v row=%#v", row["capabilities"], row)
	}
	return caps
}

func assertStringSlice(t *testing.T, raw any, want []string) {
	t.Helper()
	gotAny, ok := raw.([]any)
	if !ok || len(gotAny) != len(want) {
		t.Fatalf("slice=%#v want=%v", raw, want)
	}
	for i := range want {
		if gotAny[i] != want[i] {
			t.Fatalf("slice=%#v want=%v", raw, want)
		}
	}
}
