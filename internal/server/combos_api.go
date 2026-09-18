package server

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"github.com/Wibias/Benes/internal/config"
)

type comboDiskTarget struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Weight   int    `json:"weight,omitempty"`
}

type comboDiskRecord struct {
	Targets       []comboDiskTarget `json:"targets"`
	Strategy      string            `json:"strategy,omitempty"`
	StickyLimit   int               `json:"stickyLimit,omitempty"`
	DefaultEffort *string           `json:"defaultEffort,omitempty"`
	ImageInput    string            `json:"imageInput,omitempty"`
	Alias         string            `json:"alias,omitempty"`
	NativeAlias   bool              `json:"nativeAlias,omitempty"`
	DisplayName   string            `json:"displayName,omitempty"`
}

func (h *handler) serveCombosAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/combos" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch r.Method {
	case http.MethodGet:
		h.serveCombosGET(w)
		return true
	case http.MethodHead:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	case http.MethodPut:
		h.serveCombosPUT(w, r)
		return true
	case http.MethodDelete:
		h.serveCombosDELETE(w, r)
		return true
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) serveCombosGET(w http.ResponseWriter) {
	rows, err := h.loadComboDTOs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "combos could not be loaded")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"combos": rows})
}

func (h *handler) serveCombosPUT(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	var body struct {
		ID         string          `json:"id"`
		RenameFrom string          `json:"renameFrom"`
		Combo      json.RawMessage `json:"combo"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return
	}
	id := strings.TrimSpace(body.ID)
	if !config.ValidComboID(id) {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid combo id")
		return
	}
	record, err := decodeComboDiskRecord(body.Combo)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	renameFrom := strings.TrimSpace(body.RenameFrom)
	if err := h.validateComboRecord(id, record, renameFrom); err != nil {
		writeError(w, http.StatusBadRequest, err.code, err.message)
		return
	}
	encoded, marshalErr := json.Marshal(record)
	if marshalErr != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "combo could not be stored")
		return
	}
	if err := h.commitConfig(func(tx *config.Transaction) error {
		if renameFrom != "" && renameFrom != id {
			if err := tx.Delete(config.JSONPath("combos", renameFrom)); err != nil {
				return err
			}
		}
		return tx.Set(config.JSONPath("combos", id), encoded)
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "combo could not be stored")
		return
	}
	_ = h.reloadRuntimeCombos()
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *handler) serveCombosDELETE(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !config.ValidComboID(id) {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid combo id")
		return
	}
	if err := h.commitConfig(func(tx *config.Transaction) error {
		return tx.Delete(config.JSONPath("combos", id))
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "combo could not be removed")
		return
	}
	_ = h.reloadRuntimeCombos()
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func decodeComboDiskRecord(raw json.RawMessage) (comboDiskRecord, error) {
	var record comboDiskRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return comboDiskRecord{}, err
	}
	return record, nil
}

type comboAPIError struct {
	code    string
	message string
}

func (e *comboAPIError) Error() string { return e.message }

func (h *handler) validateComboRecord(id string, record comboDiskRecord, renameFrom string) *comboAPIError {
	strategy := strings.TrimSpace(record.Strategy)
	if strategy == "" {
		strategy = "failover"
	}
	// Failover is the only strategy allowed for new writes / conversions.
	// Round-robin and any other unsupported strategy may be re-saved only when
	// the same id (or rename source) already stores that exact strategy
	// (preserve-only; never introduce or convert into them).
	if strategy != "failover" {
		if !h.comboStrategyAlreadyStored(id, renameFrom, strategy) {
			return &comboAPIError{code: "unsupported_strategy", message: "combos are failover only"}
		}
	}
	if len(record.Targets) == 0 {
		return &comboAPIError{code: "no_targets", message: "combo requires at least one target"}
	}
	if strings.EqualFold(id, comboNamespace) {
		return &comboAPIError{code: "reserved_namespace", message: "combo id is reserved"}
	}
	seen := map[string]struct{}{}
	known := h.knownProviderIDs()
	for _, target := range record.Targets {
		provider := strings.TrimSpace(target.Provider)
		model := strings.TrimSpace(target.Model)
		if provider == "" || model == "" {
			return &comboAPIError{code: "incomplete_target", message: "each target needs provider and model"}
		}
		if _, ok := known[provider]; !ok {
			return &comboAPIError{code: "unknown_provider", message: "unknown provider: " + provider}
		}
		key := provider + "/" + model
		if _, dup := seen[key]; dup {
			return &comboAPIError{code: "duplicate_target", message: "duplicate target: " + key}
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (h *handler) comboStrategyAlreadyStored(id, renameFrom, strategy string) bool {
	if strings.TrimSpace(h.configPath) == "" {
		return false
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return false
	}
	records, ok := parseComboRecords(disk.Raw)
	if !ok {
		return false
	}
	for _, key := range []string{strings.TrimSpace(id), strings.TrimSpace(renameFrom)} {
		if key == "" {
			continue
		}
		existing, found := records[key]
		if !found {
			continue
		}
		existingStrategy := strings.TrimSpace(existing.Strategy)
		if existingStrategy == "" {
			existingStrategy = "failover"
		}
		if existingStrategy == strategy {
			return true
		}
	}
	return false
}

func (h *handler) knownProviderIDs() map[string]struct{} {
	out := map[string]struct{}{}
	for id := range h.providers {
		out[id] = struct{}{}
	}
	if strings.TrimSpace(h.configPath) == "" {
		return out
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return out
	}
	for id := range disk.Providers {
		out[id] = struct{}{}
	}
	return out
}

func (h *handler) loadComboDTOs() ([]map[string]any, error) {
	if strings.TrimSpace(h.configPath) != "" {
		disk, err := config.LoadDiskConfig(h.configPath, 0)
		if err != nil {
			return nil, err
		}
		if records, ok := parseComboRecords(disk.Raw); ok {
			return comboDTOsFromRecords(records), nil
		}
	}
	return comboDTOsFromRuntime(h.listRuntimeCombos()), nil
}

func parseComboRecords(raw json.RawMessage) (map[string]comboDiskRecord, bool) {
	var root map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil {
		return nil, false
	}
	blob, ok := root["combos"]
	if !ok || len(blob) == 0 || string(blob) == "null" {
		return nil, false
	}
	var parsed map[string]json.RawMessage
	if json.Unmarshal(blob, &parsed) != nil || parsed == nil {
		return nil, false
	}
	out := make(map[string]comboDiskRecord, len(parsed))
	for id, item := range parsed {
		var record comboDiskRecord
		if json.Unmarshal(item, &record) != nil {
			continue
		}
		out[id] = record
	}
	return out, true
}

func comboDTOsFromRecords(records map[string]comboDiskRecord) []map[string]any {
	ids := make([]string, 0, len(records))
	for id := range records {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, comboDTO(id, records[id]))
	}
	return out
}

func comboDTOsFromRuntime(values []Combo) []map[string]any {
	ids := make([]string, 0, len(values))
	byID := make(map[string]Combo, len(values))
	for _, spec := range values {
		ids = append(ids, spec.ID)
		byID[spec.ID] = spec
	}
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		spec := byID[id]
		targets := make([]map[string]any, 0, len(spec.Targets))
		for _, target := range spec.Targets {
			item := map[string]any{"provider": target.ProviderID, "model": target.Model}
			if target.Protocol != "" {
				item["protocol"] = target.Protocol
			}
			targets = append(targets, item)
		}
		out = append(out, map[string]any{
			"id":       id,
			"model":    "combo/" + id,
			"strategy": "failover",
			"targets":  targets,
		})
	}
	return out
}

func comboDTO(id string, record comboDiskRecord) map[string]any {
	strategy := strings.TrimSpace(record.Strategy)
	if strategy == "" {
		strategy = "failover"
	}
	alias := strings.TrimSpace(record.Alias)
	model := "combo/" + id
	targets := make([]map[string]any, 0, len(record.Targets))
	for _, target := range record.Targets {
		item := map[string]any{
			"provider": strings.TrimSpace(target.Provider),
			"model":    strings.TrimSpace(target.Model),
		}
		if target.Weight > 0 {
			item["weight"] = target.Weight
		}
		targets = append(targets, item)
	}
	row := map[string]any{
		"id":       id,
		"model":    model,
		"strategy": strategy,
		"targets":  targets,
	}
	if alias != "" {
		row["alias"] = alias
	}
	if record.NativeAlias {
		row["nativeAlias"] = true
	}
	if strings.TrimSpace(record.DisplayName) != "" {
		row["displayName"] = strings.TrimSpace(record.DisplayName)
	}
	if record.StickyLimit > 0 {
		row["stickyLimit"] = record.StickyLimit
	}
	if record.DefaultEffort != nil {
		row["defaultEffort"] = record.DefaultEffort
	}
	if record.ImageInput == "disabled" {
		row["imageInput"] = "disabled"
	}
	return row
}

func (h *handler) reloadRuntimeCombos() error {
	if strings.TrimSpace(h.configPath) == "" {
		return nil
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return err
	}
	protocols := map[string]string{}
	for _, existing := range h.listRuntimeCombos() {
		for _, target := range existing.Targets {
			if target.Protocol != "" {
				protocols[target.ProviderID] = target.Protocol
			}
		}
	}
	projection := config.ProjectCombos(disk)
	values := make([]Combo, 0, len(projection.Combos))
	for _, spec := range projection.Combos {
		targets := make([]ComboTarget, 0, len(spec.Targets))
		for _, target := range spec.Targets {
			targets = append(targets, ComboTarget{
				ProviderID: target.ProviderID,
				Model:      target.Model,
				Protocol:   protocols[target.ProviderID],
			})
		}
		values = append(values, Combo{ID: spec.ID, Targets: targets})
	}
	h.replaceRuntimeCombos(values)
	h.reloadSynthesizedCatalog()
	return nil
}

func validProfileID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		if i > 0 && (r == '.' || r == '_' || r == '-') {
			continue
		}
		return false
	}
	return true
}
