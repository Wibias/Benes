package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentials"
)

var publicProviderConfigFields = map[string]struct{}{
	"adapter":             {},
	"baseUrl":             {},
	"defaultModel":        {},
	"models":              {},
	"liveModels":          {},
	"selectedModels":      {},
	"modelPreset":         {},
	"newModelPolicy":      {},
	"authMode":            {},
	"keyOptional":         {},
	"disabled":            {},
	"note":                {},
	"allowPrivateNetwork": {},
	"requestPacingMs":     {},
	"requestPacing":       {},
	"transientRetryOn5xx": {},
	"codexAccountMode":    {},
	"defaultAccess":       {},
	"apiKeyTransport":     {},
	"contextWindow":       {},
	"modelContextWindows": {},
	"reasoningWireFormat": {},
	"freeTier":            {},
}

func (h *handler) serveConfigMutationsAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/config/mutations" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return true
		}
		limit = parsed
	}
	if strings.TrimSpace(h.configPath) == "" {
		writeJSON(w, http.StatusOK, []any{})
		return true
	}
	records, err := config.ListMutations(h.configPath, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_mutations_read_failed", err.Error())
		return true
	}
	if records == nil {
		records = []config.MutationRecord{}
	}
	writeJSON(w, http.StatusOK, records)
	return true
}

func (h *handler) serveConfigAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/config" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	present := h.credentialPresence()
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	if strings.TrimSpace(h.configPath) != "" {
		if disk, err := config.LoadDiskConfig(h.configPath, 0); err == nil {
			writeJSON(w, http.StatusOK, h.publicConfigFromDisk(disk, present))
			return true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": h.memoryProviderConfig(present)})
	return true
}

func (h *handler) publicConfigFromDisk(disk config.DiskConfig, present map[string]bool) map[string]any {
	var root map[string]any
	_ = json.Unmarshal(disk.Raw, &root)
	out := map[string]any{"providers": map[string]any{}}
	if root != nil {
		if port, ok := root["port"]; ok {
			out["port"] = port
		}
		if defaultProvider, ok := root["defaultProvider"]; ok {
			out["defaultProvider"] = defaultProvider
		}
	}
	names := make([]string, 0, len(disk.Providers))
	for name := range disk.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	providers := map[string]any{}
	for _, name := range names {
		providers[name] = publicProviderView(name, disk.Providers[name], present)
	}
	out["providers"] = providers
	return out
}

func (h *handler) memoryProviderConfig(present map[string]bool) map[string]any {
	names := make([]string, 0, len(h.providers))
	for name := range h.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	providers := map[string]any{}
	for _, name := range names {
		providers[name] = publicProviderStub(name, present)
	}
	return providers
}

func publicProviderView(name string, raw json.RawMessage, present map[string]bool) map[string]any {
	item := publicProviderStub(name, present)
	var rec map[string]any
	if json.Unmarshal(raw, &rec) != nil || rec == nil {
		return item
	}
	for key, value := range rec {
		if _, ok := publicProviderConfigFields[key]; !ok {
			continue
		}
		item[key] = value
	}
	if headers, ok := rec["headers"].(map[string]any); ok && len(headers) > 0 {
		item["hasHeaders"] = true
	}
	if hasPlaintextAPIKey(rec) {
		item["hasApiKey"] = true
	}
	if ref, ok := rec["credentialRef"].(map[string]any); ok {
		if id, _ := ref["id"].(string); strings.TrimSpace(id) != "" && present[id] {
			item["hasApiKey"] = true
			item["credential"] = map[string]any{"id": id, "source": credentials.SourceSecureStore}
		}
	}
	return item
}

func publicProviderStub(name string, present map[string]bool) map[string]any {
	item := map[string]any{"hasApiKey": present[name]}
	if present[name] {
		item["credential"] = map[string]any{"id": name, "source": credentials.SourceSecureStore}
	}
	return item
}

func hasPlaintextAPIKey(rec map[string]any) bool {
	text, _ := rec["apiKey"].(string)
	return strings.TrimSpace(text) != ""
}

func (h *handler) credentialPresence() map[string]bool {
	present := map[string]bool{}
	if h == nil {
		return present
	}
	if lister, ok := h.credentials.(credentialLister); ok && lister != nil {
		if refs, err := lister.List(); err == nil {
			for _, ref := range refs {
				present[ref.ID] = true
			}
		}
	}
	return present
}
