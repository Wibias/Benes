package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/config"
)

const catalogPath = "/v1/catalog"

type CatalogError struct {
	ID   string
	Code string
}

func cloneCatalogErrors(values []CatalogError) []CatalogError {
	if len(values) == 0 {
		return nil
	}
	out := make([]CatalogError, 0, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value.ID)
		code := strings.TrimSpace(value.Code)
		if id == "" || code == "" {
			continue
		}
		out = append(out, CatalogError{ID: id, Code: code})
	}
	return out
}

func (h *handler) handleCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	payload, etag := h.catalogPayload()
	if h.catalogMaxBytes > 0 && len(payload) > h.catalogMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "catalog_too_large", "catalog projection exceeds the configured byte bound")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("ETag", etag)
	if catalogETagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func catalogETagMatches(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" || etag == "" {
		return false
	}
	for _, part := range strings.Split(header, ",") {
		candidate := strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(candidate), "w/") {
			candidate = strings.TrimSpace(candidate[2:])
		}
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

func foldLogicalOpenAICatalog(disk config.DiskConfig, models []catalog.Model) []catalog.Model {
	logicalOpenAI := false
	for _, provider := range config.FoldLogicalProviders(disk) {
		if provider.ID == config.LogicalOpenAIID && provider.Access.DefaultAccess {
			logicalOpenAI = true
			break
		}
	}
	if !logicalOpenAI {
		return append([]catalog.Model(nil), models...)
	}

	selected := config.OpenAIDefaultConnection(config.OpenAIDefaultAccess(disk))
	out := make([]catalog.Model, 0, len(models))
	for _, model := range models {
		providerID, modelID, qualified := strings.Cut(strings.TrimSpace(model.ID), "/")
		if !qualified || (providerID != config.LogicalOpenAIID && providerID != config.OpenAIAPIConnection) {
			out = append(out, model)
			continue
		}
		if providerID != selected {
			continue
		}
		if selected == config.OpenAIAPIConnection {
			model.ID = config.LogicalOpenAIID + "/" + modelID
		}
		out = append(out, model)
	}
	return out
}

func withoutOpenAICatalogModels(models []catalog.Model) []catalog.Model {
	out := make([]catalog.Model, 0, len(models))
	for _, model := range models {
		providerID, _, qualified := strings.Cut(strings.TrimSpace(model.ID), "/")
		if qualified && (providerID == config.LogicalOpenAIID || providerID == config.OpenAIAPIConnection) {
			continue
		}
		out = append(out, model)
	}
	return out
}

func (h *handler) exposedCatalogModels() []catalog.Model {
	if h == nil {
		return nil
	}
	if strings.TrimSpace(h.configPath) == "" {
		return append([]catalog.Model(nil), h.catalogModels...)
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return withoutOpenAICatalogModels(h.catalogModels)
	}
	return foldLogicalOpenAICatalog(disk, h.catalogModels)
}

func modelOwnedBy(id string) string {
	provider, _, ok := strings.Cut(id, "/")
	if !ok {
		return ""
	}
	if provider == comboNamespace {
		return "openai"
	}
	return provider
}

func modelCapabilities(model catalog.Model) map[string]any {
	caps := map[string]any{
		"context_length":    model.Context.Tokens,
		"output_modalities": []string{"text"},
	}
	putCapabilityBool(caps, "supports_tool_use", model.ToolUse)
	putCapabilityBool(caps, "supports_streaming", model.Streaming)
	putCapabilityBool(caps, "supports_reasoning", model.Reasoning)
	putCapabilityBool(caps, "supports_vision", model.Vision)
	if model.Vision == catalog.CapabilityTrue {
		caps["input_modalities"] = []string{"text", "image"}
	} else if model.Vision == catalog.CapabilityFalse {
		caps["input_modalities"] = []string{"text"}
	}
	if model.Reasoning == catalog.CapabilityTrue && len(model.ReasoningEfforts) > 0 {
		caps["reasoning_effort"] = append([]string(nil), model.ReasoningEfforts...)
	}
	return caps
}

func putCapabilityBool(dst map[string]any, key string, state catalog.CapabilityState) {
	switch state {
	case catalog.CapabilityTrue:
		dst[key] = true
	case catalog.CapabilityFalse:
		dst[key] = false
	}
}

func (h *handler) catalogPayload() ([]byte, string) {
	models := h.exposedCatalogModels()
	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		if !strings.Contains(id, "/") && len(h.providers) == 1 {
			for providerID := range h.providers {
				id = providerID + "/" + id
			}
		}
		ownedBy := modelOwnedBy(id)
		item := map[string]any{
			"id":                       id,
			"object":                   "model",
			"owned_by":                 ownedBy,
			"context_window":           model.Context.Tokens,
			"context_source":           string(model.Context.Source),
			"auto_compact_token_limit": model.AutoCompactTokenLimit,
			"reasoning_efforts":        append([]string(nil), model.ReasoningEfforts...),
			"selectable":               model.Availability.Selectable,
			"capabilities":             modelCapabilities(model),
		}
		if len(model.APITypes) > 0 {
			item["api_types"] = append([]string(nil), model.APITypes...)
		}
		if model.Availability.Reason != "" {
			item["availability_reason"] = model.Availability.Reason
		}
		data = append(data, item)
	}
	root := map[string]any{"object": "list", "data": data}
	if len(h.catalogErrors) > 0 {
		errors := make([]map[string]string, 0, len(h.catalogErrors))
		for _, item := range h.catalogErrors {
			errors = append(errors, map[string]string{"id": item.ID, "code": item.Code})
		}
		root["errors"] = errors
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		encoded = []byte(`{"object":"list","data":[]}`)
	}
	sum := sha256.Sum256(encoded)
	return encoded, `"` + hex.EncodeToString(sum[:]) + `"`
}
