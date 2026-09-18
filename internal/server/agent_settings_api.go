package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

var codexEfforts = []string{"low", "medium", "high", "xhigh", "max", "ultra"}

func (h *handler) serveAgentSettingsAPI(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/injection-model", "/api/effort-caps", "/api/subagent-models", "/api/subagent-model-fallback":
	default:
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
		switch r.URL.Path {
		case "/api/injection-model":
			return h.serveInjectionModelGET(w)
		case "/api/effort-caps":
			return h.serveEffortCapsGET(w)
		case "/api/subagent-models":
			return h.serveSubagentModelsGET(w)
		default:
			return h.serveSubagentFallbackGET(w)
		}
	case http.MethodPut:
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return true
		}
		switch r.URL.Path {
		case "/api/injection-model":
			return h.serveInjectionModelPUT(w, body)
		case "/api/effort-caps":
			return h.serveEffortCapsPUT(w, body)
		case "/api/subagent-models":
			return h.serveSubagentModelsPUT(w, body)
		default:
			return h.serveSubagentFallbackPUT(w, body)
		}
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) serveInjectionModelGET(w http.ResponseWriter) bool {
	root := h.loadConfigRoot()
	if root == nil {
		root = map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"multiAgentGuidanceEnabled": boolField(root, "multiAgentGuidanceEnabled"),
		"syncCodexSubagentDefaults": boolField(root, "syncCodexSubagentDefaults") && strings.TrimSpace(stringField(root, "injectionModel")) != "",
		"model":                     nullableString(root, "injectionModel"),
		"effort":                    nullableString(root, "injectionEffort"),
		"prompt":                    nullableString(root, "injectionPrompt"),
		"efforts":                   append([]string(nil), codexEfforts...),
		"available":                 h.availableAgentModels(),
	})
	return true
}

func (h *handler) serveInjectionModelPUT(w http.ResponseWriter, raw []byte) bool {
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || body == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "body must be a JSON object")
		return true
	}
	root := h.loadConfigRoot()
	if root == nil {
		root = map[string]any{}
	}
	nextEnabled := boolField(root, "multiAgentGuidanceEnabled")
	nextSync := boolField(root, "syncCodexSubagentDefaults") && strings.TrimSpace(stringField(root, "injectionModel")) != ""
	nextModel := stringField(root, "injectionModel")
	nextEffort := stringField(root, "injectionEffort")
	nextPrompt := stringField(root, "injectionPrompt")
	if raw, ok := body["multiAgentGuidanceEnabled"]; ok {
		if json.Unmarshal(raw, &nextEnabled) != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "multiAgentGuidanceEnabled must be a boolean")
			return true
		}
	}
	if raw, ok := body["syncCodexSubagentDefaults"]; ok {
		if json.Unmarshal(raw, &nextSync) != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "syncCodexSubagentDefaults must be a boolean")
			return true
		}
	}
	if raw, ok := body["model"]; ok {
		value, ok := optionalString(raw)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_body", "model must be a nonblank string or null")
			return true
		}
		nextModel = value
	}
	if raw, ok := body["effort"]; ok {
		value, ok := optionalString(raw)
		if !ok || (value != "" && !isCodexEffort(value)) {
			writeError(w, http.StatusBadRequest, "invalid_body", "unknown reasoning effort")
			return true
		}
		nextEffort = value
	}
	if raw, ok := body["prompt"]; ok {
		value, ok := optionalString(raw)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_body", "prompt must be a string or null")
			return true
		}
		nextPrompt = value
	}
	if nextModel == "" {
		nextEffort = ""
		nextSync = false
	}
	if nextSync && nextModel == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "syncCodexSubagentDefaults requires an injection model")
		return true
	}
	if !h.patchConfig(w, map[string]any{
		"multiAgentGuidanceEnabled": nextEnabled,
		"syncCodexSubagentDefaults": nextSync,
		"injectionModel":            nextModel,
		"injectionEffort":           nextEffort,
		"injectionPrompt":           nextPrompt,
	}, []string{"syncCodexSubagentDefaults", "injectionModel", "injectionEffort", "injectionPrompt"}) {
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                        true,
		"multiAgentGuidanceEnabled": nextEnabled,
		"syncCodexSubagentDefaults": nextSync && nextModel != "",
		"model":                     nullIfEmpty(nextModel),
		"effort":                    nullIfEmpty(nextEffort),
		"prompt":                    nullIfEmpty(nextPrompt),
	})
	return true
}

