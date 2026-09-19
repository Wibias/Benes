package providerregistry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/Wibias/Benes/internal/capability"
	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/gatewayrouting"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/providers/anthropicmessages"
	"github.com/Wibias/Benes/internal/providers/antigravity"
	"github.com/Wibias/Benes/internal/providers/cursor"
	"github.com/Wibias/Benes/internal/providers/google"
	"github.com/Wibias/Benes/internal/providers/kiro"
	"github.com/Wibias/Benes/internal/providers/openaichat"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
	"github.com/Wibias/Benes/internal/providers/xaicapability"
	"github.com/Wibias/Benes/internal/responses/continuation"
	"github.com/Wibias/Benes/internal/transport"
)

type Protocol string

const (
	ProtocolOpenAIResponses   Protocol = "openai-responses"
	ProtocolOpenAIChat        Protocol = "openai-chat"
	ProtocolAnthropicMessages Protocol = "anthropic-messages"
	ProtocolGoogleAntigravity Protocol = "google-antigravity"
	ProtocolCursor            Protocol = "cursor"
	ProtocolKiro              Protocol = "kiro"
	ProtocolGoogle            Protocol = "google"
	ProtocolGoogleVertex      Protocol = "google-vertex"
)

type AuthMode string

const (
	AuthModeKey     AuthMode = "key"
	AuthModeForward AuthMode = "forward"
	AuthModeOAuth   AuthMode = "oauth"
)

type APIKeyTransport string

const (
	APIKeyTransportXAPIKey APIKeyTransport = "x-api-key"
	APIKeyTransportBearer  APIKeyTransport = "bearer"
)

type CodexAccountMode string

const (
	CodexAccountModeDirect CodexAccountMode = "direct"
	CodexAccountModePool   CodexAccountMode = "pool"
)

var ErrCodexPoolAuthorityRequired = errors.New("Codex Pool requires an explicit forward credential authority")

type ChatOptions struct {
	NativeOpenAI                       bool
	PreserveReasoningContent           bool
	PreserveReasoningContentModels     []string
	RequiresReasoningPlaceholderModels []string
	ReasoningSplitModels               []string
}

type Capability struct {
	SupportsServiceTier           *bool
	ModelSupportsServiceTier      map[string]bool
	ChatServiceTier               bool
	SupportsStructuredOutput      *bool
	ModelSupportsStructuredOutput map[string]bool
	NoStructuredOutputModels      []string
	HostedWebSearch               bool
	WebSearchModels               []string
}

type APIKeySlot struct {
	ID  string
	Key string
	Ref credentials.Ref
}

type Spec struct {
	ID                      string
	Protocol                Protocol
	AuthMode                AuthMode
	CodexAccountMode        CodexAccountMode
	Endpoint                string
	APIKey                  string
	APIKeyTransport         APIKeyTransport
	APIKeyPool              []APIKeySlot
	DestinationPolicy       transport.DestinationPolicy
	TransportOptions        *transport.ClientOptions
	MaxUpstreamBodyBytes    int64
	RequestPacing           time.Duration
	ModelRequestPacing      map[string]time.Duration
	CredentialRef           credentials.Ref
	Chat                    ChatOptions
	Capability              Capability
	GatewayRouting          gatewayrouting.Settings
	Transient5xx            transport.Transient5xxPolicy
	OpenCodeSessionOverride string
	CatalogModels           []string
	Project                 string
	Accounts                []antigravity.Account
	HTTPVersion             string
	ProfileARN              string
	APIRegion               string
	SSORegion               string
	AuthType                string
	Location                string
	KiroAccounts            []kiro.AccountSnapshot
}

type Options struct {
	TransportOptions   transport.ClientOptions
	ForwardAuthorities map[string]openairesponses.ForwardCredentialAuthority
	Continuation       *continuation.Authority
	Credentials        credentials.Store
	PacingRuntime      map[string]*transport.Pacer
}

