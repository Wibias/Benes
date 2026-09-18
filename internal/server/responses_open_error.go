package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/bridge"
	"github.com/Wibias/Benes/internal/responses/sse"
	"github.com/Wibias/Benes/internal/transport"
)

const localProviderInputTooLargeMessage = "The configured provider request-size limit rejects this turn. Reduce the current input or compact the conversation before retrying."

func writeResponsesOpenError(
	w http.ResponseWriter,
	request protocol.ParsedRequest,
	model string,
	routedCompaction bool,
	turn *resourcebudget.Turn,
	err error,
) bool {
	openErr := canonicalResponsesOpenError(err)
	if openErr == nil {
		return false
	}

	b := bridge.New(model, bridge.Options{
		Tools:                request.Context.Tools,
		ToolChoice:           request.Options.ToolChoice,
		ParallelToolCalls:    request.Options.ParallelToolCalls,
		HideThinkingSummary:  request.Options.HideThinkingSummary,
		RequestedServiceTier: request.Options.ServiceTier,
		CompactionRequest:    routedCompaction,
	})
	frames := b.Start()
	failed, bridgeErr := b.Handle(openErr.Event())
	if bridgeErr != nil {
		return false
	}
	frames = append(frames, failed...)
	enrichResponsesOpenErrorFrames(frames, openErr)

	if request.Stream {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		for _, frame := range frames {
			encoded, err := json.Marshal(frame.Data)
			if err != nil {
				return true
			}
			if turn != nil {
				if _, err := turn.Reserve(resourcebudget.ClassDownstreamQueue, int64(len(encoded))); err != nil {
					return true
				}
			}
			if err := sse.WriteJSON(w, frame.Data); err != nil {
				return true
			}
			if turn != nil {
				turn.MarkCommitted()
			}
		}
		return true
	}

	var terminal map[string]any
	for _, frame := range frames {
		if frame.Name != "response.failed" {
			continue
		}
		if response, ok := frame.Data["response"].(map[string]any); ok {
			terminal = response
		}
	}
	if terminal == nil {
		return false
	}
	encoded, marshalErr := json.Marshal(terminal)
	if marshalErr != nil {
		return false
	}
	if turn != nil {
		if _, reserveErr := turn.Reserve(resourcebudget.ClassOutput, int64(len(encoded))); reserveErr != nil {
			writeError(w, http.StatusServiceUnavailable, "resource_exhausted", "request resource budget is exhausted")
			return true
		}
	}
	status := openErr.StatusCode
	if status <= 0 {
		status = http.StatusRequestEntityTooLarge
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
	_, _ = w.Write([]byte("\n"))
	if turn != nil {
		turn.MarkCommitted()
	}
	return true
}

func canonicalResponsesOpenError(err error) *providercontract.OpenError {
	var openErr *providercontract.OpenError
	if errors.As(err, &openErr) && openErr != nil && openErr.Code == "context_length_exceeded" {
		return openErr
	}
	var bodyErr *transport.RequestBodyTooLargeError
	if !errors.As(err, &bodyErr) || bodyErr == nil {
		return nil
	}
	retryable := false
	return &providercontract.OpenError{
		StatusCode: http.StatusRequestEntityTooLarge,
		ErrorType:  "invalid_request_error",
		Code:       "context_length_exceeded",
		Message:    localProviderInputTooLargeMessage,
		Retryable:  &retryable,
	}
}

func enrichResponsesOpenErrorFrames(frames []bridge.Frame, openErr *providercontract.OpenError) {
	if openErr == nil {
		return
	}
	for _, frame := range frames {
		if frame.Name != "response.failed" {
			continue
		}
		response, ok := frame.Data["response"].(map[string]any)
		if !ok {
			continue
		}
		failure := map[string]any{
			"type":    openErr.ErrorType,
			"code":    openErr.Code,
			"message": openErr.Error(),
		}
		response["error"] = failure
		response["last_error"] = failure
		if openErr.Retryable != nil {
			response["retryable"] = *openErr.Retryable
		}
	}
}
