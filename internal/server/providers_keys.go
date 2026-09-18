package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentials"
)

type persistedAPIKey struct {
	ID      string `json:"id"`
	Label   string `json:"label,omitempty"`
	AddedAt int64  `json:"addedAt,omitempty"`
}

func apiKeyPoolEntryID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])[:8]
}

func maskAPIKey(value string) string {
	if strings.TrimSpace(value) == "" {
		return "stored"
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "****" + value[len(value)-4:]
}

func (h *handler) serveProviderKeysAPI(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/providers/keys", "/api/providers/keys/active", "/api/providers/keys/alias":
	default:
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch r.URL.Path {
	case "/api/providers/keys/active":
		if r.Method != http.MethodPut {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		h.serveProviderKeysActive(w, r)
		return true
	case "/api/providers/keys/alias":
		if r.Method != http.MethodPut {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		h.serveProviderKeysAlias(w, r)
		return true
	}
	switch r.Method {
	case http.MethodPost:
		h.serveProviderKeysPOST(w, r)
	case http.MethodDelete:
		h.serveProviderKeysDELETE(w, r)
	case http.MethodGet, http.MethodHead:
		h.serveProviderKeysGET(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
	}
	return true
}

func (h *handler) serveProviderKeysPOST(w http.ResponseWriter, r *http.Request) {
	if h.credentials == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "credential store is unavailable")
		return
	}
	var body struct {
		Name  string `json:"name"`
		Key   string `json:"key"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid body")
		return
	}
	name := strings.TrimSpace(body.Name)
	key := strings.TrimSpace(body.Key)
	label := strings.TrimSpace(body.Label)
	if name == "" || key == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "name and key are required")
		return
	}
	if strings.ContainsAny(key, "\r\n") {
		writeError(w, http.StatusBadRequest, "invalid_body", "key must not include line breaks")
		return
	}
	if !h.ensureProviderConfigured(name) {
		writeError(w, http.StatusNotFound, "unknown_provider", "unknown provider")
		return
	}
	id := apiKeyPoolEntryID(key)
	ref, err := h.credentials.Put(id, []byte(key))
	if err != nil {
		writeError(w, http.StatusBadRequest, "store_failed", "credential was not stored")
		return
	}
	pool := h.loadAPIKeyPool(name)
	found := false
	now := time.Now().UnixMilli()
	for i := range pool {
		if pool[i].ID == id {
			if label != "" {
				pool[i].Label = label
			}
			found = true
			break
		}
	}
	if !found {
		pool = append(pool, persistedAPIKey{ID: id, Label: label, AddedAt: now})
	}
	if err := h.saveAPIKeyPool(name, pool); err != nil {
		writeError(w, http.StatusInternalServerError, "config_failed", "key pool was not saved")
		return
	}
	if err := h.publishProviderCredentialRef(name, ref); err != nil {
		writeError(w, http.StatusInternalServerError, "config_failed", "credential ref was not published")
		return
	}
	h.recordProviderActivity(name, "api_key_added", "", "info")
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "id": id})
}

func (h *handler) serveProviderKeysDELETE(w http.ResponseWriter, r *http.Request) {
	if h.credentials == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "credential store is unavailable")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if name == "" || id == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "name and id are required")
		return
	}
	if !h.providerConfigured(name) {
		writeError(w, http.StatusNotFound, "unknown_provider", "unknown provider")
		return
	}
	pool := h.loadAPIKeyPool(name)
	next := make([]persistedAPIKey, 0, len(pool))
	found := false
	for _, entry := range pool {
		if entry.ID == id {
			found = true
			continue
		}
		next = append(next, entry)
	}
	if !found && !h.credentialPresent(id) {
		writeError(w, http.StatusNotFound, "key_not_found", "key not found")
		return
	}
	if err := h.credentials.Delete(credentials.Ref{ID: id, Source: credentials.SourceSecureStore}); err != nil {
		writeError(w, http.StatusBadRequest, "store_failed", "credential was not deleted")
		return
	}
	if err := h.saveAPIKeyPool(name, next); err != nil {
		writeError(w, http.StatusInternalServerError, "config_failed", "key pool was not saved")
		return
	}
	active := h.activeCredentialID(name)
	if active == id || active == "" {
		if len(next) == 0 {
			if err := h.unpublishProviderCredentialRef(name); err != nil {
				writeError(w, http.StatusInternalServerError, "config_failed", "credential ref was not unpublished")
				return
			}
		} else {
			ref := credentials.Ref{ID: next[0].ID, Source: credentials.SourceSecureStore}
			if err := h.publishProviderCredentialRef(name, ref); err != nil {
				writeError(w, http.StatusInternalServerError, "config_failed", "credential ref was not published")
				return
			}
		}
	}
	h.recordProviderActivity(name, "api_key_removed", "", "info")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *handler) serveProviderKeysGET(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_name", "name is required")
		return
	}
	if !h.providerConfigured(name) {
		writeError(w, http.StatusNotFound, "unknown_provider", "unknown provider")
		return
	}
	pool := h.loadAPIKeyPool(name)
	if len(pool) == 0 && h.credentialPresent(name) {
		pool = []persistedAPIKey{{ID: name}}
	}
	activeID := h.activeCredentialID(name)
	if activeID == "" {
		if h.credentialPresent(name) {
			activeID = name
		} else if len(pool) == 1 {
			activeID = pool[0].ID
		}
	}
	keys := make([]map[string]any, 0, len(pool))
	for _, entry := range pool {
		masked := "stored"
		if h.credentials != nil {
			if secret, err := h.credentials.Get(credentials.Ref{ID: entry.ID, Source: credentials.SourceSecureStore}); err == nil {
				masked = maskAPIKey(string(secret))
			}
		}
		item := map[string]any{
			"id":     entry.ID,
			"masked": masked,
			"active": entry.ID == activeID,
		}
		if entry.Label != "" {
			item["label"] = entry.Label
		}
		if entry.AddedAt != 0 {
			item["addedAt"] = entry.AddedAt
		}
		keys = append(keys, item)
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"activeId": activeID, "keys": keys})
}

func (h *handler) serveProviderKeysActive(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid body")
		return
	}
	name := strings.TrimSpace(body.Name)
	id := strings.TrimSpace(body.ID)
	if name == "" || id == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "name and id are required")
		return
	}
	if !h.providerConfigured(name) {
		writeError(w, http.StatusNotFound, "unknown_provider", "unknown provider")
		return
	}
	if !h.keyBelongsToProvider(name, id) {
		writeError(w, http.StatusNotFound, "key_not_found", "key not found")
		return
	}
	if err := h.publishProviderCredentialRef(name, credentials.Ref{ID: id, Source: credentials.SourceSecureStore}); err != nil {
		writeError(w, http.StatusInternalServerError, "config_failed", "credential ref was not published")
		return
	}
	h.recordProviderActivity(name, "api_key_selected", "", "info")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name, "activeId": id})
}

func (h *handler) serveProviderKeysAlias(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		ID    string `json:"id"`
		Alias string `json:"alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid body")
		return
	}
	name := strings.TrimSpace(body.Name)
	id := strings.TrimSpace(body.ID)
	alias := strings.TrimSpace(body.Alias)
	if name == "" || id == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "name and id are required")
		return
	}
	if len(alias) > 80 {
		writeError(w, http.StatusBadRequest, "invalid_body", "alias must be at most 80 printable characters")
		return
	}
	for _, r := range alias {
		if r < 32 || r == 127 || unicode.IsControl(r) {
			writeError(w, http.StatusBadRequest, "invalid_body", "alias must be at most 80 printable characters")
			return
		}
	}
	if !h.providerConfigured(name) {
		writeError(w, http.StatusNotFound, "unknown_provider", "unknown provider")
		return
	}
	pool := h.loadAPIKeyPool(name)
	found := false
	for i := range pool {
		if pool[i].ID == id {
			pool[i].Label = alias
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "key_not_found", "key not found")
		return
	}
	if err := h.saveAPIKeyPool(name, pool); err != nil {
		writeError(w, http.StatusInternalServerError, "config_failed", "key pool was not saved")
		return
	}
	outAlias := any(nil)
	if alias != "" {
		outAlias = alias
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name, "id": id, "alias": outAlias})
}

