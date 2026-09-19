package server

import (
	"encoding/json"
	"errors"
	"net/http"

	anthropicout "github.com/Wibias/Benes/internal/anthropic/outbound"
	anthropicrequest "github.com/Wibias/Benes/internal/anthropic/request"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
	"github.com/Wibias/Benes/internal/timeline"
)

const anthropicMessagesPath = "/v1/messages"

func (h *handler) handleAnthropicMessages(w http.ResponseWriter, r *http.Request, trace *timeline.Trace) {
	request, err := anthropicrequest.Decode(r.Body, h.maxRequestBytes, anthropicrequest.DecodeOptions{})
	if err != nil {
		switch {
		case errors.Is(err, anthropicrequest.ErrTooLarge):
			writeAnthropicError(w, http.StatusRequestEntityTooLarge, "Anthropic Messages request is too large", "request_too_large", "request_too_large")
		case errors.Is(err, anthropicrequest.ErrUnsupportedTool):
			writeAnthropicError(w, http.StatusNotImplemented, "Anthropic Messages tool is not migrated to the Go path", "api_error", "migration_not_ready")
		default:
			writeAnthropicError(w, http.StatusBadRequest, "invalid Anthropic Messages request", "invalid_request_error", "")
		}
		return
	}

	route, err := h.parseRoute(request.ModelID)
	if err != nil {
		if message, ok := ambiguousAliasMessage(err); ok {
			writeAnthropicError(w, http.StatusBadRequest, message, "invalid_request_error", "invalid_request")
			return
		}
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "explicit_route_required")
		writeAnthropicError(w, http.StatusNotImplemented, "explicit provider/model routing is required by the Go migration path", "api_error", "migration_not_ready")
		return
	}
	if rec := sessionRecorderFrom(r.Context()); rec != nil {
		rec.SetParsed(request)
		rec.SetRoute(request.ModelID, route)
	}
	if diag := diagnosticsRecorderFrom(r.Context()); diag != nil {
		diag.SetRoute(request.ModelID, route)
	}
	resolved := h.resolveProviderWithEvidence(route, policyEvidenceFromRequest(request))
	if resolved.MissingCombo {
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "combo_not_found")
		writeAnthropicError(w, http.StatusNotFound, "combo is not configured", "not_found_error", "not_found")
		return
	}
	if resolved.MissingPolicy {
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "policy_not_found")
		writeAnthropicError(w, http.StatusNotFound, "routing profile is not configured", "not_found_error", "not_found")
		return
	}
	if resolved.MissingProvider {
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "provider_not_migrated")
		writeAnthropicError(w, http.StatusNotImplemented, "provider is not migrated to the Go Anthropic Messages path", "api_error", "migration_not_ready")
		return
	}
	provider := resolved.Provider
	trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", true, "")
	var searchClient *websearch.Client
	if hostedWebSearchRequested(request) {
		nativeHostedSearch := providercontract.SupportsNativeHostedWebSearch(provider, route.Model)
		_, client, err := h.webSearchClientForRequest(r.Context(), webSearchProviderID(route, resolved), route.Model, nativeHostedSearch)
		if err != nil {
			writeAnthropicError(w, http.StatusNotImplemented, err.Error(), "api_error", "web_search_unavailable")
			return
		}
		if client != nil {
			searchClient = client
			request.Context.Tools = websearch.ReplaceHostedTools(request.Context.Tools)
		}
	}

	requestedModel := request.ModelID
	request.UpstreamModelID = route.Model
	turn, err := h.acquireTurn(r.Context(), len(request.Raw))
	if err != nil {
		writeAnthropicError(w, http.StatusServiceUnavailable, "request resource budget is exhausted", "api_error", "resource_exhausted")
		return
	}
	defer turn.Close()
	reader := turn.OpenReader()
	defer reader.Close()
	dispatch := providercontract.DispatchRequest{
		ConfiguredServiceTier: admittedServiceTier(r.Context()),
		Parsed:                request,
		ForwardHeaders:        h.snapshotForwardHeaders(r.Header),
		Turn:                  turn,
	}

	var stream EventStream
	if searchClient != nil { stream, err = websearch.OpenLoop(r.Context(), provider.Open, dispatch, searchClient) } else { stream, err = provider.Open(r.Context(), dispatch) }
	stream = h.watchSession(r.Context(), provider, stream, err)
	if err != nil {
		trace.Mark(timeline.StageUpstreamWaitHeaders, timeline.SideUpstream, timeline.MilestoneDispatch, false, "provider_open_failed")
		if structuredOutputCapabilityRefusal(err) {
			writeAnthropicError(w, http.StatusBadRequest, unsupportedStructuredOutputMessage, "invalid_request_error", unsupportedStructuredOutputCode)
			return
		}
		writeAnthropicError(w, http.StatusBadGateway, publicProviderOpenMessage(err), "api_error", "")
		return
	}
	trace.Mark(timeline.StageUpstreamWaitHeaders, timeline.SideUpstream, timeline.MilestoneDispatch, true, "")
	defer stream.Close()
	if request.Stream { _ = anthropicout.WriteSSE(r.Context(), w, stream, requestedModel, anthropicout.Options{Trace: trace, Turn: turn}); return }
	message, err := anthropicout.Collect(stream, requestedModel, anthropicout.Options{Trace: trace})
	if err != nil {
		var terminal *anthropicout.TerminalError
		if errors.As(err, &terminal) { status := terminal.Status; if status < 400 || status > 599 { status = http.StatusBadGateway }; writeAnthropicError(w, status, terminal.Message, terminal.Type, terminal.Code); return }
		writeAnthropicError(w, http.StatusBadGateway, "provider stream failed", "api_error", ""); return
	}
	encoded, err := json.Marshal(message)
	if err != nil { writeAnthropicError(w, http.StatusBadGateway, "provider stream failed", "api_error", ""); return }
	if turn != nil { if _, err := turn.Reserve(resourcebudget.ClassOutput, int64(len(encoded))); err != nil { writeAnthropicError(w, http.StatusServiceUnavailable, "request resource budget is exhausted", "api_error", "resource_exhausted"); return } }
	w.Header().Set("Content-Type", "application/json"); w.Header().Set("Cache-Control", "no-store"); w.Header().Set("X-Content-Type-Options", "nosniff"); w.WriteHeader(http.StatusOK); _, _ = w.Write(encoded); _, _ = w.Write([]byte("\n")); turn.MarkCommitted()
}

func writeAnthropicError(w http.ResponseWriter, status int, message, typ, code string) {
	if status < 400 || status > 599 { status = http.StatusBadGateway }
	if typ == "" { typ = "api_error" }
	errBody := map[string]any{"type": typ, "message": message}; if code != "" { errBody["code"] = code }
	w.Header().Set("Content-Type", "application/json"); w.Header().Set("Cache-Control", "no-store"); w.Header().Set("X-Content-Type-Options", "nosniff"); w.WriteHeader(status); _ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": errBody})
}
