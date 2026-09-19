package bootstrap

import (
	"encoding/json"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/providerregistry"
)

func TestProjectCatalogModelsCarriesConfiguredCapabilityMetadata(t *testing.T) {
	provider := json.RawMessage(`{
		"adapter":"openai-responses",
		"baseUrl":"https://example.invalid/v1/responses",
		"models":["reasoner","plain"],
		"modelContextWindows":{"reasoner":200000,"plain":32000},
		"modelInputModalities":{"reasoner":["text","image"],"plain":["text"]},
		"modelReasoningEfforts":{"reasoner":["low","high"],"plain":[]}
	}`)
	root := json.RawMessage(`{
		"providers":{"custom":` + string(provider) + `},
		"providerContextCaps":{"custom":64000},
		"preserveExactReasoningRungs":{"custom":true},
		"customModels":[{
			"id":"custom-record",
			"provider":"custom",
			"modelId":"custom-only",
			"contextWindow":50000,
			"inputModalities":["text"]
		}]
	}`)
	disk := config.DiskConfig{
		Providers: map[string]json.RawMessage{"custom": provider},
		Raw:       root,
	}
	models := projectCatalogModels(disk, []providerregistry.Spec{{
		ID:       "custom",
		Protocol: providerregistry.ProtocolOpenAIResponses,
		Endpoint: "https://example.invalid/v1/responses",
	}})

	reasoner := requireProjectedModel(t, models, "custom/reasoner")
	if reasoner.Context.Tokens != 64000 || reasoner.Context.Source != catalog.ContextCap {
		t.Fatalf("reasoner context=%#v", reasoner.Context)
	}
	if reasoner.Vision != catalog.CapabilityTrue || reasoner.Reasoning != catalog.CapabilityTrue {
		t.Fatalf("reasoner capabilities vision=%q reasoning=%q", reasoner.Vision, reasoner.Reasoning)
	}
	if len(reasoner.ReasoningEfforts) != 2 || reasoner.ReasoningEfforts[0] != "low" || reasoner.ReasoningEfforts[1] != "high" {
		t.Fatalf("reasoner efforts=%v", reasoner.ReasoningEfforts)
	}
	if len(reasoner.APITypes) != 3 || reasoner.ToolUse != catalog.CapabilityUnknown || reasoner.Streaming != catalog.CapabilityUnknown {
		t.Fatalf("reasoner runtime contract api=%v tool=%q streaming=%q", reasoner.APITypes, reasoner.ToolUse, reasoner.Streaming)
	}

	plain := requireProjectedModel(t, models, "custom/plain")
	if plain.Vision != catalog.CapabilityFalse {
		t.Fatalf("plain vision=%q", plain.Vision)
	}
	if plain.Reasoning != catalog.CapabilityFalse || len(plain.ReasoningEfforts) != 0 {
		t.Fatalf("plain reasoning=%q efforts=%v", plain.Reasoning, plain.ReasoningEfforts)
	}

	custom := requireProjectedModel(t, models, "custom/custom-only")
	if custom.Context.Tokens != 50000 || custom.Vision != catalog.CapabilityFalse {
		t.Fatalf("custom context=%#v vision=%q", custom.Context, custom.Vision)
	}
	if custom.Reasoning != catalog.CapabilityUnknown {
		t.Fatalf("custom reasoning=%q", custom.Reasoning)
	}
}

func requireProjectedModel(t *testing.T, models []catalog.Model, id string) catalog.Model {
	t.Helper()
	for _, model := range models {
		if model.ID == id {
			return model
		}
	}
	t.Fatalf("model %q not found in %#v", id, models)
	return catalog.Model{}
}

func TestProjectCatalogModelsUsesCuratedOpenCodeGoContextEvidence(t *testing.T) {
	provider := json.RawMessage(`{
		"adapter":"openai-chat",
		"baseUrl":"https://opencode.ai/zen/go/v1",
		"models":["qwen3.8-max","unknown-model"]
	}`)
	disk := config.DiskConfig{
		Providers: map[string]json.RawMessage{"opencode-go": provider},
		Raw:       json.RawMessage(`{"providers":{"opencode-go":` + string(provider) + `}}`),
	}
	models := projectCatalogModels(disk, []providerregistry.Spec{{
		ID:       "opencode-go",
		Protocol: providerregistry.ProtocolOpenAIChat,
		Endpoint: "https://opencode.ai/zen/go/v1",
	}})

	qwen := requireProjectedModel(t, models, "opencode-go/qwen3.8-max")
	if qwen.Context.Tokens != 1_000_000 || string(qwen.Context.Source) != "curated_metadata" {
		t.Fatalf("qwen context=%#v", qwen.Context)
	}
	requireContextEvidence(t, qwen.Context, "models.dev", "6303a062a3782391f3147e9db1100ee93139abbb", "models/alibaba/qwen3.8-max.toml")

	unknown := requireProjectedModel(t, models, "opencode-go/unknown-model")
	if unknown.Context.Tokens != catalog.ConservativeContextWindow || unknown.Context.Source != catalog.ContextConservativeDefault {
		t.Fatalf("unknown context=%#v", unknown.Context)
	}
	requireNoContextEvidence(t, unknown.Context)
}

func requireContextEvidence(t *testing.T, ctx catalog.ContextWindow, source, revision, path string) {
	t.Helper()
	raw, err := json.Marshal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Evidence *struct {
			Source   string `json:"source"`
			Revision string `json:"revision"`
			Path     string `json:"path"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Evidence == nil || body.Evidence.Source != source || body.Evidence.Revision != revision || body.Evidence.Path != path {
		t.Fatalf("context evidence=%s", raw)
	}
}

func requireNoContextEvidence(t *testing.T, ctx catalog.ContextWindow) {
	t.Helper()
	raw, err := json.Marshal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["evidence"]; ok {
		t.Fatalf("unexpected context evidence=%s", raw)
	}
}