func Build(ctx context.Context, specs []Spec, options Options) (map[string]providers.Responses, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("at least one provider is required")
	}
	if err := validateSpecs(specs); err != nil {
		return nil, err
	}
	if err := validateRuntimeDependencies(specs, options); err != nil {
		return nil, err
	}

	registry := make(map[string]providers.Responses, len(specs))
	for _, spec := range specs {
		transportOptions := options.TransportOptions
		if spec.TransportOptions != nil {
			transportOptions = *spec.TransportOptions
		}
		if spec.MaxUpstreamBodyBytes > 0 {
			transportOptions.MaxRequestBodyBytes = spec.MaxUpstreamBodyBytes
		}
		transportOptions, pacingEnabled := applySpecPacing(spec, transportOptions, options.PacingRuntime)
		spec, err := adoptSpecSecrets(options.Credentials, spec)
		if err != nil {
			return nil, fmt.Errorf("adopt credentials for provider %q: %w", spec.ID, err)
		}
		spec, err = resolveSpecSecrets(options.Credentials, spec)
		if err != nil {
			return nil, fmt.Errorf("resolve credentials for provider %q: %w", spec.ID, err)
		}

		var provider providers.Responses
		switch spec.Protocol {
		case ProtocolOpenAIResponses:
			if effectiveAuthMode(spec.AuthMode) == AuthModeForward {
				provider, err = openairesponses.NewForwardHardened(ctx, openairesponses.ForwardConfig{
					Endpoint:            spec.Endpoint,
					ProviderID:          spec.ID,
					DestinationPolicy:   spec.DestinationPolicy,
					TransportOptions:    transportOptions,
					CredentialAuthority: forwardAuthorityForSpec(spec, options),
					Continuation:        options.Continuation,
				})
			} else {
				provider, err = openairesponses.NewHardened(ctx, openairesponses.Config{
					Endpoint:          spec.Endpoint,
					APIKey:            spec.APIKey,
					APIKeyPool:        cloneAPIKeyPool(spec.APIKeyPool),
					ProviderID:        spec.ID,
					DestinationPolicy: spec.DestinationPolicy,
					TransportOptions:  transportOptions,
					Continuation:      options.Continuation,
					Credentials:       options.Credentials,
					CredentialRef:     spec.CredentialRef,
					Capability:        capabilityPolicy(spec, specAuthClass(spec)),
					Transient5xx:      spec.Transient5xx,
				})
			}
		case ProtocolOpenAIChat:
			provider, err = openaichat.NewHardened(ctx, openaichat.Config{
				Endpoint:                           spec.Endpoint,
				APIKey:                             spec.APIKey,
				DestinationPolicy:                  spec.DestinationPolicy,
				TransportOptions:                   transportOptions,
				PreserveReasoningContentModels:     cloneOptionalStrings(spec.Chat.PreserveReasoningContentModels),
				RequiresReasoningPlaceholderModels: cloneOptionalStrings(spec.Chat.RequiresReasoningPlaceholderModels),
				ReasoningSplitModels:               cloneOptionalStrings(spec.Chat.ReasoningSplitModels),
				CompileOptions: openaichat.CompileOptions{
					NativeOpenAI:             spec.Chat.NativeOpenAI,
					PreserveReasoningContent: spec.Chat.PreserveReasoningContent,
				},
				Credentials:    options.Credentials,
				CredentialRef:  spec.CredentialRef,
				Capability:     capabilityPolicy(spec, specAuthClass(spec)),
				ProviderID:     spec.ID,
				GatewayRouting: spec.GatewayRouting.Clone(),
				Transient5xx:   spec.Transient5xx,
			})
			if err == nil && usesOpenCodeGoMixedWire(spec) {
				provider, err = wrapOpenCodeGoMixedWire(ctx, spec, provider, transportOptions, options)
			}
		case ProtocolAnthropicMessages:
			provider, err = anthropicmessages.NewHardened(ctx, anthropicmessages.Config{
				Endpoint:          spec.Endpoint,
				APIKey:            spec.APIKey,
				APIKeyTransport:   anthropicmessages.KeyTransport(spec.APIKeyTransport),
				ProviderID:        spec.ID,
				DestinationPolicy: spec.DestinationPolicy,
				TransportOptions:  transportOptions,
				Credentials:       options.Credentials,
				CredentialRef:     spec.CredentialRef,
				Transient5xx:      spec.Transient5xx,
			})
		case ProtocolGoogleAntigravity:
			provider, err = antigravity.NewHardened(ctx, antigravity.Config{
				Endpoint:          spec.Endpoint,
				Accounts:          antigravityAccounts(spec),
				CatalogModels:     append([]string(nil), spec.CatalogModels...),
				DestinationPolicy: spec.DestinationPolicy,
				TransportOptions:  transportOptions,
				Continuation:      options.Continuation,
			})
		case ProtocolCursor:
			provider, err = cursor.NewHardened(ctx, cursor.Config{
				Endpoint:          spec.Endpoint,
				APIKey:            spec.APIKey,
				HTTPVersion:       cursor.DefaultHTTPVersion(spec.HTTPVersion),
				DestinationPolicy: spec.DestinationPolicy,
				TransportOptions:  transportOptions,
				Continuation:      options.Continuation,
			})
		case ProtocolKiro:
			provider, err = kiro.NewHardened(ctx, kiro.Config{
				Account: kiro.AccountSnapshot{
					AccessToken: spec.APIKey,
					ProfileARN:  spec.ProfileARN,
					APIRegion:   spec.APIRegion,
					SSORegion:   spec.SSORegion,
					AuthType:    spec.AuthType,
				},
				Accounts:          append([]kiro.AccountSnapshot(nil), spec.KiroAccounts...),
				Endpoint:          spec.Endpoint,
				DestinationPolicy: spec.DestinationPolicy,
				TransportOptions:  transportOptions,
				Continuation:      options.Continuation,
			})
		case ProtocolGoogle:
			provider, err = google.New(ctx, google.Config{
				Kind:              google.KindAIStudio,
				APIKey:            spec.APIKey,
				Endpoint:          spec.Endpoint,
				Continuation:      options.Continuation,
				Keys:              googleKeySlots(spec),
				DestinationPolicy: spec.DestinationPolicy,
				TransportOptions:  transportOptions,
				Capability:        capabilityPolicy(spec, specAuthClass(spec)),
			})
		case ProtocolGoogleVertex:
			provider, err = google.New(ctx, google.Config{
				Kind:              google.KindVertex,
				APIKey:            spec.APIKey,
				Project:           spec.Project,
				Location:          spec.Location,
				Continuation:      options.Continuation,
				Keys:              googleKeySlots(spec),
				DestinationPolicy: spec.DestinationPolicy,
				TransportOptions:  transportOptions,
				Capability:        capabilityPolicy(spec, specAuthClass(spec)),
			})
		}
		if err != nil {
			return nil, fmt.Errorf("construct provider %q: %w", spec.ID, err)
		}
		registry[spec.ID] = withPacingLabel(provider, pacingEnabled)
	}
	return registry, nil
}

