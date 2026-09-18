package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/config"
)

const apiKeyNameMaxRunes = 64

type storedAPIKey struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Key       string `json:"key"`
	CreatedAt string `json:"createdAt"`
}

func (h *handler) serveKeysAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/keys" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch r.Method {
	case http.MethodGet:
		h.serveKeysGET(w, r)
		return true
	case http.MethodHead:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	case http.MethodPost:
		h.serveKeysPOST(w, r)
		return true
	case http.MethodDelete:
		h.serveKeysDELETE(w, r)
		return true
	case http.MethodPatch:
		h.serveKeysPATCH(w, r)
		return true
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) serveKeysGET(w http.ResponseWriter, r *http.Request) {
	keys, err := h.loadStoredAPIKeys()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "api keys could not be loaded")
		return
	}
	claudeCodeEnabled := h.claudeCodeEnabled()
	writeJSON(w, http.StatusOK, h.keysListPayload(r.Host, keys, claudeCodeEnabled))
}

func (h *handler) serveKeysPOST(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return
	}
	name, ok := normalizeAPIKeyName(body.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_name", "name must be at most 64 characters")
		return
	}
	entry := storedAPIKey{
		ID:        newCustomModelID(),
		Name:      name,
		Key:       newDataPlaneAPIKey(),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	keys, err := h.loadStoredAPIKeys()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "api keys could not be loaded")
		return
	}
	keys = append(keys, entry)
	if err := h.saveStoredAPIKeys(keys); err != nil {
		writeError(w, http.StatusInternalServerError, "config_failed", "api key could not be stored")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        entry.ID,
		"name":      entry.Name,
		"key":       entry.Key,
		"createdAt": entry.CreatedAt,
	})
}

func (h *handler) serveKeysDELETE(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return
	}
	id := strings.TrimSpace(body.ID)
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "id is required")
		return
	}
	keys, err := h.loadStoredAPIKeys()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "api keys could not be loaded")
		return
	}
	next := make([]storedAPIKey, 0, len(keys))
	found := false
	for _, key := range keys {
		if key.ID == id {
			found = true
			continue
		}
		next = append(next, key)
	}
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "api key not found")
		return
	}
	if err := h.saveStoredAPIKeys(next); err != nil {
		writeError(w, http.StatusInternalServerError, "config_failed", "api key could not be removed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": id})
}

func (h *handler) serveKeysPATCH(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return
	}
	id := strings.TrimSpace(body.ID)
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "id is required")
		return
	}
	name, ok := normalizeAPIKeyName(body.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_name", "name must be at most 64 characters")
		return
	}
	keys, err := h.loadStoredAPIKeys()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "api keys could not be loaded")
		return
	}
	found := false
	for i, key := range keys {
		if key.ID != id {
			continue
		}
		keys[i].Name = name
		found = true
		break
	}
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "api key not found")
		return
	}
	if err := h.saveStoredAPIKeys(keys); err != nil {
		writeError(w, http.StatusInternalServerError, "config_failed", "api key could not be renamed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": id, "name": name})
}

func (h *handler) keysListPayload(host string, keys []storedAPIKey, claudeCodeEnabled bool) map[string]any {
	counts := map[string]int{}
	for _, key := range keys {
		if key.ID == "" {
			continue
		}
		counts[key.ID]++
	}
	rows := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		if strings.TrimSpace(key.Key) == "" {
			continue
		}
		row := map[string]any{
			"id":        key.ID,
			"name":      key.Name,
			"prefix":    apiKeyPrefix(key.Key),
			"createdAt": key.CreatedAt,
			"usage":     apiKeyUsage(key.ID, counts),
		}
		rows = append(rows, row)
	}
	baseURL, responses := keysPublicEndpoints(host)
	payload := map[string]any{
		"keys":                    rows,
		"claudeCodeEnabled":       claudeCodeEnabled,
		"baseUrl":                 baseURL,
		"endpoint":                responses,
		"responsesEndpoint":       responses,
		"chatCompletionsEndpoint": strings.TrimSuffix(baseURL, "/v1") + "/v1/chat/completions",
		"messagesEndpoint":        strings.TrimSuffix(baseURL, "/v1") + "/v1/messages",
		"modelsEndpoint":          strings.TrimSuffix(baseURL, "/v1") + "/v1/models",
		"authMatrix":              h.keysAuthMatrix(claudeCodeEnabled),
	}
	return payload
}

