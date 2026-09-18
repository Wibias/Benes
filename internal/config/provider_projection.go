package config

import (
	"bytes"
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/gatewayrouting"
	"github.com/Wibias/Benes/internal/providerregistry"
	"github.com/Wibias/Benes/internal/providers/xaicapability"
	"github.com/Wibias/Benes/internal/transport"
)

const canonicalForwardBaseURL = "https://chatgpt.com/backend-api/codex"

type ProviderProjection struct {
	Specs   []providerregistry.Spec
	Skipped []ProviderProjectionSkip
}

type ProviderProjectionSkip struct {
	ID    string
	Code  string
	Field string
}

var projectionKnownFields = map[string]struct{}{
	"defaultAccess":                      {},
	"adapter":                            {},
	"baseUrl":                            {},
	"requestPacingMs":                    {},
	"requestPacing":                      {},
	"note":                               {},
	"liveModels":                         {},
	"selectedModels":                     {},
	"modelPreset":                        {},
	"newModelPolicy":                     {},
	"apiKeyTransport":                    {},
	"contextWindow":                      {},
	"modelContextWindows":                {},
	"apiKey":                             {},
	"apiKeyPool":                         {},
	"credentialRef":                      {},
	"authMode":                           {},
	"codexAccountMode":                   {},
	"allowPrivateNetwork":                {},
	"disabled":                           {},
	"keyOptional":                        {},
	"headers":                            {},
	"modelAdapters":                      {},
	"upstreamHttpVersion":                {},
	"responsesPath":                      {},
	"preserveReasoningContentModels":     {},
	"requiresReasoningPlaceholderModels": {},
	"reasoningSplitModels":               {},
	"thinkingToggleModels":               {},
	"modelDefaultReasoningEfforts":       {},
	"modelReasoningEffortMap":            {},
	"defaultModel":                       {},
	"models":                             {},
	"fetchModels":                        {},
	"modelCosts":                         {},
	"googleMode":                         {},
	"project":                            {},
	"location":                           {},
	"directGeminiWireRenames":            {},
	"supportsServiceTier":                {},
	"modelSupportsServiceTier":           {},
	"chatServiceTier":                    {},
	"noStructuredOutputModels":           {},
	"hostedWebSearch":                    {},
	"webSearchModels":                    {},
	"gatewayRouting":                     {},
	"modelGatewayRouting":                {},
	"transientRetryOn5xx":                {},
	"maxUpstreamBodyBytes":               {},
	"alias":                              {},
	"modelAliases":                       {},
	"modelReasoningEfforts":              {},
}

func ProjectProviderSpecs(config DiskConfig) ProviderProjection {
	ids := make([]string, 0, len(config.Providers))
	for id := range config.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	projection := ProviderProjection{
		Specs:   make([]providerregistry.Spec, 0, len(ids)),
		Skipped: make([]ProviderProjectionSkip, 0),
	}
	for _, id := range ids {
		spec, skip := projectProviderSpec(id, config.Providers[id])
		if skip != nil {
			projection.Skipped = append(projection.Skipped, *skip)
			continue
		}
		projection.Specs = append(projection.Specs, spec)
	}
	return projection
}

