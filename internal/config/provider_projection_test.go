package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providerregistry"
)

func TestProjectProviderSpecsProjectsChatCapabilityFields(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"openrouter": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://openrouter.ai/api/v1",
			"apiKey":"k",
			"chatServiceTier":true,
			"supportsServiceTier":false,
			"modelSupportsServiceTier":{"openai/gpt-5.6-sol":true},
			"noStructuredOutputModels":["gpt-oss"]
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	cap := projection.Specs[0].Capability
	if cap.ChatServiceTier != true || cap.SupportsServiceTier == nil || *cap.SupportsServiceTier {
		t.Fatalf("tier=%#v", cap)
	}
	if cap.ModelSupportsServiceTier["openai/gpt-5.6-sol"] != true || len(cap.NoStructuredOutputModels) != 1 || cap.NoStructuredOutputModels[0] != "gpt-oss" {
		t.Fatalf("models=%#v", cap)
	}
}

func TestProjectProviderSpecsAcceptsCredentialRefWithoutPlaintextKey(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"openai-apikey": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://api.openai.com/v1",
			"credentialRef":{"id":"openai-apikey","source":"secure-store"}
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	spec := projection.Specs[0]
	if spec.APIKey != "" || spec.CredentialRef.ID != "openai-apikey" || string(spec.CredentialRef.Source) != "secure-store" {
		t.Fatalf("spec=%#v", spec)
	}
}