func (h *handler) serveEffortCapsGET(w http.ResponseWriter) bool {
	root := h.loadConfigRoot()
	writeJSON(w, http.StatusOK, map[string]any{
		"effortCap":         nullableString(root, "effortCap"),
		"subagentEffortCap": nullableString(root, "subagentEffortCap"),
		"efforts":           append([]string(nil), codexEfforts...),
	})
	return true
}

func (h *handler) serveEffortCapsPUT(w http.ResponseWriter, raw []byte) bool {
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || body == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	root := h.loadConfigRoot()
	next := map[string]any{
		"effortCap":         stringField(root, "effortCap"),
		"subagentEffortCap": stringField(root, "subagentEffortCap"),
	}
	for _, key := range []string{"effortCap", "subagentEffortCap"} {
		raw, ok := body[key]
		if !ok {
			continue
		}
		value, ok := optionalString(raw)
		if !ok || (value != "" && !isCodexEffort(value)) {
			writeError(w, http.StatusBadRequest, "invalid_body", "unknown reasoning effort")
			return true
		}
		next[key] = value
	}
	if !h.patchConfig(w, next, []string{"effortCap", "subagentEffortCap"}) {
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"effortCap":         nullIfEmpty(next["effortCap"].(string)),
		"subagentEffortCap": nullIfEmpty(next["subagentEffortCap"].(string)),
	})
	return true
}

func (h *handler) serveSubagentModelsGET(w http.ResponseWriter) bool {
	root := h.loadConfigRoot()
	writeJSON(w, http.StatusOK, map[string]any{
		"chosen":       stringList(root, "subagentModels"),
		"available":    h.availableAgentIDs(),
		"catalogState": map[string]any{},
	})
	return true
}

func (h *handler) serveSubagentModelsPUT(w http.ResponseWriter, raw []byte) bool {
	var body struct {
		Models []string `json:"models"`
	}
	if json.Unmarshal(raw, &body) != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	chosen := make([]string, 0, 5)
	for _, id := range body.Models {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		chosen = append(chosen, id)
		if len(chosen) == 5 {
			break
		}
	}
	if !h.patchConfig(w, map[string]any{"subagentModels": chosen}, nil) {
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "applied": chosen})
	return true
}

func (h *handler) serveSubagentFallbackGET(w http.ResponseWriter) bool {
	root := h.loadConfigRoot()
	poll := 60000
	if v, ok := root["subagentModelFallbackPollMs"].(float64); ok {
		poll = int(v)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"models":    stringList(root, "subagentModelFallback"),
		"pollMs":    poll,
		"available": h.availableAgentIDs(),
	})
	return true
}