func projectProviderSpec(id string, raw json.RawMessage) (providerregistry.Spec, *ProviderProjectionSkip) {
	var provider map[string]json.RawMessage
	if json.Unmarshal(raw, &provider) != nil || provider == nil {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "")
	}

	disabled, present, valid := optionalBool(provider, "disabled")
	if !valid {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "disabled")
	}
	if present && disabled {
		return providerregistry.Spec{}, projectionSkip(id, "disabled", "")
	}

	adapter, present, valid := optionalString(provider, "adapter")
	if !valid || !present || strings.TrimSpace(adapter) == "" {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "adapter")
	}
	googleMode, _, googleModeValid := optionalString(provider, "googleMode")
	if !googleModeValid {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "googleMode")
	}
	var protocol providerregistry.Protocol
	switch adapter {
	case string(providerregistry.ProtocolOpenAIChat):
		protocol = providerregistry.ProtocolOpenAIChat
	case string(providerregistry.ProtocolOpenAIResponses):
		protocol = providerregistry.ProtocolOpenAIResponses
	case "anthropic", string(providerregistry.ProtocolAnthropicMessages):
		protocol = providerregistry.ProtocolAnthropicMessages
	case "google", string(providerregistry.ProtocolGoogleVertex), string(providerregistry.ProtocolGoogleAntigravity):
		switch strings.TrimSpace(googleMode) {
		case "", "ai-studio":
			if adapter == string(providerregistry.ProtocolGoogleAntigravity) {
				protocol = providerregistry.ProtocolGoogleAntigravity
			} else {
				protocol = providerregistry.ProtocolGoogle
			}
		case "vertex":
			protocol = providerregistry.ProtocolGoogleVertex
		case "cloud-code-assist":
			protocol = providerregistry.ProtocolGoogleAntigravity
		default:
			return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "googleMode")
		}
	case string(providerregistry.ProtocolCursor):
		protocol = providerregistry.ProtocolCursor
	case string(providerregistry.ProtocolKiro):
		protocol = providerregistry.ProtocolKiro
	default:
		return providerregistry.Spec{}, projectionSkip(id, "unsupported_adapter", "adapter")
	}

	authMode := providerregistry.AuthModeKey
	if value, exists, ok := optionalString(provider, "authMode"); !ok {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "authMode")
	} else if exists {
		switch value {
		case string(providerregistry.AuthModeKey):
			authMode = providerregistry.AuthModeKey
		case string(providerregistry.AuthModeForward):
			authMode = providerregistry.AuthModeForward
		case string(providerregistry.AuthModeOAuth):
			if id != "xai" && id != "kiro" {
				return providerregistry.Spec{}, projectionSkip(id, "unsupported_auth_mode", "authMode")
			}
			authMode = providerregistry.AuthModeOAuth
		default:
			return providerregistry.Spec{}, projectionSkip(id, "unsupported_auth_mode", "authMode")
		}
	}

	codexModeValue, codexModePresent, codexModeValid := optionalString(provider, "codexAccountMode")
	if !codexModeValid {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "codexAccountMode")
	}
	openCodeSessionOverride, headerSkip := projectOpenCodeGoSessionOverride(id, protocol, provider)
	if headerSkip != nil {
		return providerregistry.Spec{}, headerSkip
	}

	if skip := rejectUnsupportedKnownSemantics(id, provider); skip != nil {
		return providerregistry.Spec{}, skip
	}
	if field := firstUnknownProjectionField(provider); field != "" {
		return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", field)
	}
	maxUpstreamBodyBytes, bodyLimitPresent, bodyLimitValid := projectMaxUpstreamBodyBytes(provider)
	if !bodyLimitValid {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "maxUpstreamBodyBytes")
	}
	if bodyLimitPresent && protocol != providerregistry.ProtocolOpenAIResponses {
		return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", "maxUpstreamBodyBytes")
	}

	baseURL, present, valid := optionalString(provider, "baseUrl")
	if !valid || !present || strings.TrimSpace(baseURL) == "" {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "baseUrl")
	}
	baseURL = strings.TrimSpace(baseURL)
	if !validProviderBaseURL(baseURL) {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "baseUrl")
	}
	if authMode == providerregistry.AuthModeForward {
		if protocol != providerregistry.ProtocolOpenAIResponses || !canonicalForwardProviderBaseURL(baseURL) {
			return providerregistry.Spec{}, projectionSkip(id, "unsupported_auth_mode", "authMode")
		}
	}

	apiKey, apiKeyPresent, valid := optionalString(provider, "apiKey")
	if !valid {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "apiKey")
	}
	keyPool, keyPoolOK := parseAPIKeyPool(provider["apiKeyPool"])
	if !keyPoolOK {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "apiKeyPool")
	}
	if protocol == providerregistry.ProtocolAnthropicMessages && len(keyPool) > 0 {
		return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", "apiKeyPool")
	}
	credRef, credRefOK := parseCredentialRef(provider["credentialRef"])
	if !credRefOK {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "credentialRef")
	}
	apiKeyTransport := providerregistry.APIKeyTransport("")
	if protocol == providerregistry.ProtocolAnthropicMessages {
		if value, exists, ok := optionalString(provider, "apiKeyTransport"); !ok {
			return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "apiKeyTransport")
		} else if exists {
			switch value {
			case string(providerregistry.APIKeyTransportXAPIKey):
				apiKeyTransport = providerregistry.APIKeyTransportXAPIKey
			case string(providerregistry.APIKeyTransportBearer):
				apiKeyTransport = providerregistry.APIKeyTransportBearer
			default:
				return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "apiKeyTransport")
			}
		} else {
			apiKeyTransport = providerregistry.APIKeyTransportXAPIKey
		}
	}
	if authMode == providerregistry.AuthModeForward {
		if apiKeyPresent && apiKey != "" {
			return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", "apiKey")
		}
		if len(keyPool) > 0 {
			return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", "apiKeyPool")
		}
		if credRef.ID != "" || credRef.Source != "" {
			return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", "credentialRef")
		}
		apiKey = ""
	}
	hasKeyMaterial := (apiKeyPresent && strings.TrimSpace(apiKey) != "" && apiKey == strings.TrimSpace(apiKey)) || len(keyPool) > 0
	hasCredentialRef := credRef.ID != "" || credRef.Source != ""
	xaiOAuth := id == "xai" && xaicapability.UsesOAuthProxy(string(authMode), hasKeyMaterial, hasCredentialRef)
	kiroOAuth := protocol == providerregistry.ProtocolKiro && authMode == providerregistry.AuthModeOAuth
	if authMode != providerregistry.AuthModeForward && !xaiOAuth && !kiroOAuth && protocol != providerregistry.ProtocolGoogleAntigravity && protocol != providerregistry.ProtocolGoogleVertex && (!apiKeyPresent || strings.TrimSpace(apiKey) == "" || apiKey != strings.TrimSpace(apiKey)) && len(keyPool) == 0 && credRef.ID == "" && credRef.Source == "" {
		return providerregistry.Spec{}, projectionSkip(id, "missing_credential", "apiKey")
	}
	if protocol != providerregistry.ProtocolAnthropicMessages && strings.TrimSpace(apiKey) != "" && len(keyPool) == 0 {
		keyPool = []providerregistry.APIKeySlot{{ID: "primary", Key: apiKey}}
	}
	if strings.TrimSpace(apiKey) == "" && len(keyPool) > 0 {
		apiKey = keyPool[0].Key
	}

	allowPrivate, _, valid := optionalBool(provider, "allowPrivateNetwork")
	if !valid {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "allowPrivateNetwork")
	}
	if authMode == providerregistry.AuthModeForward && allowPrivate {
		return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", "allowPrivateNetwork")
	}

	codexAccountMode := providerregistry.CodexAccountMode("")
	canonicalOpenAIForward := id == "openai" &&
		protocol == providerregistry.ProtocolOpenAIResponses &&
		authMode == providerregistry.AuthModeForward &&
		canonicalForwardProviderBaseURL(baseURL)
	if codexModePresent {
		if !canonicalOpenAIForward {
			return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", "codexAccountMode")
		}
		switch codexModeValue {
		case string(providerregistry.CodexAccountModeDirect):
			codexAccountMode = providerregistry.CodexAccountModeDirect
		case string(providerregistry.CodexAccountModePool):
			codexAccountMode = providerregistry.CodexAccountModePool
		default:
			return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", "codexAccountMode")
		}
	} else if canonicalOpenAIForward {
		codexAccountMode = providerregistry.CodexAccountModePool
	}

	spec := providerregistry.Spec{
		ID:                      id,
		Protocol:                protocol,
		AuthMode:                authMode,
		CodexAccountMode:        codexAccountMode,
		APIKey:                  apiKey,
		APIKeyTransport:         apiKeyTransport,
		APIKeyPool:              keyPool,
		CredentialRef:           credRef,
		MaxUpstreamBodyBytes:    maxUpstreamBodyBytes,
		OpenCodeSessionOverride: openCodeSessionOverride,
		DestinationPolicy: transport.DestinationPolicy{
			AllowPrivateNetwork: allowPrivate,
		},
	}

	switch protocol {
	case providerregistry.ProtocolOpenAIChat:
		preserve, valid := optionalStringList(provider, "preserveReasoningContentModels")
		if !valid {
			return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "preserveReasoningContentModels")
		}
		requires, valid := optionalStringList(provider, "requiresReasoningPlaceholderModels")
		if !valid {
			return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "requiresReasoningPlaceholderModels")
		}
		split, valid := optionalStringList(provider, "reasoningSplitModels")
		if !valid {
			return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "reasoningSplitModels")
		}
		if _, valid := optionalStringList(provider, "thinkingToggleModels"); !valid {
			return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "thinkingToggleModels")
		}
		spec.Endpoint = openAIChatEndpoint(baseURL)
		spec.Chat = providerregistry.ChatOptions{
			NativeOpenAI:                       nativeOpenAIChatBaseURL(baseURL),
			PreserveReasoningContentModels:     preserve,
			RequiresReasoningPlaceholderModels: requires,
			ReasoningSplitModels:               split,
		}
		if xaiOAuth {
			applyXAIOauthTransport(&spec, xaicapability.OAuthChatEndpoint())
		}

	case providerregistry.ProtocolOpenAIResponses:
		if _, exists := provider["preserveReasoningContentModels"]; exists {
			return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", "preserveReasoningContentModels")
		}
		if _, exists := provider["requiresReasoningPlaceholderModels"]; exists {
			return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", "requiresReasoningPlaceholderModels")
		}
		if _, exists := provider["reasoningSplitModels"]; exists {
			return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", "reasoningSplitModels")
		}
		if authMode == providerregistry.AuthModeForward {
			spec.Endpoint = canonicalForwardBaseURL + "/responses"
		} else {
			spec.Endpoint = openAIResponsesEndpoint(baseURL)
			if xaiOAuth {
				applyXAIOauthTransport(&spec, xaicapability.OAuthResponsesEndpoint())
			}
		}
	case providerregistry.ProtocolAnthropicMessages:
		spec.Endpoint = anthropicMessagesEndpoint(baseURL)
	case providerregistry.ProtocolGoogleAntigravity:
		spec.Endpoint = strings.TrimRight(baseURL, "/")
		if project, present, ok := optionalString(provider, "project"); !ok {
			return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "project")
		} else if present {
			spec.Project = strings.TrimSpace(project)
		}
	case providerregistry.ProtocolGoogle:
		spec.Endpoint = strings.TrimRight(baseURL, "/")
	case providerregistry.ProtocolGoogleVertex:
		spec.Endpoint = strings.TrimRight(baseURL, "/")
		if project, present, ok := optionalString(provider, "project"); !ok {
			return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "project")
		} else if present {
			spec.Project = strings.TrimSpace(project)
		}
		if location, present, ok := optionalString(provider, "location"); !ok {
			return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "location")
		} else if present {
			spec.Location = strings.TrimSpace(location)
		}
	case providerregistry.ProtocolCursor:
		spec.Endpoint = strings.TrimRight(baseURL, "/")
	case providerregistry.ProtocolKiro:
		spec.Endpoint = strings.TrimRight(baseURL, "/")
	}

	if raw, exists := provider["supportsServiceTier"]; exists {
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "supportsServiceTier")
		}
		spec.Capability.SupportsServiceTier = &value
	}
	if raw, exists := provider["chatServiceTier"]; exists {
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "chatServiceTier")
		}
		spec.Capability.ChatServiceTier = value
	}
	if raw, exists := provider["modelSupportsServiceTier"]; exists {
		var values map[string]bool
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "modelSupportsServiceTier")
		}
		spec.Capability.ModelSupportsServiceTier = values
	}
	if names, valid := optionalStringList(provider, "noStructuredOutputModels"); !valid {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "noStructuredOutputModels")
	} else {
		spec.Capability.NoStructuredOutputModels = names
	}
	if hosted, present, valid := optionalBool(provider, "hostedWebSearch"); !valid {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "hostedWebSearch")
	} else if present {
		spec.Capability.HostedWebSearch = hosted
	}
	if models, valid := optionalStringList(provider, "webSearchModels"); !valid {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "webSearchModels")
	} else {
		spec.Capability.WebSearchModels = models
	}

	routing, field, err := parseGatewayRouting(provider)
	if err != nil {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", field)
	}
	if protocol != providerregistry.ProtocolOpenAIChat && !routing.Empty() {
		skipField := "gatewayRouting"
		if routing.Provider.Empty() {
			skipField = "modelGatewayRouting"
		}
		return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", skipField)
	}
	spec.GatewayRouting = routing

	basePacing, modelPacing, pacingOK := projectProviderPacing(provider)
	if !pacingOK {
		field := "requestPacing"
		if _, exists := provider[field]; !exists {
			field = "requestPacingMs"
		}
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", field)
	}
	spec.RequestPacing = basePacing
	spec.ModelRequestPacing = modelPacing

	retry, retryOK := parseTransientRetryOn5xx(provider["transientRetryOn5xx"])
	if !retryOK {
		return providerregistry.Spec{}, projectionSkip(id, "invalid_field", "transientRetryOn5xx")
	}
	if retry.Enabled {
		keyAuth := authMode == providerregistry.AuthModeKey
		supported := protocol == providerregistry.ProtocolOpenAIChat || protocol == providerregistry.ProtocolOpenAIResponses || protocol == providerregistry.ProtocolAnthropicMessages
		if !keyAuth || !supported {
			return providerregistry.Spec{}, projectionSkip(id, "unsupported_field", "transientRetryOn5xx")
		}
	}
	spec.Transient5xx = retry

	return spec, nil
}

