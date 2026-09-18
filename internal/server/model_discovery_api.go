package server

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/modeldiscovery"
)

func (h *handler) serveModelDiscoveryAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/model-discovery" {
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
		return h.serveModelDiscoveryGET(w)
	case http.MethodPut:
		return h.serveModelDiscoveryPUT(w, r)
	case http.MethodPost:
		return h.serveModelDiscoverySync(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) serveModelDiscoveryGET(w http.ResponseWriter) bool {
	writeJSON(w, http.StatusOK, h.modelDiscoveryPublic())
	return true
}

func (h *handler) serveModelDiscoveryPUT(w http.ResponseWriter, r *http.Request) bool {
	var body struct {
		NewModelPolicy string `json:"newModelPolicy"`
		Provider       string `json:"provider"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	policy := strings.TrimSpace(body.NewModelPolicy)
	if policy != "on" && policy != "off" {
		writeError(w, http.StatusBadRequest, "invalid_body", "newModelPolicy must be on or off")
		return true
	}
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	provider := strings.TrimSpace(body.Provider)
	if err := h.persistModelDiscovery(func(snap modeldiscovery.Snapshot, catalogs []modeldiscovery.ProviderCatalog) (modeldiscovery.Snapshot, []modeldiscovery.ProviderCatalog) {
		if provider == "" {
			snap.NewModelPolicy = policy
			return snap, catalogs
		}
		for i := range catalogs {
			if catalogs[i].ID == provider {
				catalogs[i].PolicyOverride = policy
			}
		}
		return snap, catalogs
	}, provider); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "model discovery could not be stored")
		return true
	}
	writeJSON(w, http.StatusOK, h.modelDiscoveryPublic())
	return true
}

func (h *handler) modelDiscoveryPublic() map[string]any {
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return map[string]any{"newModelPolicy": "on", "providers": map[string]any{}}
	}
	snap := modeldiscovery.DecodeConfig(disk.Raw)
	disabled := map[string]struct{}{}
	for _, id := range modeldiscovery.DisabledFromConfig(disk.Raw) {
		disabled[id] = struct{}{}
	}
	providers := map[string]any{}
	for _, catalog := range modeldiscovery.CatalogsFromConfig(disk.Providers, disk.Raw, snap) {
		arrivals := make([]map[string]any, 0)
		for _, id := range snap.Shown(catalog.ID) {
			state := "enabled"
			if _, ok := disabled[id]; ok {
				state = "auto-disabled"
			}
			arrivals = append(arrivals, map[string]any{"id": id, "state": state})
		}
		sort.Slice(arrivals, func(i, j int) bool {
			a, _ := arrivals[i]["id"].(string)
			b, _ := arrivals[j]["id"].(string)
			return a < b
		})
		providers[catalog.ID] = map[string]any{
			"policy":   string(modeldiscovery.Resolve(snap.NewModelPolicy, catalog.PolicyOverride)),
			"arrivals": arrivals,
			"skip":     catalog.Skip,
		}
	}
	return map[string]any{
		"newModelPolicy": string(modeldiscovery.Resolve(snap.NewModelPolicy, "")),
		"providers":      providers,
	}
}

func (h *handler) persistModelDiscovery(mutate func(modeldiscovery.Snapshot, []modeldiscovery.ProviderCatalog) (modeldiscovery.Snapshot, []modeldiscovery.ProviderCatalog), writeProviderPolicy string) error {
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return err
	}
	snap := modeldiscovery.DecodeConfig(disk.Raw)
	disabled := modeldiscovery.DisabledFromConfig(disk.Raw)
	catalogs := modeldiscovery.CatalogsFromConfig(disk.Providers, disk.Raw, snap)
	snap, catalogs = mutate(snap, catalogs)
	next, disabledOut, _ := modeldiscovery.Reconcile(snap, catalogs, disabled, time.Time{})
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		return err
	}
	if writeProviderPolicy != "" {
		override := ""
		for _, catalog := range catalogs {
			if catalog.ID == writeProviderPolicy {
				override = catalog.PolicyOverride
				break
			}
		}
		path := config.JSONPath("providers", writeProviderPolicy, "newModelPolicy")
		if override == "" || override == "inherit" {
			if err := tx.Delete(path); err != nil {
				return err
			}
		} else {
			payload, err := json.Marshal(override)
			if err != nil {
				return err
			}
			if err := tx.Set(path, payload); err != nil {
				return err
			}
		}
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := tx.Set(config.JSONPath("modelDiscovery"), encoded); err != nil {
		return err
	}
	if len(disabledOut) == 0 {
		if err := tx.Delete(config.JSONPath("disabledModels")); err != nil {
			return err
		}
	} else {
		payload, err := json.Marshal(disabledOut)
		if err != nil {
			return err
		}
		if err := tx.Set(config.JSONPath("disabledModels"), payload); err != nil {
			return err
		}
	}
	_, err = tx.Commit()
	return err
}