func (h *handler) serveSubagentFallbackPUT(w http.ResponseWriter, raw []byte) bool {
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || body == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	root := h.loadConfigRoot()
	nextModels := stringList(root, "subagentModelFallback")
	poll := 60000
	clearPoll := false
	if v, ok := root["subagentModelFallbackPollMs"].(float64); ok {
		poll = int(v)
	}
	if raw, ok := body["models"]; ok {
		var models []any
		if json.Unmarshal(raw, &models) != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "models must be an array")
			return true
		}
		nextModels = []string{}
		for i, entry := range models {
			text, ok := entry.(string)
			if !ok || strings.TrimSpace(text) == "" {
				writeError(w, http.StatusBadRequest, "invalid_body", "models["+strconv.Itoa(i)+"] must be a non-empty string")
				return true
			}
			nextModels = append(nextModels, strings.TrimSpace(text))
		}
	}
	if raw, ok := body["pollMs"]; ok {
		if string(raw) == "null" || string(raw) == `""` {
			clearPoll = true
		} else {
			var next int
			if json.Unmarshal(raw, &next) != nil || next < 5000 || next > 600000 {
				writeError(w, http.StatusBadRequest, "invalid_body", "pollMs must be an integer between 5000 and 600000")
				return true
			}
			poll = next
		}
	}
	values := map[string]any{"subagentModelFallback": nextModels}
	if clearPoll {
		values["subagentModelFallbackPollMs"] = ""
	} else if _, ok := body["pollMs"]; ok {
		values["subagentModelFallbackPollMs"] = poll
	}
	if !h.patchConfig(w, values, []string{"subagentModelFallback", "subagentModelFallbackPollMs"}) {
		return true
	}
	if clearPoll {
		poll = 60000
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "models": nextModels, "pollMs": poll})
	return true
}

func (h *handler) patchConfig(w http.ResponseWriter, values map[string]any, deleteEmpty []string) bool {
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return false
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
		return false
	}
	empty := map[string]struct{}{}
	for _, key := range deleteEmpty {
		empty[key] = struct{}{}
	}
	for key, value := range values {
		if _, drop := empty[key]; drop {
			switch v := value.(type) {
			case string:
				if v == "" {
					_ = tx.Delete(config.JSONPath(key))
					continue
				}
			case bool:
				if !v && key == "syncCodexSubagentDefaults" {
					_ = tx.Delete(config.JSONPath(key))
					continue
				}
			case []string:
				if len(v) == 0 && key == "subagentModelFallback" {
					_ = tx.Delete(config.JSONPath(key))
					continue
				}
			}
		}
		payload, err := json.Marshal(value)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be stored")
			return false
		}
		if err := tx.Set(config.JSONPath(key), payload); err != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be stored")
			return false
		}
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be stored")
		return false
	}
	return true
}

func (h *handler) availableAgentModels() []map[string]string {
	out := make([]map[string]string, 0, len(h.catalogModels))
	disabled := map[string]struct{}{}
	for _, id := range h.loadDisabledModels() {
		disabled[id] = struct{}{}
	}
	for _, model := range h.catalogModels {
		if model.ID == "" {
			continue
		}
		if _, hide := disabled[model.ID]; hide {
			continue
		}
		provider, id := splitNamespaced(model.ID)
		if _, hide := disabled[id]; hide {
			continue
		}
		out = append(out, map[string]string{"provider": provider, "model": id, "namespaced": model.ID})
	}
	return out
}

func (h *handler) availableAgentIDs() []string {
	ids := make([]string, 0, len(h.catalogModels))
	for _, model := range h.availableAgentModels() {
		ids = append(ids, model["namespaced"])
	}
	return ids
}

func isCodexEffort(value string) bool {
	for _, effort := range codexEfforts {
		if effort == value {
			return true
		}
	}
	return false
}

func optionalString(raw json.RawMessage) (string, bool) {
	if string(raw) == "null" {
		return "", true
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	if strings.TrimSpace(value) == "" {
		return "", true
	}
	return value, true
}

func nullableString(root map[string]any, key string) any {
	value := stringField(root, key)
	if value == "" {
		return nil
	}
	return value
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func boolField(root map[string]any, key string) bool {
	if root == nil {
		return false
	}
	v, _ := root[key].(bool)
	return v
}

func stringList(root map[string]any, key string) []string {
	raw, _ := root[key].([]any)
	out := []string{}
	for _, item := range raw {
		text, _ := item.(string)
		if strings.TrimSpace(text) != "" {
			out = append(out, text)
		}
	}
	return out
}