func TestProjectProviderSpecsProjectsCloudCodeAssistGoogleMode(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"google-antigravity": providerJSON(t, `{
			"adapter":"google",
			"googleMode":"cloud-code-assist",
			"baseUrl":"https://daily-cloudcode-pa.googleapis.com"
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if projection.Specs[0].Protocol != providerregistry.ProtocolGoogleAntigravity || projection.Specs[0].Endpoint != "https://daily-cloudcode-pa.googleapis.com" {
		t.Fatalf("spec=%#v", projection.Specs[0])
	}
}

func TestProjectProviderSpecsProjectsDirectGoogleAndVertex(t *testing.T) {
	studio := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"google": providerJSON(t, `{
			"adapter":"google",
			"baseUrl":"https://generativelanguage.googleapis.com",
			"apiKey":"gk"
		}`),
	}})
	if len(studio.Skipped) != 0 || len(studio.Specs) != 1 || studio.Specs[0].Protocol != providerregistry.ProtocolGoogle {
		t.Fatalf("studio=%#v", studio)
	}
	vertex := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"vertex": providerJSON(t, `{
			"adapter":"google",
			"googleMode":"vertex",
			"baseUrl":"https://aiplatform.googleapis.com",
			"project":"p",
			"location":"us-central1",
			"apiKey":"vk"
		}`),
	}})
	if len(vertex.Skipped) != 0 || vertex.Specs[0].Protocol != providerregistry.ProtocolGoogleVertex || vertex.Specs[0].Project != "p" || vertex.Specs[0].Location != "us-central1" {
		t.Fatalf("vertex=%#v", vertex)
	}
}

func TestProjectProviderSpecsProjectsKiroOAuthWithoutAPIKey(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"kiro": providerJSON(t, `{
			"adapter":"kiro",
			"baseUrl":"https://runtime.us-east-1.kiro.dev",
			"authMode":"oauth"
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	spec := projection.Specs[0]
	if spec.Protocol != providerregistry.ProtocolKiro || spec.AuthMode != providerregistry.AuthModeOAuth || spec.APIKey != "" {
		t.Fatalf("spec=%#v", spec)
	}
}

func TestProjectProviderSpecsProjectsCursorAdapter(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"cursor": providerJSON(t, `{
			"adapter":"cursor",
			"baseUrl":"https://api2.cursor.sh",
			"apiKey":"tok"
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 || projection.Specs[0].Protocol != providerregistry.ProtocolCursor {
		t.Fatalf("projection=%#v", projection)
	}
}

func TestProjectProviderSpecsProjectsCloudCodeAssistProject(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"google-antigravity": providerJSON(t, `{
			"adapter":"google",
			"googleMode":"cloud-code-assist",
			"baseUrl":"https://daily-cloudcode-pa.googleapis.com",
			"project":"cca-proj"
		}`),
	}})
	if len(projection.Skipped) != 0 || projection.Specs[0].Project != "cca-proj" {
		t.Fatalf("projection=%#v", projection)
	}
}

func TestProjectProviderSpecsProjectsHostedWebSearchCapability(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"opencode-zen": providerJSON(t, `{
			"adapter":"openai-responses",
			"baseUrl":"https://opencode.ai/zen/v1",
			"apiKey":"k",
			"hostedWebSearch":true,
			"webSearchModels":["deepseek-v4-flash"]
		}`),
	}})
	if len(projection.Skipped) != 0 || !projection.Specs[0].Capability.HostedWebSearch {
		t.Fatalf("projection=%#v", projection)
	}
	if got := projection.Specs[0].Capability.WebSearchModels; len(got) != 1 || got[0] != "deepseek-v4-flash" {
		t.Fatalf("models=%v", got)
	}
}

func TestProjectProviderSpecsAcceptsAPIKeyPoolWithoutPrimaryKey(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"pooled": providerJSON(t, `{
			"adapter":"openai-responses",
			"baseUrl":"https://api.openai.com/v1",
			"apiKeyPool":[{"id":"a","key":"sk-a"},{"id":"b","key":"sk-b"}]
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if projection.Specs[0].APIKey != "sk-a" || len(projection.Specs[0].APIKeyPool) != 2 {
		t.Fatalf("spec=%#v", projection.Specs[0])
	}
}

func TestProjectProviderSpecsProjectsSupportedOpenAIWires(t *testing.T) {
	disk := DiskConfig{Providers: map[string]json.RawMessage{
		"responses": providerJSON(t, `{
			"adapter":"openai-responses",
			"baseUrl":"https://gateway.example/v1/responses/",
			"apiKey":"responses-key",
			"authMode":"key",
			"allowPrivateNetwork":true,
			"apiKeyPool":[{"id":"active","key":"responses-key"}]
		}`),
		"chat": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://api.openai.com/v1/chat/completions/",
			"apiKey":"chat-key",
			"preserveReasoningContentModels":["deepseek-r1"],
			"requiresReasoningPlaceholderModels":[]
		}`),
	}}

	projection := ProjectProviderSpecs(disk)
	if len(projection.Skipped) != 0 {
		t.Fatalf("skipped=%#v", projection.Skipped)
	}
	if len(projection.Specs) != 2 {
		t.Fatalf("specs=%#v", projection.Specs)
	}

	chat := projection.Specs[0]
	if chat.ID != "chat" || chat.Protocol != providerregistry.ProtocolOpenAIChat {
		t.Fatalf("chat=%#v", chat)
	}
	if chat.Endpoint != "https://api.openai.com/v1/chat/completions" || chat.APIKey != "chat-key" {
		t.Fatalf("chat=%#v", chat)
	}
	if !chat.Chat.NativeOpenAI {
		t.Fatalf("chat NativeOpenAI=false: %#v", chat.Chat)
	}
	if !reflect.DeepEqual(chat.Chat.PreserveReasoningContentModels, []string{"deepseek-r1"}) {
		t.Fatalf("preserve=%#v", chat.Chat.PreserveReasoningContentModels)
	}
	if chat.Chat.RequiresReasoningPlaceholderModels == nil || len(chat.Chat.RequiresReasoningPlaceholderModels) != 0 {
		t.Fatalf("explicit placeholder opt-out lost: %#v", chat.Chat.RequiresReasoningPlaceholderModels)
	}

	responses := projection.Specs[1]
	if responses.ID != "responses" || responses.Protocol != providerregistry.ProtocolOpenAIResponses {
		t.Fatalf("responses=%#v", responses)
	}
	if responses.Endpoint != "https://gateway.example/v1/responses" || responses.APIKey != "responses-key" {
		t.Fatalf("responses=%#v", responses)
	}
	if !responses.DestinationPolicy.AllowPrivateNetwork {
		t.Fatalf("private-network opt-in was not projected: %#v", responses.DestinationPolicy)
	}
}

func TestProjectProviderSpecsNormalizesBaseURLsLikeExistingAdapters(t *testing.T) {
	disk := DiskConfig{Providers: map[string]json.RawMessage{
		"chat":      providerJSON(t, `{"adapter":"openai-chat","baseUrl":"https://example.com/v1/","apiKey":"k"}`),
		"responses": providerJSON(t, `{"adapter":"openai-responses","baseUrl":"https://example.com/v1/","apiKey":"k"}`),
	}}
	projection := ProjectProviderSpecs(disk)
	if len(projection.Skipped) != 0 || len(projection.Specs) != 2 {
		t.Fatalf("projection=%#v", projection)
	}
	if projection.Specs[0].Endpoint != "https://example.com/v1/chat/completions" {
		t.Fatalf("chat endpoint=%q", projection.Specs[0].Endpoint)
	}
	if projection.Specs[1].Endpoint != "https://example.com/v1/responses" {
		t.Fatalf("responses endpoint=%q", projection.Specs[1].Endpoint)
	}
}

func TestProjectProviderSpecsAllowsNoOpPersistedFields(t *testing.T) {
	disk := DiskConfig{Providers: map[string]json.RawMessage{
		"generic": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://example.com/v1",
			"apiKey":"k",
			"disabled":false,
			"keyOptional":false,
			"headers":{},
			"modelAdapters":{},
			"upstreamHttpVersion":"auto",
			"defaultModel":"model-a",
			"models":["model-a"],
			"fetchModels":false,
			"modelCosts":{"model-a":{"input":1}},
			"apiKeyPool":[]
		}`),
	}}
	projection := ProjectProviderSpecs(disk)
	if len(projection.Specs) != 1 || len(projection.Skipped) != 0 {
		t.Fatalf("projection=%#v", projection)
	}
}

func TestProjectProviderSpecsSkipsDisabledBeforeMigrationChecks(t *testing.T) {
	disk := DiskConfig{Providers: map[string]json.RawMessage{
		"parked": providerJSON(t, `{"disabled":true,"adapter":"future-wire","futureWireFlag":true}`),
	}}
	projection := ProjectProviderSpecs(disk)
	if len(projection.Specs) != 0 || len(projection.Skipped) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if projection.Skipped[0] != (ProviderProjectionSkip{ID: "parked", Code: "disabled"}) {
		t.Fatalf("skip=%#v", projection.Skipped[0])
	}
}

func TestProjectProviderSpecsFailsClosedPerProviderForUnsupportedSemantics(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		code  string
		field string
	}{
		{"oauth", `{"adapter":"openai-chat","baseUrl":"https://example.com/v1","apiKey":"k","authMode":"oauth"}`, "unsupported_auth_mode", "authMode"},
		{"forward", `{"adapter":"openai-responses","baseUrl":"https://example.com/v1","authMode":"forward"}`, "unsupported_auth_mode", "authMode"},
		{"custom headers", `{"adapter":"openai-chat","baseUrl":"https://example.com/v1","apiKey":"k","headers":{"x-test":"value"}}`, "unsupported_field", "headers"},
		{"model adapters", `{"adapter":"openai-chat","baseUrl":"https://example.com/v1","apiKey":"k","modelAdapters":{"m":"openai-responses"}}`, "unsupported_field", "modelAdapters"},
		{"custom responses path", `{"adapter":"openai-responses","baseUrl":"https://example.com","apiKey":"k","responsesPath":"/responses"}`, "unsupported_field", "responsesPath"},
		{"forced http2", `{"adapter":"openai-chat","baseUrl":"https://example.com/v1","apiKey":"k","upstreamHttpVersion":"h2"}`, "unsupported_field", "upstreamHttpVersion"},
		{"key optional", `{"adapter":"openai-chat","baseUrl":"https://example.com/v1","apiKey":"k","keyOptional":true}`, "unsupported_field", "keyOptional"},
		{"unknown behavior", `{"adapter":"openai-chat","baseUrl":"https://example.com/v1","apiKey":"k","futureWireFlag":true}`, "unsupported_field", "futureWireFlag"},
		{"unsupported adapter", `{"adapter":"future-wire","baseUrl":"https://example.com","apiKey":"k"}`, "unsupported_adapter", "adapter"},
		{"missing credential", `{"adapter":"openai-chat","baseUrl":"https://example.com/v1"}`, "missing_credential", "apiKey"},
	}

	providers := make(map[string]json.RawMessage, len(tests)+1)
	for _, test := range tests {
		providers[test.name] = providerJSON(t, test.raw)
	}
	providers["good"] = providerJSON(t, `{"adapter":"openai-chat","baseUrl":"https://example.com/v1","apiKey":"good-key"}`)

	projection := ProjectProviderSpecs(DiskConfig{Providers: providers})
	if len(projection.Specs) != 1 || projection.Specs[0].ID != "good" {
		t.Fatalf("supported provider was collateral damage: %#v", projection.Specs)
	}
	if len(projection.Skipped) != len(tests) {
		t.Fatalf("skipped=%#v", projection.Skipped)
	}
	byID := make(map[string]ProviderProjectionSkip, len(projection.Skipped))
	for _, skip := range projection.Skipped {
		byID[skip.ID] = skip
	}
	for _, test := range tests {
		skip := byID[test.name]
		if skip.Code != test.code || skip.Field != test.field {
			t.Fatalf("%s skip=%#v", test.name, skip)
		}
	}
}

func TestProjectProviderSpecsRejectsMalformedRelevantFieldsWithoutLeakingSecrets(t *testing.T) {
	secret := "TOP-SECRET-PROVIDER-KEY"
	disk := DiskConfig{Providers: map[string]json.RawMessage{
		"bad-list":    providerJSON(t, `{"adapter":"openai-chat","baseUrl":"https://example.com/v1","apiKey":"`+secret+`","preserveReasoningContentModels":"all"}`),
		"bad-private": providerJSON(t, `{"adapter":"openai-chat","baseUrl":"https://example.com/v1","apiKey":"k","allowPrivateNetwork":"yes"}`),
		"bad-key":     providerJSON(t, `{"adapter":"openai-chat","baseUrl":"https://example.com/v1","apiKey":123}`),
	}}

	projection := ProjectProviderSpecs(disk)
	if len(projection.Specs) != 0 || len(projection.Skipped) != 3 {
		t.Fatalf("projection=%#v", projection)
	}
	for _, skip := range projection.Skipped {
		if skip.Code != "invalid_field" {
			t.Fatalf("skip=%#v", skip)
		}
	}
	if got := fmt.Sprintf("%#v", projection.Skipped); containsString(got, secret) {
		t.Fatalf("secret leaked in projection diagnostics: %s", got)
	}
}

func providerJSON(t *testing.T, raw string) json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &object); err != nil || object == nil {
		t.Fatalf("invalid test provider JSON: %v", err)
	}
	return json.RawMessage(raw)
}

