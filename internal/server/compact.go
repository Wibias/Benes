package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/authpublic"
	"github.com/Wibias/Benes/internal/compaction"
	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/bridge"
	"github.com/Wibias/Benes/internal/responses/parsed"
	requestwire "github.com/Wibias/Benes/internal/responses/request"
	"github.com/Wibias/Benes/internal/router"
	"github.com/Wibias/Benes/internal/timeline"
)

const compactPath = "/v1/responses/compact"

type nativeCompactor interface {
	SupportsNativeCompact() bool
	Compact(context.Context, providercontract.DispatchRequest, []byte) (int, http.Header, []byte, error)
}

type nativeCodexForwarder interface {
	NativeCodexForward() bool
}

func isNativeCodexForward(provider Provider) bool {
	forwarder, ok := provider.(nativeCodexForwarder)
	return ok && forwarder.NativeCodexForward()
}

func nativeCompact(provider Provider) nativeCompactor {
	compactor, ok := provider.(nativeCompactor)
	if !ok || !compactor.SupportsNativeCompact() {
		return nil
	}
	return compactor
}

func applyRoutedCompaction(request *protocol.ParsedRequest) {
	if request == nil {
		return
	}
	request.Context.Tools = nil
	request.Options.ToolChoice = nil
	request.Options.ParallelToolCalls = nil
	request.Options.TextFormat = nil
	request.StructuredOutput = false
	request.Context.Messages = append(request.Context.Messages, protocol.Message{
		Role:      protocol.RoleUser,
		Content:   []protocol.ContentPart{{Type: protocol.ContentText, Text: compaction.CompactPrompt}},
		Timestamp: time.Now().UnixMilli(),
	})
}

func (h *handler) handleCompact(w http.ResponseWriter, r *http.Request, trace *timeline.Trace) {
	body, err := io.ReadAll(io.LimitReader(r.Body, h.maxRequestBytes+1))
	if err != nil {
		writeCompactError(w, http.StatusBadRequest, "invalid_request_error", "Invalid compaction request body")
		return
	}
	if int64(len(body)) > h.maxRequestBytes {
		writeCompactError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "Invalid compaction request body")
		return
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		writeCompactError(w, http.StatusBadRequest, "invalid_request_error", "Invalid compaction request body")
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		writeCompactError(w, http.StatusBadRequest, "invalid_request_error", "Invalid compaction request body")
		return
	}
	model, _ := rawJSONString(fields["model"])
	if strings.TrimSpace(model) == "" {
		writeCompactError(w, http.StatusBadRequest, "invalid_request_error", "compaction request requires a model")
		return
	}
	route, err := h.parseRoute(model)
	if err != nil {
		if message, ok := ambiguousAliasMessage(err); ok {
			writeCompactError(w, http.StatusBadRequest, "invalid_request_error", message)
			return
		}
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "explicit_route_required")
		writeCompactError(w, http.StatusNotFound, "invalid_request_error", err.Error())
		return
	}
	resolved := h.resolveProvider(route)
	if resolved.MissingCombo {
		writeCompactError(w, http.StatusNotFound, "invalid_request_error", "combo is not configured")
		return
	}
	if resolved.MissingProvider {
		writeCompactError(w, http.StatusNotFound, "invalid_request_error", "provider is not migrated to the Go Responses path")
		return
	}
	provider := resolved.Provider
	turn, err := h.acquireTurn(r.Context(), len(body))
	if err != nil {
		writeCompactError(w, http.StatusServiceUnavailable, "resource_exhausted", "request resource budget is exhausted")
		return
	}
	if turn != nil {
		defer turn.Close()
	}
	dispatch := providercontract.DispatchRequest{
		ForwardHeaders:        h.snapshotForwardHeaders(r.Header),
		CodexAccountID:        route.CodexAccountID,
		Turn:                  turn,
		ConfiguredServiceTier: admittedServiceTier(r.Context()),
	}
	if r.Context().Err() != nil {
		writeCompactError(w, 499, "client_cancelled", "Client cancelled compact request")
		return
	}
	if compactor := nativeCompact(provider); compactor != nil {
		status, header, payload, compactErr := compactor.Compact(r.Context(), dispatch, trimmed)
		if compactErr != nil {
			writeCompactTransportError(w, r.Context(), compactErr)
			return
		}
		writeNativeCompact(w, status, header, payload)
		if turn != nil {
			turn.MarkCommitted()
		}
		return
	}
	h.handleRoutedCompact(w, r, provider, route, fields, dispatch, turn, trace)
}

