package openairesponses

import (
	"fmt"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/requestpolicy"
	"github.com/Wibias/Benes/internal/responses/continuation"
)

func prepareCanonicalBody(request protocol.ParsedRequest) ([]byte, error) {
	switch request.Source {
	case protocol.RequestSourceResponses:
		if err := validateMigratedRequestForCompile(request); err != nil {
			return nil, err
		}
	case protocol.RequestSourceChatCompletions, protocol.RequestSourceAnthropicMessages:
	default:
		return nil, fmt.Errorf("%w: request source is required", ErrUnsupportedRequestShape)
	}
	upstream := request
	if request.Source == protocol.RequestSourceAnthropicMessages {
		upstream.Context = normalizeAnthropicToolErrors(request.Context)
	}
	upstream.Stream = true
	return Compile(upstream)
}

func prepareForwardCanonicalBody(request protocol.ParsedRequest, destination string, configuredServiceTier string) ([]byte, error) {
	request.Options.ServiceTier = requestpolicy.ResolveServiceTier(request.Options.ServiceTier, configuredServiceTier)
	request.Options.Store = continuation.ApplyNativeStoreDefault(
		request.Options.Store,
		destination,
		"openai-responses",
		"forward",
	)
	body, err := prepareCanonicalBody(request)
	if err != nil {
		return nil, err
	}
	if request.CompactionRequest && nativeCodexForwardDestination(destination) {
		return appendCompactionTrigger(body)
	}
	return body, nil
}