func applyXAIOauthTransport(spec *providerregistry.Spec, endpoint string) {
	if spec == nil {
		return
	}
	spec.AuthMode = providerregistry.AuthModeOAuth
	spec.Endpoint = endpoint
	spec.APIKey = ""
	spec.APIKeyPool = nil
	spec.CredentialRef = credentials.Ref{}
}

func projectOpenCodeGoSessionOverride(id string, protocol providerregistry.Protocol, provider map[string]json.RawMessage) (string, *ProviderProjectionSkip) {
	raw, exists := provider["headers"]
	if !exists {
		return "", nil
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return "", projectionSkip(id, "invalid_field", "headers")
	}
	if len(values) == 0 {
		return "", nil
	}
	if id != "opencode-go" || protocol != providerregistry.ProtocolOpenAIChat {
		return "", projectionSkip(id, "unsupported_field", "headers")
	}

	var override string
	found := false
	for name, rawValue := range values {
		if !strings.EqualFold(name, "x-opencode-session") {
			return "", projectionSkip(id, "unsupported_field", "headers")
		}
		if found {
			return "", projectionSkip(id, "invalid_field", "headers")
		}
		if json.Unmarshal(rawValue, &override) != nil || !validConfiguredHeaderValue(override) {
			return "", projectionSkip(id, "invalid_field", "headers")
		}
		found = true
	}
	return override, nil
}

