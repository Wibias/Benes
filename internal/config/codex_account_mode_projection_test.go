package config

import (
	"encoding/json"
	"testing"

	"github.com/Wibias/Benes/internal/providerregistry"
)

func TestProjectProviderSpecsCanonicalOpenAIDefaultsCodexModeToPool(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"openai": providerJSON(t, `{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward"}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if got := projection.Specs[0].CodexAccountMode; got != providerregistry.CodexAccountModePool {
		t.Fatalf("mode=%q want=%q", got, providerregistry.CodexAccountModePool)
	}
}

func TestProjectProviderSpecsCanonicalOpenAIAcceptsExplicitCodexModes(t *testing.T) {
	for _, mode := range []providerregistry.CodexAccountMode{
		providerregistry.CodexAccountModeDirect,
		providerregistry.CodexAccountModePool,
	} {
		t.Run(string(mode), func(t *testing.T) {
			projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
				"openai": providerJSON(t, `{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward","codexAccountMode":"`+string(mode)+`"}`),
			}})
			if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
				t.Fatalf("projection=%#v", projection)
			}
			if got := projection.Specs[0].CodexAccountMode; got != mode {
				t.Fatalf("mode=%q want=%q", got, mode)
			}
		})
	}
}

func TestProjectProviderSpecsRejectsCodexModeOutsideCanonicalOpenAIForwardShape(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		raw      string
		code     string
		field    string
	}{
		{
			name:     "custom provider id",
			provider: "custom",
			raw:      `{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward","codexAccountMode":"pool"}`,
			code:     "unsupported_field",
			field:    "codexAccountMode",
		},
		{
			name:     "key auth",
			provider: "openai",
			raw:      `{"adapter":"openai-responses","baseUrl":"https://api.openai.com/v1","authMode":"key","apiKey":"k","codexAccountMode":"pool"}`,
			code:     "unsupported_field",
			field:    "codexAccountMode",
		},
		{
			name:     "custom forward base",
			provider: "openai",
			raw:      `{"adapter":"openai-responses","baseUrl":"https://example.com","authMode":"forward","codexAccountMode":"pool"}`,
			code:     "unsupported_auth_mode",
			field:    "authMode",
		},
		{
			name:     "invalid mode",
			provider: "openai",
			raw:      `{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward","codexAccountMode":"future"}`,
			code:     "unsupported_field",
			field:    "codexAccountMode",
		},
		{
			name:     "non string mode",
			provider: "openai",
			raw:      `{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward","codexAccountMode":123}`,
			code:     "invalid_field",
			field:    "codexAccountMode",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
				tc.provider: providerJSON(t, tc.raw),
			}})
			if len(projection.Specs) != 0 || len(projection.Skipped) != 1 {
				t.Fatalf("projection=%#v", projection)
			}
			if got := projection.Skipped[0]; got.Code != tc.code || got.Field != tc.field {
				t.Fatalf("skip=%#v want code=%q field=%q", got, tc.code, tc.field)
			}
		})
	}
}

func TestProjectProviderSpecsCustomForwardWithoutCodexModeKeepsCompatibilityModeEmpty(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"custom": providerJSON(t, `{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward"}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if projection.Specs[0].CodexAccountMode != "" {
		t.Fatalf("custom mode=%q", projection.Specs[0].CodexAccountMode)
	}
}