func validateSpecs(specs []Spec) error {
	seen := make(map[string]struct{}, len(specs))
	for _, spec := range specs {
		if !validProviderID(spec.ID) {
			return fmt.Errorf("invalid provider id %q", spec.ID)
		}
		if _, exists := seen[spec.ID]; exists {
			return fmt.Errorf("duplicate provider id %q", spec.ID)
		}
		seen[spec.ID] = struct{}{}
		switch spec.Protocol {
		case ProtocolOpenAIResponses, ProtocolOpenAIChat, ProtocolAnthropicMessages, ProtocolGoogleAntigravity, ProtocolCursor, ProtocolKiro, ProtocolGoogle, ProtocolGoogleVertex:
		default:
			return fmt.Errorf("unsupported provider protocol %q for provider %q", spec.Protocol, spec.ID)
		}
		if spec.MaxUpstreamBodyBytes < 0 {
			return fmt.Errorf("provider %q: max upstream body bytes must be non-negative", spec.ID)
		}
		if spec.MaxUpstreamBodyBytes > 0 && spec.Protocol != ProtocolOpenAIResponses {
			return fmt.Errorf("provider %q: max upstream body bytes are only supported for openai-responses", spec.ID)
		}
		if spec.Protocol == ProtocolAnthropicMessages && len(spec.APIKeyPool) > 0 {
			return fmt.Errorf("provider %q: Anthropic Messages does not support an API key pool", spec.ID)
		}
		if spec.OpenCodeSessionOverride != "" {
			if !usesOpenCodeGoMixedWire(spec) {
				return fmt.Errorf("provider %q: OpenCode Go session override requires the canonical OpenCode Go mixed-wire provider", spec.ID)
			}
			if !validOpenCodeGoSessionOverride(spec.OpenCodeSessionOverride) {
				return fmt.Errorf("provider %q: invalid OpenCode Go session override", spec.ID)
			}
		}

		authMode := effectiveAuthMode(spec.AuthMode)
		switch authMode {
		case AuthModeKey:
		case AuthModeForward:
			if spec.Protocol != ProtocolOpenAIResponses {
				return fmt.Errorf("provider %q: forward auth is only supported for openai-responses", spec.ID)
			}
			if strings.TrimSpace(spec.APIKey) != "" {
				return fmt.Errorf("provider %q: forward auth cannot include a provider API key", spec.ID)
			}
		case AuthModeOAuth:
			switch spec.ID {
			case "xai":
				if spec.Protocol != ProtocolOpenAIChat && spec.Protocol != ProtocolOpenAIResponses {
					return fmt.Errorf("provider %q: oauth auth requires openai-chat or openai-responses", spec.ID)
				}
			case "kiro":
				if spec.Protocol != ProtocolKiro {
					return fmt.Errorf("provider %q: oauth auth requires kiro", spec.ID)
				}
			default:
				return fmt.Errorf("provider %q: oauth auth is only supported for xai", spec.ID)
			}
		default:
			return fmt.Errorf("unsupported provider auth mode %q for provider %q", spec.AuthMode, spec.ID)
		}

		if spec.Protocol == ProtocolAnthropicMessages {
			switch spec.APIKeyTransport {
			case "", APIKeyTransportXAPIKey, APIKeyTransportBearer:
			default:
				return fmt.Errorf("provider %q: unsupported Anthropic API key transport %q", spec.ID, spec.APIKeyTransport)
			}
		} else if spec.APIKeyTransport != "" {
			return fmt.Errorf("provider %q: API key transport is only supported for anthropic-messages", spec.ID)
		}

		if err := spec.GatewayRouting.Validate(); err != nil {
			return fmt.Errorf("provider %q: %w", spec.ID, err)
		}
		if spec.Protocol != ProtocolOpenAIChat && !spec.GatewayRouting.Empty() {
			return fmt.Errorf("provider %q: gateway routing is only supported for openai-chat", spec.ID)
		}

		if spec.CodexAccountMode != "" {
			if spec.ID != "openai" || authMode != AuthModeForward || spec.Protocol != ProtocolOpenAIResponses {
				return fmt.Errorf("provider %q: codex account mode is only supported for canonical openai forward auth", spec.ID)
			}
			switch spec.CodexAccountMode {
			case CodexAccountModeDirect, CodexAccountModePool:
			default:
				return fmt.Errorf("unsupported Codex account mode %q for provider %q", spec.CodexAccountMode, spec.ID)
			}
		}
	}
	return nil
}