func validConfiguredHeaderValue(value string) bool {
	if value == "" || value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func rejectUnsupportedKnownSemantics(id string, provider map[string]json.RawMessage) *ProviderProjectionSkip {
	if value, present, valid := optionalBool(provider, "keyOptional"); !valid {
		return projectionSkip(id, "invalid_field", "keyOptional")
	} else if present && value {
		return projectionSkip(id, "unsupported_field", "keyOptional")
	}

	if raw, exists := provider["modelAdapters"]; exists {
		var values map[string]json.RawMessage
		if json.Unmarshal(raw, &values) != nil || values == nil {
			return projectionSkip(id, "invalid_field", "modelAdapters")
		}
		if len(values) > 0 {
			return projectionSkip(id, "unsupported_field", "modelAdapters")
		}
	}

	if value, present, valid := optionalString(provider, "upstreamHttpVersion"); !valid {
		return projectionSkip(id, "invalid_field", "upstreamHttpVersion")
	} else if present && value != "auto" {
		return projectionSkip(id, "unsupported_field", "upstreamHttpVersion")
	}

	if raw, exists := provider["responsesPath"]; exists {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return projectionSkip(id, "invalid_field", "responsesPath")
		}
		return projectionSkip(id, "unsupported_field", "responsesPath")
	}
	return nil
}

