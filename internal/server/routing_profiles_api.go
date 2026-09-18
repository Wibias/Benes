package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

type routingProfileCandidate struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type routingProfileRequire struct {
	MinContextWindow    float64 `json:"minContextWindow,omitempty"`
	MinQuotaHeadroom    *float64 `json:"minQuotaHeadroom,omitempty"`
	Tools               *bool   `json:"tools,omitempty"`
	ImageInput          *bool   `json:"imageInput,omitempty"`
	StructuredOutput    *bool   `json:"structuredOutput,omitempty"`
	ReasoningEffort     string  `json:"reasoningEffort,omitempty"`
	ServiceTier         string  `json:"serviceTier,omitempty"`
	LocalOnly           *bool   `json:"localOnly,omitempty"`
	RemoteAllowed       *bool   `json:"remoteAllowed,omitempty"`
	EncryptedCodexTasks *bool   `json:"encryptedCodexTasks,omitempty"`
}

type routingProfileRecord struct {
	Alias           string                    `json:"alias,omitempty"`
	Icon            string                    `json:"icon,omitempty"`
	Candidates      []routingProfileCandidate `json:"candidates"`
	Require         routingProfileRequire     `json:"require"`
	Optimize        map[string]any            `json:"optimize,omitempty"`
	Limits          map[string]any            `json:"limits,omitempty"`
	UnknownEvidence map[string]any            `json:"unknownEvidence,omitempty"`
	Compatibility   map[string]any            `json:"compatibility,omitempty"`
}

func (h *handler) serveRoutingProfilesAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/api/routing-profiles/dry-run" {
		return h.serveRoutingProfileDryRun(w, r)
	}
	if r.URL.Path == "/api/routing-analytics" {
		return h.serveRoutingAnalytics(w, r)
	}
	if r.URL.Path != "/api/routing-profiles" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch r.Method {
	case http.MethodGet:
		profiles, err := h.loadRoutingProfileDTOs()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "routing profiles could not be loaded")
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
		return true
	case http.MethodHead:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	case http.MethodPut:
		h.serveRoutingProfilesPUT(w, r)
		return true
	case http.MethodDelete:
		h.serveRoutingProfilesDELETE(w, r)
		return true
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) serveRoutingAnalytics(w http.ResponseWriter, r *http.Request) bool {
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
	profile := strings.TrimSpace(r.URL.Query().Get("profile"))
	if h.policyRuntime == nil {
		writeJSON(w, http.StatusOK, emptyRoutingAnalytics())
		return true
	}
	writeJSON(w, http.StatusOK, h.policyRuntime.analyticsSnapshot(profile))
	return true
}

func emptyRoutingAnalytics() map[string]any {
	return map[string]any{
		"totalRequests":              0,
		"successRate":                nil,
		"fallbackRate":               nil,
		"confidence":                 nil,
		"historyTruncated":           false,
		"cooldownTriggeringFailures": 0,
		"durationMs":                 map[string]any{"sampleCount": 0},
		"firstOutputMs":              map[string]any{"sampleCount": 0, "coverage": nil},
		"breakdown":                  []any{},
	}
}

func (h *handler) serveRoutingProfilesPUT(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	var body struct {
		Mode             string          `json:"mode"`
		ID               string          `json:"id"`
		ExpectedRevision string          `json:"expectedRevision"`
		Profile          json.RawMessage `json:"profile"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return
	}
	id := strings.TrimSpace(body.ID)
	if !validProfileID(id) {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid routing profile id")
		return
	}
	var record routingProfileRecord
	if err := json.Unmarshal(body.Profile, &record); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid profile")
		return
	}
	if len(record.Candidates) == 0 {
		writeError(w, http.StatusBadRequest, "no_candidates", "profile requires at least one candidate")
		return
	}
	if icon, ok := normalizeRoutingProfileIcon(record.Icon); !ok {
		writeError(w, http.StatusBadRequest, "invalid_icon", "unknown routing profile icon")
		return
	} else {
		record.Icon = icon
	}
	known := h.knownProviderIDs()
	for _, candidate := range record.Candidates {
		if strings.TrimSpace(candidate.Provider) == "" || strings.TrimSpace(candidate.Model) == "" {
			writeError(w, http.StatusBadRequest, "incomplete_candidate", "each candidate needs provider and model")
			return
		}
		if _, ok := known[candidate.Provider]; !ok {
			writeError(w, http.StatusBadRequest, "unknown_provider", "unknown provider: "+candidate.Provider)
			return
		}
	}
	if body.Mode == "update" && strings.TrimSpace(body.ExpectedRevision) != "" {
		current, ok := h.routingProfileRaw(id)
		if !ok {
			writeError(w, http.StatusNotFound, "unknown_profile", "unknown routing profile: "+id)
			return
		}
		sum := sha256.Sum256(current)
		if fmt.Sprintf("%x", sum[:8]) != body.ExpectedRevision {
			writeError(w, http.StatusConflict, "revision_conflict", "routing profile was updated elsewhere")
			return
		}
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "routing profile could not be stored")
		return
	}
	if err := h.commitConfig(func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("routingProfiles", id), encoded)
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "routing profile could not be stored")
		return
	}
	h.reloadSynthesizedCatalog()
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *handler) serveRoutingProfilesDELETE(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !validProfileID(id) {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid routing profile id")
		return
	}
	if err := h.commitConfig(func(tx *config.Transaction) error {
		return tx.Delete(config.JSONPath("routingProfiles", id))
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "routing profile could not be removed")
		return
	}
	h.reloadSynthesizedCatalog()
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *handler) serveRoutingProfileDryRun(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		Profile  string         `json:"profile"`
		Evidence map[string]any `json:"evidence"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	id := strings.TrimSpace(body.Profile)
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_profile", "profile is required")
		return true
	}
	record, ok := h.routingProfileRecord(id)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_profile", "unknown routing profile: "+id)
		return true
	}
	raw, _ := h.routingProfileRaw(id)
	sum := sha256.Sum256(raw)
	window, _ := evidenceFloat(body.Evidence, "contextWindow")
	evidence := policyRequestEvidence{
		ContextWindow: int(window),
		Tools:         evidenceBool(body.Evidence, "toolsRequired"),
		Image:         evidenceBool(body.Evidence, "imageInputRequired"),
		Structured:    evidenceBool(body.Evidence, "structuredOutputRequired"),
	}
	_, candidates, selectedIndex := evaluatePolicySelection(policyEvalInput{
		Record:   record,
		Views:    h.policyCandidateViews(record),
		Evidence: evidence,
		Signals:  h.policySignals(),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"candidates":    candidates,
		"selectedIndex": selectedIndex,
		"trace":         map[string]any{"profile": map[string]any{"revision": fmt.Sprintf("%x", sum[:8])}},
	})
	return true
}