func (h *handler) loadAPIKeyPool(name string) []persistedAPIKey {
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return nil
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return nil
	}
	raw, ok := disk.Providers[name]
	if !ok {
		return nil
	}
	var rec struct {
		Pool []persistedAPIKey `json:"apiKeyPool"`
	}
	if json.Unmarshal(raw, &rec) != nil {
		return nil
	}
	out := make([]persistedAPIKey, 0, len(rec.Pool))
	seen := map[string]struct{}{}
	for _, entry := range rec.Pool {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, persistedAPIKey{ID: id, Label: strings.TrimSpace(entry.Label), AddedAt: entry.AddedAt})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].AddedAt < out[j].AddedAt })
	return out
}

func (h *handler) saveAPIKeyPool(name string, pool []persistedAPIKey) error {
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return nil
	}
	body, err := json.Marshal(pool)
	if err != nil {
		return err
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		return err
	}
	if err := tx.Set(config.JSONPath("providers", name, "apiKeyPool"), body); err != nil {
		return err
	}
	_, err = tx.Commit()
	return err
}

func (h *handler) publishProviderCredentialRef(name string, ref credentials.Ref) error {
	if h == nil || strings.TrimSpace(h.configPath) == "" || strings.TrimSpace(name) == "" {
		return nil
	}
	body, err := json.Marshal(ref)
	if err != nil {
		return err
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		return err
	}
	if err := tx.Set(config.JSONPath("providers", name, "credentialRef"), body); err != nil {
		return err
	}
	_, err = tx.Commit()
	return err
}