func (h *handler) keysAuthMatrix(claudeCodeEnabled bool) []map[string]string {
	bearer, dedicated, xAPIKey := h.dataPlaneAuthDispositions()
	endpoints := []string{"/v1/responses", "/v1/chat/completions"}
	if claudeCodeEnabled {
		endpoints = append(endpoints, "/v1/messages")
	}
	endpoints = append(endpoints, "/v1/models")
	rows := make([]map[string]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		rows = append(rows, map[string]string{
			"endpoint":  endpoint,
			"bearer":    bearer,
			"dedicated": dedicated,
			"xApiKey":   xAPIKey,
		})
	}
	return rows
}

func (h *handler) dataPlaneAuthDispositions() (bearer, dedicated, xAPIKey string) {
	switch h.admission.kind {
	case admissionLoopback:
		return "accepted", "accepted", "accepted"
	case admissionRemote:
		return "rejected", "required", "rejected"
	case admissionLegacyBearer:
		return "required", "rejected", "rejected"
	default:
		return "rejected", "rejected", "rejected"
	}
}

func (h *handler) claudeCodeEnabled() bool {
	block := h.loadClaudeCode()
	if v, ok := block["enabled"].(bool); ok {
		return v
	}
	return true
}

func (h *handler) loadStoredAPIKeys() ([]storedAPIKey, error) {
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return nil, nil
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(disk.Raw, &root) != nil {
		return nil, nil
	}
	raw, ok := root["apiKeys"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var keys []storedAPIKey
	if json.Unmarshal(raw, &keys) != nil {
		return nil, nil
	}
	out := make([]storedAPIKey, 0, len(keys))
	for _, key := range keys {
		key.Key = strings.TrimSpace(key.Key)
		if key.Key == "" {
			continue
		}
		out = append(out, key)
	}
	return out, nil
}

func (h *handler) saveStoredAPIKeys(keys []storedAPIKey) error {
	if len(keys) == 0 {
		return h.commitConfig(func(tx *config.Transaction) error {
			return tx.Delete(config.JSONPath("apiKeys"))
		})
	}
	encoded, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	return h.commitConfig(func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("apiKeys"), encoded)
	})
}

func apiKeyUsage(id string, counts map[string]int) map[string]any {
	if id != "" && counts[id] > 1 {
		return map[string]any{"ambiguous": true}
	}
	return map[string]any{"requests7d": 0, "totalRequests": 0}
}

const apiKeyPrefixKeep = 12 // "benes_" + 6 identifying hex

func apiKeyPrefix(key string) string {
	display := strings.Replace(key, "benes_data_", "benes_", 1)
	if len(display) <= apiKeyPrefixKeep {
		return display
	}
	return display[:apiKeyPrefixKeep] + "..."
}

func newDataPlaneAPIKey() string {
	var b [20]byte
	_, _ = rand.Read(b[:])
	return "benes_" + hex.EncodeToString(b[:])
}

func normalizeAPIKeyName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" {
		name = "default"
	}
	if utf8.RuneCountInString(name) > apiKeyNameMaxRunes {
		return "", false
	}
	return name, true
}

func keysPublicEndpoints(hostport string) (baseURL, responses string) {
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		host = "127.0.0.1"
		portStr = "23100"
	}
	port, conv := strconv.Atoi(portStr)
	if conv != nil || port <= 0 {
		port = 23100
	}
	if strings.TrimSpace(host) == "" {
		host = "127.0.0.1"
	}
	root := "http://" + host + ":" + strconv.Itoa(port)
	return root + "/v1", root + "/v1/responses"
}
