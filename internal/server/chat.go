package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	chatoutbound "github.com/Wibias/Benes/internal/chat/outbound"
	chatrequest "github.com/Wibias/Benes/internal/chat/request"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
	"github.com/Wibias/Benes/internal/timeline"
)

const chatCompletionsPath = "/v1/chat/completions"

func (h *handler) handleChatCompletions(w http.ResponseWriter, r *http.Request, trace *timeline.Trace) {
	request, err := chatrequest.Decode(r.Body, h.maxRequestBytes, chatrequest.DecodeOptions{})
	if err != nil {
		status := http.StatusBadRequest
		message := "invalid Chat Completions request"
		code := ""
		switch {
		case errors.Is(err, chatrequest.ErrTooLarge):
			status = http.StatusRequestEntityTooLarge
			message = "Chat Completions request is too large"
			code = "request_too_large"
		case errors.Is(err, chatrequest.ErrUnsupportedTool):
			status = http.StatusNotImplemented
			message = "Chat Completions tool is not migrated to the Go path"
			code = "migration_not_ready"
		}
		writeChatError(w, status, message, "invalid_request_error", code)
		return
	}

	route, err := h.parseRoute(request.ModelID)
	if err != nil {
		if message, ok := ambiguousAliasMessage(err); ok {
			writeChatError(w, http.StatusBadRequest, message, "invalid_request_error", "invalid_request")
			return
		}
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "explicit_route_required")
		writeChatError(w, http.StatusNotImplemented, "explicit provider/model routing is required by the Go migration path", "invalid_request_error", "migration_not_ready")
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
		writeChatError(w, http.StatusNotFound, "combo is not configured", "invalid_request_error", "not_found")
		return
	}
	if resolved.MissingPolicy {
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "policy_not_found")
		writeChatError(w, http.StatusNotFound, "routing profile is not configured", "invalid_request_error", "not_found")
		return
	}
	if resolved.MissingProvider {
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "provider_not_migrated")
		writeChatError(w, http.StatusNotImplemented, "provider is not migrated to the Go Chat Completions path", "invalid_request_error", "migration_not_ready")
		return
	}
	provider := resolved.Provider
	trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", true, "")
	var searchClient *websearch.Client
	if hostedWebSearchRequested(request) {
		nativeHostedSearch := providercontract.SupportsNativeHostedWebSearch(provider, route.Model)
		_, client, err := h.webSearchClientForRequest(r.Context(), webSearchProviderID(route, resolved), route.Model, nativeHostedSearch)
		if err != nil {
			writeChatError(w, http.StatusNotImplemented, err.Error(), "invalid_request_error", "web_search_unavailable")
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
		writeChatError(w, http.StatusServiceUnavailable, "request resource budget is exhausted", "server_error", "resource_exhausted")
		return
	}
	defer turn.Close()
	reader := turn.OpenReader()
	defer reader.Close()
	dispatch := providercontract.DispatchRequest{
		Parsed:                request,
		ForwardHeaders:        h.snapshotForwardHeaders(r.Header),
		Turn:                  turn,
		ConfiguredServiceTier: admittedServiceTier(r.Context()),
	}

	var stream EventStream
	if searchClient != nil {
		stream, err = websearch.OpenLoop(r.Context(), provider.Open, dispatch, searchClient)
	} else {
		stream, err = provider.Open(r.Context(), dispatch)
	}
	stream = h.watchSession(r.Context(), provider, stream, err)
	if err != nil {
		if errors.Is(err, resourcebudget.ErrPhysicalSendBudgetExceeded) {
			trace.Mark(timeline.StageUpstreamWaitHeaders, timeline.SideLocal, timeline.MilestoneDispatch, false, physicalSendBudgetErrorCode)
			writeChatError(w, http.StatusTooManyRequests, physicalSendBudgetErrorMessage, "server_error", physicalSendBudgetErrorCode)
			return
		}
		trace.Mark(timeline.StageUpstreamWaitHeaders, timeline.SideUpstream, timeline.MilestoneDispatch, false, "provider_open_failed")
		if structuredOutputCapabilityRefusal(err) {
			writeChatError(w, http.StatusBadRequest, unsupportedStructuredOutputMessage, "invalid_request_error", unsupportedStructuredOutputCode)
			return
		}
		writeChatError(w, http.StatusBadGateway, publicProviderOpenMessage(err), "upstream_error", "")
		return
	}
	trace.Mark(timeline.StageUpstreamWaitHeaders, timeline.SideUpstream, timeline.MilestoneDispatch, true, "")
	defer stream.Close()

	opts := chatoutbound.Options{Trace: trace, Turn: turn, RequestedServiceTier: request.Options.ServiceTier}
	if request.Stream {
		_ = chatoutbound.WriteSSE(r.Context(), w, stream, requestedModel, opts)
		return
	}

	completion, err := chatoutbound.Collect(stream, requestedModel, opts)

	if err != nil {
		var terminal *chatoutbound.TerminalError
		if errors.As(err, &terminal) {
			status := terminal.Status
			if status < 400 || status > 599 {
				status = http.StatusBadGateway
			}
			writeChatError(w, status, terminal.Message, terminal.Type, terminal.Code)
			return
		}
		writeChatError(w, http.StatusBadGateway, "provider stream failed", "upstream_error", "")
		return
	}

	encoded, err := json.Marshal(completion)
	if err != nil {
		writeChatError(w, http.StatusBadGateway, "provider stream failed", "upstream_error", "")
		return
	}
	if turn != nil {
		if _, err := turn.Reserve(resourcebudget.ClassOutput, int64(len(encoded))); err != nil {
			writeChatError(w, http.StatusServiceUnavailable, "request resource budget is exhausted", "server_error", "resource_exhausted")
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
	_, _ = w.Write([]byte("\n"))
	turn.MarkCommitted()
}

func hostedWebSearchRequested(request protocol.ParsedRequest) bool {
	for _, tool := range request.Context.Tools {
		if tool.HostedWebSearch {
			return true
		}
	}
	return false
}

func (h *handler) webSearchConfig(providerID, modelID string) websearch.Config {
	cfg := websearch.Config{}
	if h != nil && h.webSearch != nil {
		cfg = h.webSearch[providerID]
	}
	if strings.TrimSpace(cfg.ProviderID) == "" {
		cfg.ProviderID = providerID
	}
	if strings.TrimSpace(cfg.ModelID) == "" {
		cfg.ModelID = modelID
	}
	return cfg
}

func (h *handler) webSearchClient(providerID, modelID string) (*websearch.Client, error) {
	if !h.webSearchSidecarEnabled() {
		return nil, websearch.ErrUnsupportedSelection
	}
	return h.webSearchClientCandidate(providerID, modelID)
}

func cloneWebSearch(in map[string]websearch.Config) map[string]websearch.Config {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]websearch.Config, len(in))
	for id, cfg := range in {
		cloned := cfg
		cloned.AllowedModels = append([]string(nil), cfg.AllowedModels...)
		out[id] = cloned
	}
	return out
}

func writeChatError(w http.ResponseWriter, status int, message, typ, code string) {
	if status < 400 || status > 599 {
		status = http.StatusBadGateway
	}
	if typ == "" {
		if status >= 500 {
			typ = "upstream_error"
		} else {
			typ = "invalid_request_error"
		}
	}
	if code == "" {
		switch status {
		case http.StatusUnauthorized:
			code = "invalid_api_key"
		case http.StatusNotFound:
			code = "model_not_found"
		case http.StatusTooManyRequests:
			code = "rate_limit_exceeded"
		}
	}
	var wireCode any
	if code != "" {
		wireCode = code
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
		"message": message,
		"type":    typ,
		"param":   nil,
		"code":    wireCode,
	}})
}
