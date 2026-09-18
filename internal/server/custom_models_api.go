package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/config"
)

type customModelRecord struct {
	ID                     string   `json:"id"`
	Provider               string   `json:"provider"`
	ModelID                string   `json:"modelId"`
	DisplayName            string   `json:"displayName,omitempty"`
	ContextWindow          int      `json:"contextWindow,omitempty"`
	InputModalities        []string `json:"inputModalities,omitempty"`
	ReasoningEfforts       []string `json:"reasoningEfforts,omitempty"`
	DefaultReasoningEffort string   `json:"defaultReasoningEffort,omitempty"`
	AddedAt                string   `json:"addedAt"`
}

func (h *handler) serveCustomModelsAPI(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	id := ""
	if strings.HasPrefix(path, "/api/custom-models/") {
		id = strings.TrimPrefix(path, "/api/custom-models/")
		if id == "" || strings.Contains(id, "/") {
			return false
		}
		path = "/api/custom-models/:id"
	} else if path != "/api/custom-models" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch path {
	case "/api/custom-models":
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			if r.Method == http.MethodHead {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				return true
			}
			writeJSON(w, http.StatusOK, h.loadCustomModels())
			return true
		case http.MethodPost:
			return h.serveCustomModelsPOST(w, r)
		default:
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
	default:
		switch r.Method {
		case http.MethodPut:
			return h.serveCustomModelPUT(w, r, id)
		case http.MethodDelete:
			return h.serveCustomModelDELETE(w, id)
		default:
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
	}
}

func (h *handler) serveCustomModelsPOST(w http.ResponseWriter, r *http.Request) bool {
	var body struct {
		Provider        string   `json:"provider"`
		ModelID         string   `json:"modelId"`
		DisplayName     string   `json:"displayName"`
		ContextWindow   int      `json:"contextWindow"`
		InputModalities []string `json:"inputModalities"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	provider := strings.TrimSpace(body.Provider)
	modelID := strings.TrimSpace(body.ModelID)
	if provider == "" || modelID == "" || strings.Contains(provider, "/") {
		writeError(w, http.StatusBadRequest, "invalid_body", "provider and modelId are required")
		return true
	}
	if strings.Contains(body.DisplayName, "/") {
		writeError(w, http.StatusBadRequest, "invalid_body", "displayName must not contain /")
		return true
	}
	if !h.providerConfigured(provider) {
		writeError(w, http.StatusNotFound, "unknown_provider", "provider not configured")
		return true
	}
	existing := h.loadCustomModels()
	slug := provider + "/" + modelID
	for _, model := range existing {
		if model.Provider+"/"+model.ModelID == slug {
			writeError(w, http.StatusConflict, "duplicate_model", "duplicate model")
			return true
		}
	}
	entry := customModelRecord{
		ID:            newCustomModelID(),
		Provider:      provider,
		ModelID:       modelID,
		AddedAt:       time.Now().UTC().Format(time.RFC3339),
		ContextWindow: body.ContextWindow,
	}
	if strings.TrimSpace(body.DisplayName) != "" {
		entry.DisplayName = strings.TrimSpace(body.DisplayName)
	}
	if len(body.InputModalities) > 0 {
		entry.InputModalities = body.InputModalities
	}
	existing = append(existing, entry)
	if !h.storeCustomModels(w, existing) {
		return true
	}
	h.retainCustomModelOnPreset(provider, modelID)
	writeJSON(w, http.StatusCreated, entry)
	return true
}

func (h *handler) serveCustomModelPUT(w http.ResponseWriter, r *http.Request, id string) bool {
	var body struct {
		ModelID         *string  `json:"modelId"`
		DisplayName     *string  `json:"displayName"`
		ContextWindow   *int     `json:"contextWindow"`
		InputModalities []string `json:"inputModalities"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	list := h.loadCustomModels()
	idx := -1
	for i, model := range list {
		if model.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return true
	}
	entry := list[idx]
	if body.ModelID != nil {
		entry.ModelID = strings.TrimSpace(*body.ModelID)
	}
	if body.DisplayName != nil {
		name := strings.TrimSpace(*body.DisplayName)
		if strings.Contains(name, "/") {
			writeError(w, http.StatusBadRequest, "invalid_body", "displayName must not contain /")
			return true
		}
		entry.DisplayName = name
	}
	if body.ContextWindow != nil {
		entry.ContextWindow = *body.ContextWindow
	}
	if body.InputModalities != nil {
		entry.InputModalities = body.InputModalities
	}
	list[idx] = entry
	if !h.storeCustomModels(w, list) {
		return true
	}
	writeJSON(w, http.StatusOK, entry)
	return true
}

func (h *handler) serveCustomModelDELETE(w http.ResponseWriter, id string) bool {
	list := h.loadCustomModels()
	next := make([]customModelRecord, 0, len(list))
	found := false
	for _, model := range list {
		if model.ID == id {
			found = true
			continue
		}
		next = append(next, model)
	}
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return true
	}
	if !h.storeCustomModels(w, next) {
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return true
}

func (h *handler) loadCustomModels() []customModelRecord {
	root := h.loadConfigRoot()
	if root == nil {
		return []customModelRecord{}
	}
	raw, _ := root["customModels"].([]any)
	out := []customModelRecord{}
	encoded, err := json.Marshal(raw)
	if err != nil || json.Unmarshal(encoded, &out) != nil {
		return []customModelRecord{}
	}
	return out
}

func (h *handler) storeCustomModels(w http.ResponseWriter, models []customModelRecord) bool {
	if models == nil {
		models = []customModelRecord{}
	}
	payload, err := json.Marshal(models)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "custom models could not be stored")
		return false
	}
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return false
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
		return false
	}
	if len(models) == 0 {
		_ = tx.Delete(config.JSONPath("customModels"))
	} else if err := tx.Set(config.JSONPath("customModels"), payload); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "custom models could not be stored")
		return false
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "custom models could not be stored")
		return false
	}
	return true
}

func (h *handler) providerConfigured(name string) bool {
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		_, ok := h.providers[name]
		return ok
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		_, ok := h.providers[name]
		return ok
	}
	_, ok := disk.Providers[name]
	return ok
}

func newCustomModelID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	text := hex.EncodeToString(b[:])
	return text[0:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:]
}
