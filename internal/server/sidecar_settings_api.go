package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/sidecar"
)

func (h *handler) serveSidecarSettingsAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/sidecar-settings" {
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
		writeJSON(w, http.StatusOK, h.sidecarSettingsView())
		return true
	case http.MethodPut:
		return h.serveSidecarSettingsPUT(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) sidecarSettingsView() map[string]any {
	root := h.loadConfigRoot()
	if root == nil {
		root = map[string]any{}
	}
	ws, _ := root["webSearchSidecar"].(map[string]any)
	vision, _ := root["visionSidecar"].(map[string]any)
	model, _ := ws["model"].(string)
	if model == "" {
		model = "gpt-5.6-luna"
	}
	stream, _ := ws["streamRoutedModelOutput"].(bool)
	visionModel, _ := vision["model"].(string)
	return map[string]any{
		"webSearch": map[string]any{
			"model":                   model,
			"backend":                 ws["backend"],
			"streamRoutedModelOutput": stream,
			"enabled":                 sidecarSectionEnabled(ws),
		},
		"vision": map[string]any{
			"model":                  visionModel,
			"backend":                vision["backend"],
			"enabled":                sidecarSectionEnabled(vision),
			"timeoutMs":              vision["timeoutMs"],
			"maxDescriptionsPerTurn": vision["maxDescriptionsPerTurn"],
			"reasoning":              vision["reasoning"],
		},
		"visionModels": h.visionModelIDs(),
		"backends":     h.sidecarBackends(),
	}
}

func (h *handler) serveSidecarSettingsPUT(w http.ResponseWriter, r *http.Request) bool {
	var body map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || body == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "body must be a JSON object")
		return true
	}
	root := h.loadConfigRoot()
	if root == nil {
		root = map[string]any{}
	}
	ws := copyMap(root["webSearchSidecar"])
	vision := copyMap(root["visionSidecar"])
	if raw, ok := body["webSearch"]; ok {
		var section map[string]json.RawMessage
		if json.Unmarshal(raw, &section) != nil || section == nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "webSearch must be an object")
			return true
		}
		if modelRaw, ok := section["model"]; ok {
			var model string
			if json.Unmarshal(modelRaw, &model) != nil {
				writeError(w, http.StatusBadRequest, "invalid_body", "webSearch.model must be a string")
				return true
			}
			if strings.TrimSpace(model) == "" {
				delete(ws, "model")
			} else {
				ws["model"] = model
			}
		}
		if backendRaw, ok := section["backend"]; ok {
			if string(backendRaw) == "null" {
				delete(ws, "backend")
			} else {
				var backend string
				if json.Unmarshal(backendRaw, &backend) != nil {
					writeError(w, http.StatusBadRequest, "invalid_body", "webSearch.backend must be a string or null")
					return true
				}
				if _, err := sidecar.NormalizeBackend(backend, sidecar.ModalityWebSearch); err != nil {
					writeError(w, http.StatusBadRequest, "invalid_body", "webSearch.backend must be dedicated_search, provider_native_search, openai, anthropic, or null")
					return true
				}
				ws["backend"] = backend
			}
		}
		if streamRaw, ok := section["streamRoutedModelOutput"]; ok {
			var stream bool
			if json.Unmarshal(streamRaw, &stream) != nil {
				writeError(w, http.StatusBadRequest, "invalid_body", "webSearch.streamRoutedModelOutput must be a boolean")
				return true
			}
			if stream {
				ws["streamRoutedModelOutput"] = true
			} else {
				delete(ws, "streamRoutedModelOutput")
			}
		}
		if enabledRaw, ok := section["enabled"]; ok {
			var enabled bool
			if json.Unmarshal(enabledRaw, &enabled) != nil {
				writeError(w, http.StatusBadRequest, "invalid_body", "webSearch.enabled must be a boolean")
				return true
			}
			if enabled {
				delete(ws, "enabled")
			} else {
				ws["enabled"] = false
			}
		}
	}
	if raw, ok := body["vision"]; ok {
		var section map[string]json.RawMessage
		if json.Unmarshal(raw, &section) != nil || section == nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "vision must be an object")
			return true
		}
		if modelRaw, ok := section["model"]; ok {
			var model string
			if json.Unmarshal(modelRaw, &model) != nil {
				writeError(w, http.StatusBadRequest, "invalid_body", "vision.model must be a string")
				return true
			}
			if strings.TrimSpace(model) != "" {
				if err := h.validateVisionDescriber(model); err != nil {
					writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
					return true
				}
			}
			if strings.TrimSpace(model) == "" {
				delete(vision, "model")
			} else {
				vision["model"] = model
			}
		}
		if backendRaw, ok := section["backend"]; ok {
			if string(backendRaw) == "null" {
				delete(vision, "backend")
			} else {
				var backend string
				if json.Unmarshal(backendRaw, &backend) != nil {
					writeError(w, http.StatusBadRequest, "invalid_body", "vision.backend must be a string or null")
					return true
				}
				if _, err := sidecar.NormalizeBackend(backend, sidecar.ModalityVisionDescribe); err != nil {
					writeError(w, http.StatusBadRequest, "invalid_body", "vision.backend must be vision_describe, openai, anthropic, or null")
					return true
				}
				vision["backend"] = backend
			}
		}
		if enabledRaw, ok := section["enabled"]; ok {
			var enabled bool
			if json.Unmarshal(enabledRaw, &enabled) != nil {
				writeError(w, http.StatusBadRequest, "invalid_body", "vision.enabled must be a boolean")
				return true
			}
			if enabled {
				delete(vision, "enabled")
			} else {
				vision["enabled"] = false
			}
		}
	}
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
		return true
	}
	wsPayload, _ := json.Marshal(ws)
	visionPayload, _ := json.Marshal(vision)
	if err := tx.Set(config.JSONPath("webSearchSidecar"), wsPayload); err != nil || tx.Set(config.JSONPath("visionSidecar"), visionPayload) != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "sidecar settings could not be stored")
		return true
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "sidecar settings could not be stored")
		return true
	}
	view := h.sidecarSettingsView()
	view["ok"] = true
	writeJSON(w, http.StatusOK, view)
	return true
}

