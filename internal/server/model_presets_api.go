package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/modelpreset"
)

func (h *handler) serveModelPresetsAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/model-presets" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return true
		}
		return h.serveModelPresetsGET(w)
	case http.MethodPut:
		return h.serveModelPresetsPUT(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) serveModelPresetsGET(w http.ResponseWriter) bool {
	root := h.loadConfigRoot()
	providers, _ := root["providers"].(map[string]any)
	out := map[string]any{}
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	for _, name := range names {
		out[name] = h.presetPublicRow(name, root)
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
	return true
}

func (h *handler) serveModelPresetsPUT(w http.ResponseWriter, r *http.Request) bool {
	var body struct {
		Provider string `json:"provider"`
		Mode     string `json:"mode"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	provider := strings.TrimSpace(body.Provider)
	if provider == "" || !h.providerConfigured(provider) {
		writeError(w, http.StatusNotFound, "unknown_provider", "unknown provider")
		return true
	}
	mode := modelpreset.Mode(strings.TrimSpace(body.Mode))
	if mode != modelpreset.ModePreset && mode != modelpreset.ModeAll {
		writeError(w, http.StatusBadRequest, "invalid_body", "mode must be preset or all")
		return true
	}
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	root := h.loadConfigRoot()
	row := h.presetDecision(provider, root)
	result := modelpreset.Apply(modelpreset.Decision{
		Mode:     mode,
		Catalog:  row.catalogIDs,
		Custom:   row.customIDs,
		Previous: row.selected,
		Marker:   row.marker,
		Spec:     row.spec,
	})
	if result.Changed {
		if err := h.persistPresetResult(provider, result); err != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "model preset could not be stored")
			return true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"provider": provider,
		"mode":     result.Marker.Mode,
		"selected": result.Selected,
		"warning":  result.Warning,
		"changed":  result.Changed,
	})
	return true
}

type presetRow struct {
	catalogIDs []string
	customIDs  []string
	selected   []string
	marker     modelpreset.Marker
	spec       modelpreset.Spec
	available  bool
}

func (h *handler) presetDecision(provider string, root map[string]any) presetRow {
	spec, available := modelpreset.Lookup(provider)
	row := presetRow{spec: spec, available: available}
	providers, _ := root["providers"].(map[string]any)
	rec, _ := providers[provider].(map[string]any)
	if rec == nil {
		rec = map[string]any{}
	}
	row.selected = stringList(rec, "selectedModels")
	if raw, ok := rec["modelPreset"].(map[string]any); ok {
		encoded, _ := json.Marshal(raw)
		_ = json.Unmarshal(encoded, &row.marker)
	}
	prefix := provider + "/"
	seen := map[string]struct{}{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if strings.HasPrefix(id, prefix) {
			id = strings.TrimPrefix(id, prefix)
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		row.catalogIDs = append(row.catalogIDs, id)
	}
	// OpenRouter-style ids already contain a slash, so ListCatalogModels does
	// not prefix them with provider/. Collect every catalog id; Match filters.
	for _, model := range h.catalogModels {
		add(model.ID)
	}
	add(asString(rec["defaultModel"]))
	for _, id := range stringList(rec, "models") {
		add(id)
	}
	if custom, ok := root["customModels"].([]any); ok {
		for _, item := range custom {
			entry, _ := item.(map[string]any)
			if asString(entry["provider"]) != provider {
				continue
			}
			id := strings.TrimSpace(asString(entry["modelId"]))
			if id != "" {
				row.customIDs = append(row.customIDs, id)
			}
		}
	}
	return row
}

func (h *handler) presetPublicRow(provider string, root map[string]any) map[string]any {
	row := h.presetDecision(provider, root)
	mode := string(row.marker.Mode)
	if mode == "" {
		if len(row.selected) == 0 {
			mode = string(modelpreset.ModeAll)
		} else {
			mode = string(modelpreset.ModeCustom)
		}
	}
	matched := 0
	if row.available {
		matched = len(modelpreset.Match(row.catalogIDs, row.spec))
	}
	return map[string]any{
		"mode":      mode,
		"available": row.available,
		"version":   row.spec.Version,
		"matched":   matched,
		"catalog":   len(row.catalogIDs),
		"selected":  row.selected,
		"warning":   row.marker.Warning,
	}
}

func (h *handler) persistPresetResult(provider string, result modelpreset.Result) error {
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		return err
	}
	selectedPath := config.JSONPath("providers", provider, "selectedModels")
	if len(result.Selected) == 0 {
		if err := tx.Delete(selectedPath); err != nil {
			return err
		}
	} else {
		payload, err := json.Marshal(result.Selected)
		if err != nil {
			return err
		}
		if err := tx.Set(selectedPath, payload); err != nil {
			return err
		}
	}
	marker, err := json.Marshal(result.Marker)
	if err != nil {
		return err
	}
	if err := tx.Set(config.JSONPath("providers", provider, "modelPreset"), marker); err != nil {
		return err
	}
	_, err = tx.Commit()
	return err
}

func (h *handler) markSelectedModelsCustom(provider string) {
	root := h.loadConfigRoot()
	providers, _ := root["providers"].(map[string]any)
	rec, _ := providers[provider].(map[string]any)
	if rec == nil {
		return
	}
	raw, _ := rec["modelPreset"].(map[string]any)
	mode := modelpreset.Mode(asString(raw["mode"]))
	if !modelpreset.MarkCustom(mode) {
		return
	}
	payload, err := json.Marshal(modelpreset.Marker{Mode: modelpreset.ModeCustom})
	if err != nil {
		return
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		return
	}
	if err := tx.Set(config.JSONPath("providers", provider, "modelPreset"), payload); err != nil {
		return
	}
	_, _ = tx.Commit()
}

func (h *handler) retainCustomModelOnPreset(provider, modelID string) {
	modelID = strings.TrimSpace(modelID)
	if provider == "" || modelID == "" {
		return
	}
	root := h.loadConfigRoot()
	providers, _ := root["providers"].(map[string]any)
	rec, _ := providers[provider].(map[string]any)
	if rec == nil {
		return
	}
	raw, _ := rec["modelPreset"].(map[string]any)
	if modelpreset.Mode(asString(raw["mode"])) != modelpreset.ModePreset {
		return
	}
	selected := stringList(rec, "selectedModels")
	if len(selected) == 0 {
		return
	}
	for _, id := range selected {
		if id == modelID {
			return
		}
	}
	selected = append(selected, modelID)
	payload, err := json.Marshal(selected)
	if err != nil {
		return
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		return
	}
	if err := tx.Set(config.JSONPath("providers", provider, "selectedModels"), payload); err != nil {
		return
	}
	_, _ = tx.Commit()
}

func (h *handler) seedNewProviderPreset(provider string) {
	spec, available := modelpreset.Lookup(provider)
	if !available {
		return
	}
	root := h.loadConfigRoot()
	row := h.presetDecision(provider, root)
	if len(row.selected) > 0 || row.marker.Mode != "" {
		return
	}
	result := modelpreset.Apply(modelpreset.Decision{
		Mode:     modelpreset.ModePreset,
		Catalog:  row.catalogIDs,
		Custom:   row.customIDs,
		Previous: row.selected,
		Marker:   row.marker,
		Spec:     spec,
	})
	if !result.Changed {
		return
	}
	_ = h.persistPresetResult(provider, result)
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}