func (h *handler) unpublishProviderCredentialRef(name string) error {
	if h == nil || strings.TrimSpace(h.configPath) == "" || strings.TrimSpace(name) == "" {
		return nil
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		return err
	}
	if err := tx.Delete(config.JSONPath("providers", name, "credentialRef")); err != nil {
		return err
	}
	_, err = tx.Commit()
	return err
}

func (h *handler) credentialPresent(id string) bool {
	if h == nil || h.credentials == nil || strings.TrimSpace(id) == "" {
		return false
	}
	if lister, ok := h.credentials.(credentialLister); ok && lister != nil {
		if refs, err := lister.List(); err == nil {
			for _, ref := range refs {
				if ref.ID == id {
					return true
				}
			}
		}
	}
	_, err := h.credentials.Get(credentials.Ref{ID: id, Source: credentials.SourceSecureStore})
	return err == nil
}

func (h *handler) activeCredentialID(name string) string {
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return ""
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return ""
	}
	raw, ok := disk.Providers[name]
	if !ok {
		return ""
	}
	var rec struct {
		Ref credentials.Ref `json:"credentialRef"`
	}
	if json.Unmarshal(raw, &rec) != nil {
		return ""
	}
	return strings.TrimSpace(rec.Ref.ID)
}

func (h *handler) keyBelongsToProvider(name, id string) bool {
	for _, entry := range h.loadAPIKeyPool(name) {
		if entry.ID == id {
			return true
		}
	}
	return id == name && h.credentialPresent(id)
}

func (h *handler) ensureProviderConfigured(name string) bool {
	if h.providerConfigured(name) {
		return true
	}
	if name != config.OpenAIAPIConnection || strings.TrimSpace(h.configPath) == "" {
		return false
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return false
	}
	raw, ok := disk.Providers[config.LogicalOpenAIID]
	if !ok {
		return false
	}
	rec := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &rec) != nil {
		return false
	}
	if !config.CanonicalOpenAIForward(config.LogicalOpenAIID, rec) {
		return false
	}
	encoded, err := json.Marshal(map[string]any{
		"adapter":  string("openai-responses"),
		"baseUrl":  "https://api.openai.com/v1",
		"authMode": "key",
	})
	if err != nil {
		return false
	}
	if err := h.commitConfig(func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("providers", config.OpenAIAPIConnection), encoded)
	}); err != nil {
		return false
	}
	return true
}