func firstUnknownProjectionField(provider map[string]json.RawMessage) string {
	unknown := make([]string, 0)
	for field := range provider {
		if _, known := projectionKnownFields[field]; !known {
			unknown = append(unknown, field)
		}
	}
	if len(unknown) == 0 {
		return ""
	}
	sort.Strings(unknown)
	return unknown[0]
}

func parseTransientRetryOn5xx(raw json.RawMessage) (transport.Transient5xxPolicy, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return transport.Transient5xxPolicy{}, true
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var cfg struct {
		Enabled  *bool `json:"enabled"`
		Attempts *int  `json:"attempts"`
	}
	if err := dec.Decode(&cfg); err != nil {
		return transport.Transient5xxPolicy{}, false
	}
	if cfg.Enabled == nil || !*cfg.Enabled {
		return transport.Transient5xxPolicy{}, true
	}
	attempts := 2
	if cfg.Attempts != nil {
		attempts = *cfg.Attempts
	}
	if attempts < 1 || attempts > 4 {
		return transport.Transient5xxPolicy{}, false
	}
	return transport.Transient5xxPolicy{Enabled: true, Attempts: attempts}, true
}

func optionalString(provider map[string]json.RawMessage, field string) (string, bool, bool) {
	raw, exists := provider[field]
	if !exists {
		return "", false, true
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", true, false
	}
	return value, true, true
}

