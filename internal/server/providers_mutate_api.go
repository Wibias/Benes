package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentials"
)

func (h *handler) serveProvidersPOST(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(h.configPath) == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "config path is required"})
		return
	}
	var body struct {
		Name     string          `json:"name"`
		Provider json.RawMessage `json:"provider"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if !validDashboardProviderName(name) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid provider name"})
		return
	}
	rec, apiKey, err := sanitizeProviderRecord(body.Provider)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "config could not be opened"})
		return
	}
	if _, exists := disk.Providers[name]; exists {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "provider already exists"})
		return
	}
	encoded, err := json.Marshal(rawMapToAny(rec))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "provider could not be stored"})
		return
	}
	defaultName := diskDefaultProvider(disk)
	if err := h.commitConfig(func(tx *config.Transaction) error {
		if err := tx.Set(config.JSONPath("providers", name), encoded); err != nil {
			return err
		}
		if defaultName == "" {
			payload, err := json.Marshal(name)
			if err != nil {
				return err
			}
			return tx.Set(config.JSONPath("defaultProvider"), payload)
		}
		return nil
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "provider could not be stored"})
		return
	}
	if apiKey != "" {
		if err := h.storeProviderAPIKey(name, apiKey); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "credential was not stored"})
			return
		}
	}
	h.seedNewProviderPreset(name)
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "name": name})
}

func (h *handler) serveProvidersPATCH(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name is required"})
		return
	}
	if strings.TrimSpace(h.configPath) == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "config path is required"})
		return
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || body == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}
	if _, ok := body["setDefault"]; ok {
		if len(body) != 1 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "setDefault cannot be combined with other edits"})
			return
		}
		h.patchSetDefault(w, name)
		return
	}
	if _, ok := body["codexAccountMode"]; ok {
		if len(body) != 1 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "codexAccountMode cannot be combined with other edits"})
			return
		}
		h.patchCodexAccountMode(w, name, body["codexAccountMode"])
		return
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "config could not be opened"})
		return
	}
	raw, ok := disk.Providers[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown provider"})
		return
	}
	if disabled, has := rawBool(body["disabled"]); has && disabled && name == diskDefaultProvider(disk) {
		writeJSON(w, http.StatusConflict, map[string]any{"code": "default_provider_disabled"})
		return
	}
	rec := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &rec) != nil || rec == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "provider could not be stored"})
		return
	}
	apiKey := ""
	if rawKey, ok := body["apiKey"]; ok {
		if json.Unmarshal(rawKey, &apiKey) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid apiKey"})
			return
		}
		apiKey = strings.TrimSpace(apiKey)
		delete(body, "apiKey")
	}
	if err := applyProviderPatch(rec, body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	encoded, err := json.Marshal(rawMapToAny(rec))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "provider could not be stored"})
		return
	}
	if err := h.commitConfig(func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("providers", name), encoded)
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "provider could not be stored"})
		return
	}
	if apiKey != "" {
		if err := h.storeProviderAPIKey(name, apiKey); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "credential was not stored"})
			return
		}
		h.recordProviderActivity(name, "api_key_used", "API key stored", "info")
	}
	if _, ok := body["defaultAccess"]; ok {
		h.recordProviderActivity(name, "default_access_changed", "", "info")
	}
	if _, ok := body["requestPacing"]; ok {
		h.recordProviderActivity(name, "configuration_changed", "API request pacing updated", "info")
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *handler) serveProvidersDELETE(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name is required"})
		return
	}
	if strings.TrimSpace(h.configPath) == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "config path is required"})
		return
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "config could not be opened"})
		return
	}
	if _, ok := disk.Providers[name]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown provider"})
		return
	}
	if len(disk.Providers) <= 1 {
		writeJSON(w, http.StatusConflict, map[string]any{"code": "last_provider"})
		return
	}
	if name == diskDefaultProvider(disk) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "cannot remove the default provider"})
		return
	}
	if combos := comboDependents(disk, name); len(combos) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{"code": "provider_has_dependent_combos", "combos": combos})
		return
	}
	h.purgeRemovedProviderSecrets(name)
	if err := h.commitConfig(func(tx *config.Transaction) error {
		return tx.Delete(config.JSONPath("providers", name))
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "provider could not be removed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *handler) patchSetDefault(w http.ResponseWriter, name string) {
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "config could not be opened"})
		return
	}
	raw, ok := disk.Providers[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown provider"})
		return
	}
	var rec struct {
		Disabled bool `json:"disabled"`
	}
	_ = json.Unmarshal(raw, &rec)
	if rec.Disabled {
		writeJSON(w, http.StatusConflict, map[string]any{"code": "default_provider_disabled"})
		return
	}
	encoded, err := json.Marshal(name)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "default provider could not be stored"})
		return
	}
	if err := h.commitConfig(func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("defaultProvider"), encoded)
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "default provider could not be stored"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *handler) patchCodexAccountMode(w http.ResponseWriter, name string, raw json.RawMessage) {
	if name != "openai" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "codexAccountMode is only valid for openai"})
		return
	}
	var mode string
	if json.Unmarshal(raw, &mode) != nil || (mode != "direct" && mode != "pool") {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "codexAccountMode must be pool or direct"})
		return
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "config could not be opened"})
		return
	}
	body, ok := disk.Providers[name]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown provider"})
		return
	}
	rec := map[string]json.RawMessage{}
	if json.Unmarshal(body, &rec) != nil || rec == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "provider could not be stored"})
		return
	}
	encodedMode, err := json.Marshal(mode)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "provider could not be stored"})
		return
	}
	rec["codexAccountMode"] = encodedMode
	encoded, err := json.Marshal(rawMapToAny(rec))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "provider could not be stored"})
		return
	}
	if err := h.commitConfig(func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("providers", name), encoded)
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "provider could not be stored"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *handler) commitConfig(write func(*config.Transaction) error) error {
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		return err
	}
	if err := write(tx); err != nil {
		return err
	}
	_, err = tx.Commit()
	return err
}

func (h *handler) storeProviderAPIKey(name, key string) error {
	if h == nil || h.credentials == nil {
		return credentials.ErrCredentialUnavailable
	}
	ref, err := h.credentials.Put(name, []byte(key))
	if err != nil {
		return err
	}
	return h.publishProviderCredentialRef(name, ref)
}

func applyProviderPatch(rec map[string]json.RawMessage, patch map[string]json.RawMessage) error {
	for key, raw := range patch {
		switch key {
		case "adapter", "baseUrl", "defaultModel", "authMode", "note", "apiKeyTransport":
			text := ""
			if json.Unmarshal(raw, &text) != nil {
				return errInvalidProviderField(key)
			}
			text = strings.TrimSpace(text)
			if text == "" {
				delete(rec, key)
				continue
			}
			encoded, err := json.Marshal(text)
			if err != nil {
				return err
			}
			rec[key] = encoded
		case "newModelPolicy":
			text := ""
			if json.Unmarshal(raw, &text) != nil {
				return errInvalidProviderField(key)
			}
			text = strings.TrimSpace(text)
			if text == "" {
				delete(rec, key)
				continue
			}
			if text != "on" && text != "off" {
				return errInvalidProviderField(key)
			}
			encoded, err := json.Marshal(text)
			if err != nil {
				return err
			}
			rec[key] = encoded
		case "defaultAccess":
			text := ""
			if json.Unmarshal(raw, &text) != nil {
				return errInvalidProviderField(key)
			}
			text = strings.TrimSpace(text)
			if text == "" {
				delete(rec, key)
				continue
			}
			if text != config.DefaultAccessOAuth && text != config.DefaultAccessAPI {
				return errInvalidProviderField(key)
			}
			encoded, err := json.Marshal(text)
			if err != nil {
				return err
			}
			rec[key] = encoded
		case "disabled", "allowPrivateNetwork", "liveModels":
			var value bool
			if json.Unmarshal(raw, &value) != nil {
				return errInvalidProviderField(key)
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return err
			}
			rec[key] = encoded
		case "requestPacing":
			if err := applyRequestPacingPatch(rec, raw); err != nil {
				return err
			}
		case "contextWindow":
			if isJSONNull(raw) {
				delete(rec, key)
				continue
			}
			var n float64
			if json.Unmarshal(raw, &n) != nil || n < 0 {
				return errInvalidProviderField(key)
			}
			rec[key] = append(json.RawMessage(nil), raw...)
		case "modelContextWindows":
			if err := applyModelContextWindowsPatch(rec, raw); err != nil {
				return err
			}
		default:
			// Ignore dashboard-unknown keys rather than persist fields that skip projection.
		}
	}
	return nil
}

func applyRequestPacingPatch(rec map[string]json.RawMessage, raw json.RawMessage) error {
	if isJSONNull(raw) {
		delete(rec, "requestPacing")
		delete(rec, "requestPacingMs")
		return nil
	}
	ms, ok := requestPacingMinIntervalFromPatch(raw)
	if !ok {
		return errInvalidProviderField("requestPacing")
	}
	rec["requestPacing"] = append(json.RawMessage(nil), raw...)
	if ms > 0 {
		encoded, err := json.Marshal(ms)
		if err != nil {
			return err
		}
		rec["requestPacingMs"] = encoded
		return nil
	}
	delete(rec, "requestPacingMs")
	return nil
}

func requestPacingMinIntervalFromPatch(raw json.RawMessage) (int, bool) {
	var pacing struct {
		Enabled       *bool    `json:"enabled"`
		MinIntervalMs *float64 `json:"minIntervalMs"`
	}
	if json.Unmarshal(raw, &pacing) != nil {
		return 0, false
	}
	if pacing.Enabled != nil && !*pacing.Enabled {
		return 0, true
	}
	if pacing.MinIntervalMs == nil || *pacing.MinIntervalMs <= 0 {
		return 0, true
	}
	return int(*pacing.MinIntervalMs), true
}

func applyModelContextWindowsPatch(rec map[string]json.RawMessage, raw json.RawMessage) error {
	if isJSONNull(raw) {
		delete(rec, "modelContextWindows")
		return nil
	}
	var incoming map[string]*float64
	if json.Unmarshal(raw, &incoming) != nil {
		return errInvalidProviderField("modelContextWindows")
	}
	current := map[string]json.RawMessage{}
	if existing, ok := rec["modelContextWindows"]; ok && json.Unmarshal(existing, &current) != nil {
		current = map[string]json.RawMessage{}
	}
	for model, value := range incoming {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if value == nil {
			delete(current, model)
			continue
		}
		encoded, err := json.Marshal(*value)
		if err != nil {
			return err
		}
		current[model] = encoded
	}
	if len(current) == 0 {
		delete(rec, "modelContextWindows")
		return nil
	}
	encoded, err := json.Marshal(rawMapToAny(current))
	if err != nil {
		return err
	}
	rec["modelContextWindows"] = encoded
	return nil
}

func sanitizeProviderRecord(raw json.RawMessage) (map[string]json.RawMessage, string, error) {
	rec := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &rec) != nil || rec == nil {
		return nil, "", errInvalidProviderField("provider")
	}
	apiKey := ""
	if value, ok := rec["apiKey"]; ok {
		if json.Unmarshal(value, &apiKey) != nil {
			return nil, "", errInvalidProviderField("apiKey")
		}
		apiKey = strings.TrimSpace(apiKey)
		delete(rec, "apiKey")
	}
	delete(rec, "responsesPath")
	adapter := strings.TrimSpace(rawString(rec["adapter"]))
	baseURL := strings.TrimSpace(rawString(rec["baseUrl"]))
	if adapter == "" || baseURL == "" {
		return nil, "", errInvalidProviderField("adapter")
	}
	return rec, apiKey, nil
}

func comboDependents(disk config.DiskConfig, provider string) []string {
	var root struct {
		Combos map[string]struct {
			Targets []struct {
				Provider string `json:"provider"`
			} `json:"targets"`
		} `json:"combos"`
	}
	if json.Unmarshal(disk.Raw, &root) != nil {
		return nil
	}
	ids := make([]string, 0)
	for id, combo := range root.Combos {
		for _, target := range combo.Targets {
			if strings.TrimSpace(target.Provider) == provider {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Strings(ids)
	return ids
}

func diskDefaultProvider(disk config.DiskConfig) string {
	var root struct {
		DefaultProvider string `json:"defaultProvider"`
	}
	_ = json.Unmarshal(disk.Raw, &root)
	return strings.TrimSpace(root.DefaultProvider)
}

func validDashboardProviderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func rawMapToAny(in map[string]json.RawMessage) map[string]any {
	out := make(map[string]any, len(in))
	for key, raw := range in {
		var value any
		if json.Unmarshal(raw, &value) != nil {
			continue
		}
		out[key] = value
	}
	return out
}

func rawString(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return ""
	}
	return text
}

func rawBool(raw json.RawMessage) (bool, bool) {
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return false, false
	}
	return value, true
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

type providerFieldError string

func (e providerFieldError) Error() string { return string(e) }

func errInvalidProviderField(field string) error {
	return providerFieldError("invalid " + field)
}
