package server

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/claude"
	"github.com/Wibias/Benes/internal/config"
)

func (h *handler) serveClaudeCodeAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/claude-code" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		return h.serveClaudeCodeGET(w, r)
	case http.MethodPut:
		return h.serveClaudeCodePUT(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) serveClaudeCodeGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	block := h.loadClaudeCode()
	enabled := true
	if v, ok := block["enabled"].(bool); ok {
		enabled = v
	}
	authMode := "auto"
	if v, ok := block["authMode"].(string); ok && v != "" {
		authMode = v
	}
	autoContext := true
	if v, ok := block["autoContext"].(bool); ok {
		autoContext = v
	}
	injectAgents := true
	if v, ok := block["injectAgents"].(bool); ok {
		injectAgents = v
	}
	available := make([]string, 0, len(h.catalogModels))
	for _, model := range h.catalogModels {
		if model.ID != "" {
			available = append(available, model.ID)
		}
	}
	_, portStr, err := net.SplitHostPort(r.Host)
	port := 23100
	if err == nil {
		if n, conv := strconv.Atoi(portStr); conv == nil && n > 0 {
			port = n
		}
	}
	families := claude.ProjectFamilyMap(claude.ReadFamilyRoutes(block))
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":           enabled,
		"authMode":          authMode,
		"autoContext":       autoContext,
		"injectAgents":      injectAgents,
		"smallFastModel":    stringField(block, "smallFastModel"),
		"model":             stringField(block, "model"),
		"tierModels":        families,
		"modelMap":          families,
		"blockedSkills":     block["blockedSkills"],
		"autoCompactWindow": block["autoCompactWindow"],
		"available":         available,
		"aliases":           []any{},
		"detectionScope":    "daemon",
		"port":              port,
	})
	return true
}

func (h *handler) serveClaudeCodePUT(w http.ResponseWriter, r *http.Request) bool {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	if body == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "body must be an object")
		return true
	}
	for _, field := range []string{"systemEnv", "fastMode", "webSearchSidecar", "visionSidecar"} {
		if _, present := body[field]; present {
			writeError(w, http.StatusBadRequest, "invalid_body", field+" is no longer supported by the Claude Code settings contract")
			return true
		}
	}
	next := h.loadClaudeCode()
	if v, ok := body["enabled"]; ok {
		b, ok := v.(bool)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_body", "enabled must be a boolean")
			return true
		}
		next["enabled"] = b
	}
	if v, ok := body["authMode"]; ok {
		s, ok := v.(string)
		if !ok || (s != "auto" && s != "proxy" && s != "subscription") {
			writeError(w, http.StatusBadRequest, "invalid_body", "authMode must be \"auto\", \"proxy\", or \"subscription\"")
			return true
		}
		if s == "auto" {
			delete(next, "authMode")
		} else {
			next["authMode"] = s
		}
	}
	if v, ok := body["autoContext"]; ok {
		b, ok := v.(bool)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_body", "autoContext must be a boolean")
			return true
		}
		if b {
			delete(next, "autoContext")
		} else {
			next["autoContext"] = false
		}
	}
	if v, ok := body["injectAgents"]; ok {
		b, ok := v.(bool)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_body", "injectAgents must be a boolean")
			return true
		}
		if b {
			delete(next, "injectAgents")
		} else {
			next["injectAgents"] = false
		}
	}
	if v, ok := body["smallFastModel"]; ok {
		s, ok := v.(string)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_body", "smallFastModel must be a string")
			return true
		}
		if s == "" {
			delete(next, "smallFastModel")
		} else {
			next["smallFastModel"] = s
		}
	}
	familySet := false
	var families claude.FamilyRoutes
	if v, ok := body["modelMap"]; ok {
		parsed, err := claude.ParseFamilyObject(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", familyObjectError("modelMap", err))
			return true
		}
		families = parsed
		familySet = true
	}
	if v, ok := body["tierModels"]; ok {
		parsed, err := claude.ParseFamilyObject(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", familyObjectError("tierModels", err))
			return true
		}
		families = parsed
		familySet = true
	}
	if familySet {
		claude.WriteFamilyRoutes(next, families)
	}
	if v, ok := body["blockedSkills"]; ok {
		if v == nil {
			delete(next, "blockedSkills")
		} else {
			arr, ok := v.([]any)
			if !ok {
				writeError(w, http.StatusBadRequest, "invalid_body", "blockedSkills must be an array of non-empty strings, or null")
				return true
			}
			skills := make([]string, 0, len(arr))
			for _, item := range arr {
				s, ok := item.(string)
				if !ok || strings.TrimSpace(s) == "" {
					writeError(w, http.StatusBadRequest, "invalid_body", "blockedSkills must be an array of non-empty strings, or null")
					return true
				}
				skills = append(skills, strings.TrimSpace(s))
			}
			next["blockedSkills"] = skills
		}
	}
	if v, ok := body["autoCompactWindow"]; ok {
		if v == nil {
			delete(next, "autoCompactWindow")
		} else {
			n, ok := v.(float64)
			if !ok || n != float64(int(n)) || n < 100000 || n > 1000000 {
				writeError(w, http.StatusBadRequest, "invalid_body", "autoCompactWindow must be an integer between 100000 and 1000000, or null")
				return true
			}
			next["autoCompactWindow"] = int(n)
		}
	}
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	payload, err := json.Marshal(next)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "claude settings could not be stored")
		return true
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
		return true
	}
	if err := tx.Set(config.JSONPath("claudeCode"), payload); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "claude settings could not be stored")
		return true
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "claude settings could not be stored")
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return true
}

func (h *handler) loadConfigRoot() map[string]any {
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return nil
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return nil
	}
	var root map[string]any
	if json.Unmarshal(disk.Raw, &root) != nil {
		return nil
	}
	return root
}

func (h *handler) loadClaudeCode() map[string]any {
	root := h.loadConfigRoot()
	if root == nil {
		return map[string]any{}
	}
	raw, ok := root["claudeCode"]
	if !ok || raw == nil {
		return map[string]any{}
	}
	block, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	out := map[string]any{}
	for k, v := range block {
		out[k] = v
	}
	return out
}

func familyObjectError(field string, err error) string {
	msg := err.Error()
	if msg == "must be an object" {
		return field + " must be an object"
	}
	return field + "." + msg
}

func stringField(block map[string]any, key string) string {
	v, _ := block[key].(string)
	return v
}