func appendUnknown(exclusions []map[string]any, mode, code string) []map[string]any {
	if mode == "allow" {
		return exclusions
	}
	return append(exclusions, map[string]any{"code": code})
}

func evidenceFloat(evidence map[string]any, key string) (float64, bool) {
	if evidence == nil {
		return 0, false
	}
	switch typed := evidence[key].(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	default:
		return 0, false
	}
}

func evidenceBool(evidence map[string]any, key string) bool {
	if evidence == nil {
		return false
	}
	value, _ := evidence[key].(bool)
	return value
}

func boolValue(value *bool) bool {
	return value != nil && *value
}


func floatPtrValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func (h *handler) loadRoutingProfileDTOs() ([]map[string]any, error) {
	parsed, err := h.routingProfileRawMap()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(parsed))
	for id := range parsed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		row, ok := routingProfileDTO(id, parsed[id])
		if !ok {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

func routingProfileDTO(id string, raw json.RawMessage) (map[string]any, bool) {
	var profile map[string]any
	if json.Unmarshal(raw, &profile) != nil {
		return nil, false
	}
	delete(profile, "apiKey")
	alias, _ := profile["alias"].(string)
	model := "policy/" + id
	candidates, _ := profile["candidates"].([]any)
	if candidates == nil {
		candidates = []any{}
	}
	require, _ := profile["require"].(map[string]any)
	if require == nil {
		require = map[string]any{}
	}
	optimize, _ := profile["optimize"].(map[string]any)
	if optimize == nil {
		optimize = map[string]any{"latency": 0.55, "health": 0.25, "cost": 0.1, "quota": 0.1}
	}
	limits, _ := profile["limits"].(map[string]any)
	if limits == nil {
		limits = map[string]any{}
	}
	unknown, _ := profile["unknownEvidence"].(map[string]any)
	if unknown == nil {
		unknown = map[string]any{
			"capability": "allow",
			"health":     "penalize",
			"quota":      "penalize",
			"cost":       "penalize",
		}
	}
	sum := sha256.Sum256(raw)
	row := map[string]any{
		"id":              id,
		"alias":           alias,
		"model":           model,
		"revision":        fmt.Sprintf("%x", sum[:8]),
		"candidates":      candidates,
		"require":         require,
		"optimize":        optimize,
		"limits":          limits,
		"unknownEvidence": unknown,
	}
	if icon, ok := profile["icon"].(string); ok {
		if normalized, valid := normalizeRoutingProfileIcon(icon); valid && normalized != "" {
			row["icon"] = normalized
		}
	}
	if compatibility, ok := profile["compatibility"].(map[string]any); ok {
		row["compatibility"] = compatibility
	}
	return row, true
}

var routingProfileIconIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func normalizeRoutingProfileIcon(icon string) (string, bool) {
	icon = strings.TrimSpace(icon)
	if icon == "" {
		return "", true
	}
	if len(icon) > 64 || !routingProfileIconIDPattern.MatchString(icon) {
		return "", false
	}
	return icon, true
}

func (h *handler) routingProfileRawMap() (map[string]json.RawMessage, error) {
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return map[string]json.RawMessage{}, nil
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if len(disk.Raw) == 0 || json.Unmarshal(disk.Raw, &root) != nil {
		return map[string]json.RawMessage{}, nil
	}
	raw, ok := root["routingProfiles"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return map[string]json.RawMessage{}, nil
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if parsed == nil {
		return map[string]json.RawMessage{}, nil
	}
	return parsed, nil
}

func (h *handler) routingProfileRaw(id string) (json.RawMessage, bool) {
	parsed, err := h.routingProfileRawMap()
	if err != nil {
		return nil, false
	}
	raw, ok := parsed[id]
	return raw, ok
}

func (h *handler) routingProfileRecord(id string) (routingProfileRecord, bool) {
	raw, ok := h.routingProfileRaw(id)
	if !ok {
		return routingProfileRecord{}, false
	}
	var record routingProfileRecord
	if json.Unmarshal(raw, &record) != nil {
		return routingProfileRecord{}, false
	}
	return record, true
}