func optionalBool(provider map[string]json.RawMessage, field string) (bool, bool, bool) {
	raw, exists := provider[field]
	if !exists {
		return false, false, true
	}
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return false, true, false
	}
	return value, true, true
}

func parseAPIKeyPool(raw json.RawMessage) ([]providerregistry.APIKeySlot, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, true
	}
	var entries []struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if json.Unmarshal(trimmed, &entries) != nil {
		return nil, false
	}
	out := make([]providerregistry.APIKeySlot, 0, len(entries))
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if key == "" {
			continue
		}
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			id = key
		}
		out = append(out, providerregistry.APIKeySlot{ID: id, Key: key})
	}
	return out, true
}

func parseCredentialRef(raw json.RawMessage) (credentials.Ref, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return credentials.Ref{}, true
	}
	var ref credentials.Ref
	if json.Unmarshal(trimmed, &ref) != nil {
		return credentials.Ref{}, false
	}
	ref.ID = strings.TrimSpace(ref.ID)
	ref.Source = credentials.Source(strings.TrimSpace(string(ref.Source)))
	ref.Hint = strings.TrimSpace(ref.Hint)
	if ref.ID == "" && ref.Source == "" && ref.Hint == "" {
		return credentials.Ref{}, true
	}
	return ref, true
}

func optionalStringList(provider map[string]json.RawMessage, field string) ([]string, bool) {
	raw, exists := provider[field]
	if !exists {
		return nil, true
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return nil, false
	}
	return values, true
}

func validProviderBaseURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func canonicalForwardProviderBaseURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "chatgpt.com") {
		return false
	}
	if parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return strings.TrimRight(parsed.EscapedPath(), "/") == "/backend-api/codex"
}

func openAIChatEndpoint(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	trimmed = strings.TrimSuffix(trimmed, "/chat/completions")
	return strings.TrimRight(trimmed, "/") + "/chat/completions"
}

func openAIResponsesEndpoint(baseURL string) string {
	trimmed := strings.TrimRight(baseURL, "/")
	trimmed = strings.TrimSuffix(trimmed, "/responses")
	trimmed = strings.TrimRight(trimmed, "/")
	trimmed = strings.TrimSuffix(trimmed, "/v1")
	return strings.TrimRight(trimmed, "/") + "/v1/responses"
}

func anthropicMessagesEndpoint(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(trimmed, "/messages") {
		return trimmed
	}
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed + "/messages"
	}
	return trimmed + "/v1/messages"
}

func nativeOpenAIChatBaseURL(baseURL string) bool {
	parsed, err := url.Parse(baseURL)
	return err == nil && strings.EqualFold(parsed.Hostname(), "api.openai.com")
}

func parseGatewayRouting(provider map[string]json.RawMessage) (gatewayrouting.Settings, string, error) {
	var settings gatewayrouting.Settings
	if raw, exists := provider["gatewayRouting"]; exists {
		policy, err := gatewayrouting.DecodePolicy(raw)
		if err != nil {
			return gatewayrouting.Settings{}, "gatewayRouting", err
		}
		settings.Provider = policy
	}
	if raw, exists := provider["modelGatewayRouting"]; exists {
		models, err := gatewayrouting.DecodeModelPolicies(raw)
		if err != nil {
			return gatewayrouting.Settings{}, "modelGatewayRouting", err
		}
		settings.Models = models
	}
	if err := settings.Validate(); err != nil {
		field := "gatewayRouting"
		if settings.Provider.Validate() == nil {
			field = "modelGatewayRouting"
		}
		return gatewayrouting.Settings{}, field, err
	}
	return settings, "", nil
}

func projectionSkip(id, code, field string) *ProviderProjectionSkip {
	return &ProviderProjectionSkip{ID: id, Code: code, Field: field}
}