func containsString(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && stringContains(haystack, needle)
}

func stringContains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
func TestProjectProviderSpecsProjectsVercelGatewayRouting(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"vercel-ai-gateway": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://ai-gateway.vercel.sh/v1",
			"apiKey":"k",
			"gatewayRouting":{"only":["anthropic"],"sort":"ttft"},
			"modelGatewayRouting":{"anthropic/claude-sonnet-5":{"order":["vertex","anthropic"]}}
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	settings := projection.Specs[0].GatewayRouting
	if strings.Join(settings.Provider.Only, ",") != "anthropic" || settings.Provider.Sort != "ttft" {
		t.Fatalf("provider=%#v", settings.Provider)
	}
	override := settings.Resolve("anthropic/claude-sonnet-5")
	if len(override.Only) != 0 || override.Sort != "" || strings.Join(override.Order, ",") != "vertex,anthropic" {
		t.Fatalf("override=%#v", override)
	}
}

func TestProjectProviderSpecsRejectsInvalidGatewayRouting(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"bad-sort":       providerJSON(t, `{"adapter":"openai-chat","baseUrl":"https://ai-gateway.vercel.sh/v1","apiKey":"k","gatewayRouting":{"sort":"fast"}}`),
		"lookalike":      providerJSON(t, `{"adapter":"openai-chat","baseUrl":"https://example.com/v1","apiKey":"k","gatewayRouting":{"only":["anthropic"]}}`),
		"google-routing": providerJSON(t, `{"adapter":"google","baseUrl":"https://generativelanguage.googleapis.com","apiKey":"gk","gatewayRouting":{"sort":"cost"}}`),
	}})
	if len(projection.Specs) != 1 || projection.Specs[0].ID != "lookalike" {
		t.Fatalf("specs=%#v skipped=%#v", projection.Specs, projection.Skipped)
	}
	byID := map[string]ProviderProjectionSkip{}
	for _, skip := range projection.Skipped {
		byID[skip.ID] = skip
	}
	if byID["bad-sort"].Code != "invalid_field" || byID["bad-sort"].Field != "gatewayRouting" {
		t.Fatalf("bad-sort=%#v", byID["bad-sort"])
	}
	if byID["google-routing"].Code != "unsupported_field" || byID["google-routing"].Field != "gatewayRouting" {
		t.Fatalf("google=%#v", byID["google-routing"])
	}
	if projection.Specs[0].GatewayRouting.Provider.Only[0] != "anthropic" {
		t.Fatalf("lookalike must keep requested policy for later destination guard: %#v", projection.Specs[0].GatewayRouting)
	}
}

