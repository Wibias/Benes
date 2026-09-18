package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/authpublic"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/sidecar"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
	"github.com/Wibias/Benes/internal/timeline"
)

const alphaSearchPath = "/v1/alpha/search"

type nativeCodexSearcher interface {
	Search(context.Context, providercontract.DispatchRequest, []byte) (int, http.Header, []byte, error)
}

func nativeCodexSearch(provider Provider) nativeCodexSearcher {
	if !isNativeCodexForward(provider) {
		return nil
	}
	searcher, ok := provider.(nativeCodexSearcher)
	if !ok {
		return nil
	}
	return searcher
}

func (h *handler) handleAlphaSearch(w http.ResponseWriter, r *http.Request, trace *timeline.Trace) {
	body, err := io.ReadAll(io.LimitReader(r.Body, h.maxRequestBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid search request")
		return
	}
	if int64(len(body)) > h.maxRequestBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "invalid_request", "search request is too large")
		return
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		writeError(w, http.StatusBadRequest, "invalid_request", "search request must be a JSON object")
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(trimmed, &fields) != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "search request must be a JSON object")
		return
	}
	turn, err := h.acquireTurn(r.Context(), len(trimmed))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "resource_exhausted", "request resource budget is exhausted")
		return
	}
	if turn != nil {
		defer turn.Close()
	}
	cfg, err := h.selectedWebSearchConfig()
	if err != nil {
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "web_search_unconfigured")
		writeError(w, http.StatusBadRequest, "web_search_unavailable", err.Error())
		return
	}
	decision, err := h.resolveWebSearchActivation(r.Context(), false, true)
	if err != nil || !decision.Enabled {
		writeError(w, http.StatusBadRequest, "web_search_unavailable", "web search sidecar is disabled")
		return
	}
	dispatch := providercontract.DispatchRequest{
		ForwardHeaders:        h.snapshotForwardHeaders(r.Header),
		Turn:                  turn,
		ConfiguredServiceTier: admittedServiceTier(r.Context()),
	}
	if model, _ := rawJSONString(fields["model"]); model != "" {
		if route, parseErr := h.parseRoute(model); parseErr == nil && route.Provider == cfg.ProviderID {
			dispatch.CodexAccountID = route.CodexAccountID
		}
	}
	if strings.EqualFold(strings.TrimSpace(cfg.AuthClass), "forward") {
		provider := h.providers[cfg.ProviderID]
		searcher := nativeCodexSearch(provider)
		if searcher == nil {
			writeError(w, http.StatusBadRequest, "web_search_unavailable", cfg.ProviderID+" cannot relay native Codex search")
			return
		}
		status, header, payload, searchErr := searcher.Search(r.Context(), dispatch, trimmed)
		if searchErr != nil {
			writeAlphaSearchTransportError(w, r.Context(), cfg.ProviderID, searchErr)
			return
		}
		writeNativeCompact(w, status, header, payload)
		if turn != nil {
			turn.MarkCommitted()
		}
		return
	}
	query := alphaSearchQuery(trimmed)
	if query == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "web search query is required")
		return
	}
	modelID := strings.TrimSpace(cfg.ModelID)
	if modelID == "" && len(cfg.AllowedModels) > 0 {
		modelID = cfg.AllowedModels[0]
	}
	_, client, err := h.webSearchClientForRequest(r.Context(), cfg.ProviderID, modelID, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "web_search_unavailable", err.Error())
		return
	}
	result, err := client.Search(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusBadGateway, "web_search_unavailable", cfg.ProviderID+": "+authpublic.Project(err))
		return
	}
	encoded, err := json.Marshal(adaptAlphaSearchResult(result))
	if err != nil {
		writeError(w, http.StatusBadGateway, "web_search_unavailable", "search backend returned a non-JSON result")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(encoded)
	if turn != nil {
		turn.MarkCommitted()
	}
}

func (h *handler) webSearchSidecarEnabled() bool {
	if h == nil {
		return true
	}
	root := h.loadConfigRoot()
	if root == nil {
		return true
	}
	ws, _ := root["webSearchSidecar"].(map[string]any)
	return sidecarSectionEnabled(ws)
}

func (h *handler) selectedWebSearchConfig() (websearch.Config, error) {
	backend := ""
	if root := h.loadConfigRoot(); root != nil {
		if ws, ok := root["webSearchSidecar"].(map[string]any); ok {
			backend, _ = ws["backend"].(string)
		}
	}
	candidates := h.sidecarBackends()
	if len(sidecar.Catalog(candidates, sidecar.ModalityWebSearch)) == 0 {
		return websearch.Config{}, fmt.Errorf("no web search backend is configured")
	}
	got, err := sidecar.Resolve(sidecar.Selection{
		Modality:   sidecar.ModalityWebSearch,
		Backend:    backend,
		Candidates: candidates,
	})
	if err != nil {
		return websearch.Config{}, fmt.Errorf("web search backend is not configured")
	}
	cfg := h.webSearchConfig(got.ProviderID, got.ModelID)
	if strings.TrimSpace(cfg.ProviderID) == "" {
		cfg.ProviderID = got.ProviderID
	}
	return cfg, nil
}

func alphaSearchQuery(raw []byte) string {
	var envelope struct {
		Query    string `json:"query"`
		Q        string `json:"q"`
		Commands struct {
			SearchQuery []struct {
				Q string `json:"q"`
			} `json:"search_query"`
		} `json:"commands"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return ""
	}
	for _, item := range envelope.Commands.SearchQuery {
		if q := strings.TrimSpace(item.Q); q != "" {
			return q
		}
	}
	if q := strings.TrimSpace(envelope.Query); q != "" {
		return q
	}
	return strings.TrimSpace(envelope.Q)
}

func adaptAlphaSearchResult(result websearch.Result) map[string]any {
	results := make([]map[string]any, 0, len(result.Sources))
	for i, source := range result.Sources {
		if strings.TrimSpace(source.URL) == "" {
			continue
		}
		results = append(results, map[string]any{
			"type":   "text_result",
			"ref_id": fmt.Sprintf("turn0search%d", i),
			"url":    source.URL,
			"title":  source.Title,
		})
	}
	return map[string]any{
		"encrypted_output": nil,
		"output":           result.Text,
		"results":          results,
	}
}

func writeAlphaSearchTransportError(w http.ResponseWriter, ctx context.Context, providerID string, err error) {
	if ctx.Err() != nil {
		writeError(w, 499, "client_cancelled", "client cancelled search request")
		return
	}
	if authpublic.IsAuthentication(err) {
		writeError(w, http.StatusUnauthorized, "authentication_error", authpublic.Project(err))
		return
	}
	message := authpublic.Project(err)
	if strings.TrimSpace(providerID) != "" {
		message = providerID + ": " + message
	}
	writeError(w, http.StatusBadGateway, "web_search_unavailable", message)
}
