package providerregistry

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/providers/anthropicmessages"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
	"github.com/Wibias/Benes/internal/transport"
)

func siblingEndpointFromChat(chatEndpoint, sibling string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(chatEndpoint), "/")
	const suffix = "/chat/completions"
	if !strings.HasSuffix(trimmed, suffix) {
		return "", fmt.Errorf("chat completions suffix required")
	}
	return strings.TrimSuffix(trimmed, suffix) + "/" + strings.TrimLeft(sibling, "/"), nil
}

func responsesEndpointFromChat(chatEndpoint string) (string, error) {
	return siblingEndpointFromChat(chatEndpoint, "responses")
}

func messagesEndpointFromChat(chatEndpoint string) (string, error) {
	return siblingEndpointFromChat(chatEndpoint, "messages")
}

type mixedWireProvider struct {
	chat              providers.Responses
	responses         providers.Responses
	messages          providers.Responses
	responsesEndpoint string
	sessionOverride   string
}

func (p mixedWireProvider) Open(ctx context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	dispatch = bindOpenCodeGoSession(dispatch, p.sessionOverride)
	model := strings.TrimSpace(dispatch.Parsed.UpstreamModelID)
	if model == "" {
		model = strings.TrimSpace(dispatch.Parsed.ModelID)
	}
	switch openCodeGoProtocol(model) {
	case ProtocolOpenAIResponses:
		dispatch.Parsed = openairesponses.SanitizeOpenCodeGoMuseWebSearch(dispatch.Parsed, p.responsesEndpoint)
		return p.responses.Open(ctx, dispatch)
	case ProtocolAnthropicMessages:
		return p.messages.Open(ctx, dispatch)
	default:
		return p.chat.Open(ctx, dispatch)
	}
}

func (p mixedWireProvider) SupportsNativeHostedWebSearch(modelID string) bool {
	return openCodeGoProtocol(strings.TrimSpace(modelID)) == ProtocolOpenAIResponses &&
		openairesponses.SupportsNativeHostedWebSearchDestination(p.responsesEndpoint)
}

func usesOpenCodeGoMixedWire(spec Spec) bool {
	return spec.ID == "opencode-go" && spec.Protocol == ProtocolOpenAIChat
}

func wrapOpenCodeGoMixedWire(ctx context.Context, spec Spec, chat providers.Responses, transportOptions transport.ClientOptions, options Options) (providers.Responses, error) {
	responsesURL, err := responsesEndpointFromChat(spec.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("cannot derive OpenCode Go Responses endpoint: %w", err)
	}
	messagesURL, err := messagesEndpointFromChat(spec.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("cannot derive OpenCode Go Messages endpoint: %w", err)
	}

	policy := capabilityPolicy(spec, "api-key")
	policy.Protocol = string(ProtocolOpenAIResponses)
	policy.Endpoint = responsesURL
	responses, err := openairesponses.NewHardened(ctx, openairesponses.Config{
		Endpoint:          responsesURL,
		APIKey:            spec.APIKey,
		APIKeyPool:        cloneAPIKeyPool(spec.APIKeyPool),
		ProviderID:        spec.ID,
		DestinationPolicy: spec.DestinationPolicy,
		TransportOptions:  transportOptions,
		Continuation:      options.Continuation,
		Credentials:       options.Credentials,
		CredentialRef:     spec.CredentialRef,
		Capability:        policy,
		Transient5xx:      spec.Transient5xx,
		UserAgent:         spec.UserAgent,
	})
	if err != nil {
		return nil, err
	}
	messages, err := anthropicmessages.NewHardened(ctx, anthropicmessages.Config{
		Endpoint:          messagesURL,
		APIKey:            spec.APIKey,
		ProviderID:        spec.ID,
		DestinationPolicy: spec.DestinationPolicy,
		TransportOptions:  transportOptions,
		Credentials:       options.Credentials,
		CredentialRef:     spec.CredentialRef,
		Transient5xx:      spec.Transient5xx,
	})
	if err != nil {
		return nil, err
	}
	return mixedWireProvider{
		chat:              chat,
		responses:         responses,
		messages:          messages,
		responsesEndpoint: responsesURL,
		sessionOverride:   spec.OpenCodeSessionOverride,
	}, nil
}
