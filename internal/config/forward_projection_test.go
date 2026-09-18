package config

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Wibias/Benes/internal/providerregistry"
)

func TestProjectProviderSpecsProjectsCanonicalForwardResponses(t *testing.T) {
	for _, baseURL := range []string{
		"https://chatgpt.com/backend-api/codex",
		"https://chatgpt.com/backend-api/codex/",
	} {
		projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
			"openai": providerJSON(t, `{
				"adapter":"openai-responses",
				"baseUrl":"`+baseURL+`",
				"authMode":"forward"
			}`),
		}})
		if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
			t.Fatalf("baseUrl=%q projection=%#v", baseURL, projection)
		}
		spec := projection.Specs[0]
		if spec.ID != "openai" || spec.Protocol != providerregistry.ProtocolOpenAIResponses || spec.AuthMode != providerregistry.AuthModeForward {
			t.Fatalf("spec=%#v", spec)
		}
		if spec.CodexAccountMode != providerregistry.CodexAccountModePool {
			t.Fatalf("codex mode=%q", spec.CodexAccountMode)
		}
		if spec.Endpoint != "https://chatgpt.com/backend-api/codex/responses" {
			t.Fatalf("endpoint=%q", spec.Endpoint)
		}
		if spec.APIKey != "" {
			t.Fatalf("forward spec retained API key: %#v", spec)
		}
		if spec.DestinationPolicy.AllowPrivateNetwork {
			t.Fatalf("forward spec enabled private network: %#v", spec.DestinationPolicy)
		}
	}
}

func TestProjectProviderSpecsForwardModeFailsClosedOutsideCanonicalShape(t *testing.T) {
	credentialSentinel := "FORWARD-KEY-SENTINEL"
	tests := []struct {
		name  string
		raw   string
		code  string
		field string
	}{
		{
			name:  "arbitrary responses host",
			raw:   `{"adapter":"openai-responses","baseUrl":"https://example.com/backend-api/codex","authMode":"forward"}`,
			code:  "unsupported_auth_mode",
			field: "authMode",
		},
		{
			name:  "wrong canonical path",
			raw:   `{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex/responses","authMode":"forward"}`,
			code:  "unsupported_auth_mode",
			field: "authMode",
		},
		{
			name:  "chat wire",
			raw:   `{"adapter":"openai-chat","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward"}`,
			code:  "unsupported_auth_mode",
			field: "authMode",
		},
		{
			name:  "provider api key",
			raw:   `{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward","apiKey":"` + credentialSentinel + `"}`,
			code:  "unsupported_field",
			field: "apiKey",
		},
		{
			name:  "private network opt in",
			raw:   `{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward","allowPrivateNetwork":true}`,
			code:  "unsupported_field",
			field: "allowPrivateNetwork",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
				"openai": providerJSON(t, tc.raw),
			}})
			if len(projection.Specs) != 0 || len(projection.Skipped) != 1 {
				t.Fatalf("projection=%#v", projection)
			}
			skip := projection.Skipped[0]
			if skip.Code != tc.code || skip.Field != tc.field {
				t.Fatalf("skip=%#v want code=%q field=%q", skip, tc.code, tc.field)
			}
			if got := fmt.Sprintf("%#v", projection); containsString(got, credentialSentinel) {
				t.Fatalf("credential leaked in projection diagnostics: %s", got)
			}
		})
	}
}

func TestProjectProviderSpecsKeyAuthStillProjectsExplicitKeyMode(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"keyed": providerJSON(t, `{"adapter":"openai-responses","baseUrl":"https://api.openai.com/v1","authMode":"key","apiKey":"k"}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if projection.Specs[0].AuthMode != providerregistry.AuthModeKey {
		t.Fatalf("auth mode=%q", projection.Specs[0].AuthMode)
	}
}