func TestProjectProviderSpecsKeepsDashboardOnlyFields(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"openai-apikey": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://api.openai.com/v1",
			"apiKey":"k",
			"note":"dashboard",
			"liveModels":true,
			"selectedModels":["gpt-4o"],
			"apiKeyTransport":"bearer",
			"requestPacing":{"enabled":true,"minIntervalMs":250,"requestsPerMinute":30},
			"contextWindow":128000,
			"modelContextWindows":{"gpt-4o":128000}
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if projection.Specs[0].RequestPacing.Milliseconds() != 2000 {
		t.Fatalf("pacing=%s", projection.Specs[0].RequestPacing)
	}
}

func TestProjectProviderSpecsKeepsModelPresetMarker(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"openrouter": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://openrouter.ai/api/v1",
			"apiKey":"k",
			"selectedModels":["openai/gpt-5.6-sol"],
			"modelPreset":{"mode":"preset","appliedVersion":1}
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
}

func TestProjectProviderSpecsSeparatesXAIOAuthFromAPIKeyTransport(t *testing.T) {
	oauth := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"xai": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://api.x.ai/v1",
			"authMode":"oauth",
			"apiKey":"sk-should-not-reach-proxy"
		}`),
	}})
	if len(oauth.Skipped) != 0 || len(oauth.Specs) != 1 {
		t.Fatalf("oauth projection=%#v", oauth)
	}
	spec := oauth.Specs[0]
	if spec.AuthMode != providerregistry.AuthModeOAuth ||
		spec.Endpoint != "https://cli-chat-proxy.grok.com/v1/chat/completions" ||
		spec.APIKey != "" ||
		len(spec.APIKeyPool) != 0 ||
		spec.CredentialRef.ID != "" {
		t.Fatalf("oauth spec=%#v", spec)
	}

	key := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"xai": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://api.x.ai/v1",
			"apiKey":"sk-xai-live"
		}`),
	}})
	if len(key.Skipped) != 0 || len(key.Specs) != 1 {
		t.Fatalf("key projection=%#v", key)
	}
	keySpec := key.Specs[0]
	if keySpec.AuthMode != providerregistry.AuthModeKey ||
		keySpec.Endpoint != "https://api.x.ai/v1/chat/completions" ||
		keySpec.APIKey != "sk-xai-live" {
		t.Fatalf("key spec=%#v", keySpec)
	}

	storedKey := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"xai": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://api.x.ai/v1",
			"credentialRef":{"id":"xai","source":"secure-store"}
		}`),
	}})
	if len(storedKey.Skipped) != 0 || len(storedKey.Specs) != 1 {
		t.Fatalf("stored-key projection=%#v", storedKey)
	}
	if storedKey.Specs[0].Endpoint != "https://api.x.ai/v1/chat/completions" || storedKey.Specs[0].AuthMode != providerregistry.AuthModeKey {
		t.Fatalf("stored-key spec=%#v", storedKey.Specs[0])
	}
}

func TestProjectProviderSpecsAcceptsTransientRetryOn5xxForKeyChat(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"gateway": providerJSON(t, `{
			"adapter":"openai-chat",
			"baseUrl":"https://gateway.example/v1",
			"apiKey":"k",
			"transientRetryOn5xx":{"enabled":true,"attempts":2}
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	got := projection.Specs[0].Transient5xx
	if !got.Enabled || got.Attempts != 2 {
		t.Fatalf("retry=%#v", got)
	}
}

