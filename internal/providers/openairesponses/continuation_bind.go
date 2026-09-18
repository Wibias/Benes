package openairesponses

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/responses/continuation"
)

func bindDispatchContinuation(
	authority *continuation.Authority,
	dispatch providers.DispatchRequest,
	identity continuation.PhysicalIdentity,
	stripPreviousOnMiss bool,
) (continuation.Bound, error) {
	if authority == nil {
		request := dispatch.Parsed
		if stripPreviousOnMiss && strings.TrimSpace(request.PreviousResponseID) != "" {
			request.PreviousResponseID = ""
		}
		return continuation.Bound{Request: request}, nil
	}
	return authority.Bind(continuation.BindRequest{
		Identity:            identity,
		Thread:              continuationThread(dispatch),
		Turn:                dispatch.Turn,
		Request:             dispatch.Parsed,
		StripPreviousOnMiss: stripPreviousOnMiss,
	})
}

func continuationThread(dispatch providers.DispatchRequest) string {
	if thread := strings.TrimSpace(dispatch.ForwardHeaders.Get("thread-id")); thread != "" {
		return thread
	}
	return strings.TrimSpace(dispatch.Parsed.PreviousResponseID)
}

func nativeCodexForwardDestination(destination string) bool {
	return continuation.ApplyNativeStoreDefault(nil, destination, "openai-responses", "forward") != nil
}

func wrapContinuationStream(stream providers.EventStream, bound continuation.Bound) providers.EventStream {
	if stream == nil {
		bound.Release()
		return stream
	}
	wrapped := stream
	if inner, ok := stream.(*Stream); ok {
		inner.release = bound.Release
	} else if inner, ok := stream.(*observedForwardStream); ok {
		if raw, ok := inner.inner.(*Stream); ok {
			raw.release = bound.Release
		} else {
			wrapped = &continuationStream{inner: stream, bound: bound}
		}
	} else {
		wrapped = &continuationStream{inner: stream, bound: bound}
	}
	return wrapToolIdentityStream(wrapped, bound.Request)
}

func attachContinuationPersistence(stream providers.EventStream, authority *continuation.Authority, bound continuation.Bound, thread string) {
	raw, ok := stream.(*Stream)
	if !ok {
		if observed, ok := stream.(*observedForwardStream); ok {
			raw, _ = observed.inner.(*Stream)
		}
	}
	if raw == nil || authority == nil || bound.Owner.Provider == "" {
		return
	}
	thread = firstNonEmptyThread(thread, bound.Request.PreviousResponseID)
	raw.onTerminalResponse = func(response json.RawMessage) {
		if len(response) == 0 {
			return
		}
		_ = authority.Remember(bound.Owner, bound.Durable, bound.Request, response)
		_ = authority.RememberOutputSignatures(bound.Owner, bound.Durable, thread, response)
		_ = authority.Settle(context.Background())
	}
}

type continuationStream struct {
	inner providers.EventStream
	bound continuation.Bound
}

func (s *continuationStream) Next() (protocol.Event, error) {
	return s.inner.Next()
}

func (s *continuationStream) Close() error {
	s.bound.Release()
	if s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

func (s *continuationStream) PhysicalOwnership() *providers.PhysicalPin {
	if s == nil || s.inner == nil {
		return nil
	}
	if reporter, ok := s.inner.(interface{ PhysicalOwnership() *providers.PhysicalPin }); ok {
		return reporter.PhysicalOwnership()
	}
	return nil
}

func firstNonEmptyThread(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