func (h *handler) handleRoutedCompact(
	w http.ResponseWriter,
	r *http.Request,
	provider Provider,
	route router.Route,
	fields map[string]json.RawMessage,
	dispatch providercontract.DispatchRequest,
	turn *resourcebudget.Turn,
	trace *timeline.Trace,
) {
	wireRequest, err := routedCompactWire(fields)
	if err != nil {
		writeCompactError(w, http.StatusBadRequest, "invalid_request_error", "Invalid compaction request body")
		return
	}
	request, err := parsed.Build(wireRequest, time.Now().UnixMilli())
	if err != nil {
		writeCompactError(w, http.StatusBadRequest, "invalid_request_error", "Invalid compaction request body")
		return
	}
	request.UpstreamModelID = route.Model
	request.Stream = false
	request.CompactionRequest = true
	applyRoutedCompaction(&request)
	dispatch.Parsed = request
	stream, err := provider.Open(r.Context(), dispatch)
	if err != nil {
		trace.Mark(timeline.StageUpstreamWaitHeaders, timeline.SideUpstream, timeline.MilestoneDispatch, false, "provider_open_failed")
		if r.Context().Err() != nil {
			writeCompactError(w, 499, "client_cancelled", "Client cancelled compact request")
			return
		}
		if authpublic.IsAuthentication(err) {
			writeCompactError(w, http.StatusUnauthorized, "authentication_error", authpublic.Project(err))
			return
		}
		writeCompactError(w, http.StatusBadGateway, "upstream_error", publicProviderOpenMessage(err))
		return
	}
	defer stream.Close()
	b := bridge.New(route.Model, bridge.Options{
		Tools:                request.Context.Tools,
		ToolChoice:           request.Options.ToolChoice,
		ParallelToolCalls:    request.Options.ParallelToolCalls,
		HideThinkingSummary:  request.Options.HideThinkingSummary,
		RequestedServiceTier: request.Options.ServiceTier,
		CompactionRequest:    true,
	})
	terminal, status, err := collectTerminal(r.Context(), b, stream)
	if err != nil {
		if r.Context().Err() != nil || errors.Is(err, context.Canceled) {
			writeCompactError(w, 499, "client_cancelled", "Client cancelled compact request")
			return
		}
		writeCompactError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	terminalStatus, _ := terminal["status"].(string)
	if terminalStatus != "completed" {
		if terminalStatus == "failed" || status != http.StatusOK {
			writeCompactError(w, http.StatusBadGateway, "upstream_error", "compaction turn failed")
			return
		}
		writeCompactError(w, http.StatusBadGateway, "upstream_error", "compaction turn did not complete (status: "+firstNonEmptyCompact(terminalStatus, "unknown")+")")
		return
	}
	summary, err := uniqueCompactionSummary(terminal["output"])
	if err != nil {
		writeCompactError(w, http.StatusBadGateway, "invalid_response_error", err.Error())
		return
	}
	output := compaction.BuildV1Output(compaction.ExtractUserMessages(fields["input"]), summary)
	encoded, err := json.Marshal(map[string]any{"output": output})
	if err != nil {
		writeCompactError(w, http.StatusBadGateway, "server_error", "compaction turn returned a non-JSON response")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
	_, _ = w.Write([]byte("\n"))
	if turn != nil {
		turn.MarkCommitted()
	}
}

func routedCompactWire(fields map[string]json.RawMessage) (*requestwire.Request, error) {
	rewritten := make(map[string]json.RawMessage, len(fields)+2)
	for key, value := range fields {
		rewritten[key] = value
	}
	rewritten["stream"] = json.RawMessage("false")
	delete(rewritten, "tools")
	delete(rewritten, "tool_choice")
	delete(rewritten, "text")
	input := json.RawMessage("[]")
	if raw, ok := rewritten["input"]; ok {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) > 0 && trimmed[0] == '[' {
			input = trimmed
		}
	}
	var items []json.RawMessage
	if err := json.Unmarshal(input, &items); err != nil {
		return nil, err
	}
	items = append(items, json.RawMessage(`{"type":"compaction_trigger"}`))
	encodedInput, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	rewritten["input"] = encodedInput
	body, err := json.Marshal(rewritten)
	if err != nil {
		return nil, err
	}
	return requestwire.Decode(bytes.NewReader(body), int64(len(body)+1))
}

func uniqueCompactionSummary(output any) (string, error) {
	items := compactionItems(output)
	found := make([]string, 0, 1)
	for _, item := range items {
		if item["type"] != "compaction" {
			continue
		}
		encrypted, _ := item["encrypted_content"].(string)
		found = append(found, encrypted)
	}
	if len(found) != 1 {
		return "", errors.New("compaction turn produced " + strconv.Itoa(len(found)) + " compaction items, expected exactly 1")
	}
	decoded, ok := compaction.DecodeSummary(found[0])
	if !ok || strings.TrimSpace(decoded) == "" {
		return "", errors.New("compaction turn produced an empty summary")
	}
	return decoded, nil
}

func collectTerminal(ctx context.Context, b *bridge.Bridge, stream EventStream) (map[string]any, int, error) {
	_ = b.Start()
	var terminal map[string]any
	status := http.StatusOK
	for {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		event, err := stream.Next()
		if errors.Is(err, io.EOF) {
			for _, frame := range b.End() {
				if response, ok := frame.Data["response"].(map[string]any); ok {
					terminal = response
				}
			}
			break
		}
		if err != nil {
			frames, _ := b.Handle(protocol.Event{Type: protocol.EventError, Message: "provider stream failed"})
			for _, frame := range frames {
				if response, ok := frame.Data["response"].(map[string]any); ok {
					terminal = response
				}
			}
			status = http.StatusBadGateway
			break
		}
		frames, bridgeErr := b.Handle(event)
		for _, frame := range frames {
			if frame.Name == "response.completed" || frame.Name == "response.incomplete" || frame.Name == "response.failed" {
				if response, ok := frame.Data["response"].(map[string]any); ok {
					terminal = response
				}
				if frame.Name == "response.failed" || frame.Name == "response.incomplete" {
					status = http.StatusBadGateway
				}
			}
		}
		if bridgeErr != nil {
			status = http.StatusBadGateway
			break
		}
		if terminal != nil {
			break
		}
	}
	if terminal == nil {
		return nil, http.StatusBadGateway, errors.New("provider stream did not produce a terminal response")
	}
	return terminal, status, nil
}

func writeCompactTransportError(w http.ResponseWriter, ctx context.Context, err error) {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		writeCompactError(w, 499, "client_cancelled", "Client cancelled compact request")
		return
	}
	if err != nil && strings.Contains(err.Error(), "compact response exceeded 32 MiB") {
		writeCompactError(w, http.StatusBadGateway, "compact_response_too_large", "Compact response exceeded 32 MiB")
		return
	}
	if authpublic.IsAuthentication(err) {
		writeCompactError(w, http.StatusUnauthorized, "authentication_error", authpublic.Project(err))
		return
	}
	writeCompactError(w, http.StatusBadGateway, "upstream_error", "provider request could not be opened")
}

func writeNativeCompact(w http.ResponseWriter, status int, header http.Header, payload []byte) {
	contentType := "application/json"
	if header != nil {
		if value := header.Get("Content-Type"); value != "" {
			contentType = value
		}
		for _, name := range []string{"Retry-After", "X-Codex-Primary-Reset-At", "X-Codex-Secondary-Reset-At", "X-Codex-Tertiary-Reset-At", "Location"} {
			if value := header.Get(name); value != "" {
				w.Header().Set(name, value)
			}
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if status <= 0 {
		status = http.StatusBadGateway
	}
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func writeCompactError(w http.ResponseWriter, status int, typ, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"message": message, "type": typ, "code": typ},
	})
}

func rawJSONString(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", false
	}
	var value string
	if json.Unmarshal(trimmed, &value) != nil {
		return "", false
	}
	return value, true
}

func firstNonEmptyCompact(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func compactionItems(output any) []map[string]any {
	switch v := output.(type) {
	case []map[string]any:
		return v
	case []any:
		items := make([]map[string]any, 0, len(v))
		for _, raw := range v {
			if item, ok := raw.(map[string]any); ok {
				items = append(items, item)
			}
		}
		return items
	default:
		return nil
	}
}