func TestProjectProviderSpecsRejectsTransientRetryOn5xxForGoogle(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"google": providerJSON(t, `{
			"adapter":"google",
			"baseUrl":"https://generativelanguage.googleapis.com",
			"apiKey":"k",
			"transientRetryOn5xx":{"enabled":true,"attempts":2}
		}`),
	}})
	if len(projection.Specs) != 0 || len(projection.Skipped) != 1 || projection.Skipped[0].Code != "unsupported_field" {
		t.Fatalf("projection=%#v", projection)
	}
}

func TestProjectProviderSpecsProjectsStructuredOutputEvidence(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"google": providerJSON(t, `{
			"adapter":"google",
			"baseUrl":"https://generativelanguage.googleapis.com",
			"apiKey":"gk",
			"supportsStructuredOutput":false,
			"modelSupportsStructuredOutput":{"gemini-3.7-flash":true},
			"noStructuredOutputModels":["legacy"]
		}`),
	}})
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	cap := projection.Specs[0].Capability
	if cap.SupportsStructuredOutput == nil || *cap.SupportsStructuredOutput {
		t.Fatalf("provider structured default=%#v", cap.SupportsStructuredOutput)
	}
	if !cap.ModelSupportsStructuredOutput["gemini-3.7-flash"] {
		t.Fatalf("model structured support=%#v", cap.ModelSupportsStructuredOutput)
	}
	if len(cap.NoStructuredOutputModels) != 1 || cap.NoStructuredOutputModels[0] != "legacy" {
		t.Fatalf("structured deny=%#v", cap.NoStructuredOutputModels)
	}
}