func (h *handler) visionModelIDs() []string {
	ids := []string{}
	for _, backend := range sidecar.Catalog(h.sidecarBackends(), sidecar.ModalityVisionDescribe) {
		ids = append(ids, backend.ProviderID+"/"+backend.ModelID)
	}
	return ids
}

func (h *handler) sidecarBackends() []sidecar.Candidate {
	out := []sidecar.Candidate{}
	if h == nil {
		return out
	}
	for mapKey, cfg := range h.webSearch {
		class := sidecar.ClassForWire(cfg.Wire)
		if class == "" || !cfg.Enabled {
			continue
		}
		providerID := strings.TrimSpace(cfg.ProviderID)
		if providerID == "" {
			providerID = mapKey
		}
		out = append(out, sidecar.Candidate{
			Modality:    sidecar.ModalityWebSearch,
			Class:       class,
			ProviderID:  providerID,
			Destination: strings.TrimSpace(cfg.Endpoint),
			ModelID:     cfg.ModelID,
			AuthClass:   cfg.AuthClass,
			Wire:        cfg.Wire,
			Citations:   true,
			Proven:      true,
		})
	}
	for _, model := range h.catalogModels {
		provider, id := splitNamespaced(model.ID)
		if id == "" {
			continue
		}
		proven := model.Vision == catalog.CapabilityTrue
		out = append(out, sidecar.Candidate{
			Modality:   sidecar.ModalityVisionDescribe,
			Class:      sidecar.ClassVisionDescribe,
			ProviderID: provider,
			ModelID:    id,
			ImageInput: proven,
			Proven:     proven,
		})
	}
	return out
}

func (h *handler) validateVisionDescriber(id string) error {
	_, err := sidecar.Resolve(sidecar.Selection{
		Modality:   sidecar.ModalityVisionDescribe,
		ModelID:    id,
		Candidates: h.sidecarBackends(),
	})
	if err != nil {
		if err == sidecar.ErrAmbiguousModel {
			return err
		}
		return fmt.Errorf("vision.model cannot describe images")
	}
	return nil
}

func sidecarSectionEnabled(section map[string]any) bool {
	if v, ok := section["enabled"].(bool); ok {
		return v
	}
	return true
}

func copyMap(raw any) map[string]any {
	in, _ := raw.(map[string]any)
	out := map[string]any{}
	for key, value := range in {
		out[key] = value
	}
	return out
}