func validateRuntimeDependencies(specs []Spec, options Options) error {
	for _, spec := range specs {
		if spec.CodexAccountMode != CodexAccountModePool {
			continue
		}
		if options.ForwardAuthorities == nil || options.ForwardAuthorities[spec.ID] == nil {
			return fmt.Errorf("provider %q: %w", spec.ID, ErrCodexPoolAuthorityRequired)
		}
	}
	return nil
}

func forwardAuthorityForSpec(spec Spec, options Options) openairesponses.ForwardCredentialAuthority {
	switch spec.CodexAccountMode {
	case CodexAccountModeDirect:
		if options.ForwardAuthorities != nil && options.ForwardAuthorities[spec.ID] != nil {
			return options.ForwardAuthorities[spec.ID]
		}
		return openairesponses.DirectForwardAuthority{}
	case CodexAccountModePool:
		return options.ForwardAuthorities[spec.ID]
	default:
		return nil
	}
}

func effectiveAuthMode(mode AuthMode) AuthMode {
	if mode == "" {
		return AuthModeKey
	}
	return mode
}

func validProviderID(id string) bool {
	if id == "" || id != strings.TrimSpace(id) || strings.ContainsRune(id, '/') {
		return false
	}
	for _, r := range id {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func cloneBoolMap(values map[string]bool) map[string]bool {
	if values == nil {
		return nil
	}
	cloned := make(map[string]bool, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneOptionalStrings(values []string) []string {
	if values == nil {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func capabilityPolicy(spec Spec, authClass string) capability.Policy {
	return capability.Policy{
		ProviderID:               spec.ID,
		Protocol:                 string(spec.Protocol),
		Endpoint:                 spec.Endpoint,
		AuthClass:                authClass,
		SupportsServiceTier:      spec.Capability.SupportsServiceTier,
		ModelSupportsServiceTier: spec.Capability.ModelSupportsServiceTier,
		ChatServiceTier:               spec.Capability.ChatServiceTier,
		SupportsStructuredOutput:      spec.Capability.SupportsStructuredOutput,
		ModelSupportsStructuredOutput: cloneBoolMap(spec.Capability.ModelSupportsStructuredOutput),
		NoStructuredOutputModels:      append([]string(nil), spec.Capability.NoStructuredOutputModels...),
	}
}

func specAuthClass(spec Spec) string {
	return xaicapability.AuthClass(xaicapability.IsOAuthProxyEndpoint(spec.Endpoint) || effectiveAuthMode(spec.AuthMode) == AuthModeOAuth)
}

func adoptSpecSecrets(store credentials.Store, spec Spec) (Spec, error) {
	if store == nil || effectiveAuthMode(spec.AuthMode) == AuthModeForward || effectiveAuthMode(spec.AuthMode) == AuthModeOAuth {
		return spec, nil
	}
	if spec.CredentialRef.ID == "" && spec.CredentialRef.Source == "" {
		if key := strings.TrimSpace(spec.APIKey); key != "" {
			ref, err := store.Put(spec.ID, []byte(key))
			if err != nil {
				return spec, err
			}
			if _, err := store.Get(ref); err != nil {
				_ = store.Delete(ref)
				return spec, err
			}
			spec.CredentialRef = ref
			spec.APIKey = ""
		}
	}
	for i, slot := range spec.APIKeyPool {
		if strings.TrimSpace(slot.Key) == "" {
			continue
		}
		ref, err := store.Put(fmt.Sprintf("%s-pool-%d", spec.ID, i), []byte(slot.Key))
		if err != nil {
			return spec, err
		}
		if _, err := store.Get(ref); err != nil {
			_ = store.Delete(ref)
			return spec, err
		}
		spec.APIKeyPool[i].Ref = ref
		spec.APIKeyPool[i].Key = ""
	}
	return spec, nil
}

func LiveCredentialRefs(specs []Spec) []credentials.Ref {
	var out []credentials.Ref
	for _, spec := range specs {
		if spec.CredentialRef.ID != "" || spec.CredentialRef.Source != "" {
			out = append(out, spec.CredentialRef)
		}
		for _, slot := range spec.APIKeyPool {
			if slot.Ref.ID != "" || slot.Ref.Source != "" {
				out = append(out, slot.Ref)
			}
		}
	}
	return out
}

func resolveSpecSecrets(store credentials.Store, spec Spec) (Spec, error) {
	if store == nil {
		return spec, nil
	}
	if strings.TrimSpace(spec.APIKey) == "" && (spec.CredentialRef.ID != "" || spec.CredentialRef.Source != "") {
		secret, err := store.Get(spec.CredentialRef)
		if err != nil {
			return spec, err
		}
		spec.APIKey = string(secret)
	}
	for i, slot := range spec.APIKeyPool {
		if strings.TrimSpace(slot.Key) != "" || (slot.Ref.ID == "" && slot.Ref.Source == "") {
			continue
		}
		secret, err := store.Get(slot.Ref)
		if err != nil {
			return spec, err
		}
		spec.APIKeyPool[i].Key = string(secret)
	}
	return spec, nil
}

func antigravityAccounts(spec Spec) []antigravity.Account {
	if len(spec.Accounts) > 0 {
		return append([]antigravity.Account(nil), spec.Accounts...)
	}
	project := strings.TrimSpace(spec.Project)
	if len(spec.APIKeyPool) > 0 {
		out := make([]antigravity.Account, 0, len(spec.APIKeyPool))
		for i, slot := range spec.APIKeyPool {
			token := strings.TrimSpace(slot.Key)
			if token == "" {
				continue
			}
			id := strings.TrimSpace(slot.ID)
			if id == "" {
				id = fmt.Sprintf("%s-%d", spec.ID, i)
			}
			out = append(out, antigravity.Account{ID: id, Token: token, ProjectID: project})
		}
		return out
	}
	if token := strings.TrimSpace(spec.APIKey); token != "" {
		return []antigravity.Account{{ID: spec.ID, Token: token, ProjectID: project}}
	}
	return nil
}

func googleKeySlots(spec Spec) []google.KeySlot {
	if len(spec.APIKeyPool) == 0 {
		return nil
	}
	out := make([]google.KeySlot, 0, len(spec.APIKeyPool))
	for _, slot := range spec.APIKeyPool {
		if strings.TrimSpace(slot.Key) == "" {
			continue
		}
		out = append(out, google.KeySlot{ID: slot.ID, Key: slot.Key})
	}
	return out
}

func cloneAPIKeyPool(slots []APIKeySlot) []openairesponses.APIKeySlot {
	if len(slots) == 0 {
		return nil
	}
	cloned := make([]openairesponses.APIKeySlot, len(slots))
	for i, slot := range slots {
		cloned[i] = openairesponses.APIKeySlot{ID: slot.ID, Key: slot.Key}
	}
	return cloned
}
