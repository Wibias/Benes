package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/modelprobe"
)

func (h *handler) serveModelProbeAPI(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		provider := strings.TrimSpace(r.URL.Query().Get("provider"))
		model := strings.TrimSpace(r.URL.Query().Get("model"))
		if provider == "" || model == "" {
			writeError(w, http.StatusBadRequest, "invalid_query", "provider and model are required")
			return true
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return true
		}
		req, ok := h.probeRequest(provider, false)
		host, slot := "", ""
		if ok {
			host = destinationHost(req.BaseURL)
			slot = req.CredentialSlot
		}
		result := h.probes.Lookup(provider, model, host, slot)
		writeJSON(w, http.StatusOK, result)
		return true
	case http.MethodPost:
		var body struct {
			Provider    string   `json:"provider"`
			Model       string   `json:"model"`
			Models      []string `json:"models"`
			Generate    bool     `json:"generate"`
			Concurrency int      `json:"concurrency"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		if err := dec.Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return true
		}
		provider := strings.TrimSpace(body.Provider)
		if provider == "" {
			writeError(w, http.StatusBadRequest, "invalid_body", "provider is required")
			return true
		}
		req, ok := h.probeRequest(provider, body.Generate)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown provider"})
			return true
		}
		models := append([]string(nil), body.Models...)
		if model := strings.TrimSpace(body.Model); model != "" {
			models = append([]string{model}, models...)
		}
		if len(models) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_body", "model is required")
			return true
		}
		seen := map[string]struct{}{}
		unique := make([]string, 0, len(models))
		for _, model := range models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			unique = append(unique, model)
		}
		if len(unique) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_body", "model is required")
			return true
		}
		if len(unique) == 1 {
			req.Model = unique[0]
			result, err := modelprobe.Probe(r.Context(), req)
			if err != nil {
				if r.Context().Err() != nil {
					writeError(w, http.StatusBadRequest, "canceled", "probe canceled")
					return true
				}
				writeError(w, http.StatusBadRequest, "probe_failed", "probe failed")
				return true
			}
			h.probes.Store(result)
			writeJSON(w, http.StatusOK, result)
			return true
		}
		results := modelprobe.ProbeAll(r.Context(), req, unique, body.Concurrency)
		for _, result := range results {
			if result.TestedAt.IsZero() {
				continue
			}
			h.probes.Store(result)
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
		return true
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) probeRequest(provider string, generate bool) (modelprobe.Request, bool) {
	req := modelprobe.Request{Provider: provider, Generate: generate}
	found := false
	if strings.TrimSpace(h.configPath) != "" {
		disk, err := config.LoadDiskConfig(h.configPath, 0)
		if err == nil {
			if raw, ok := disk.Providers[provider]; ok {
				found = true
				var rec struct {
					Disabled            bool            `json:"disabled"`
					LiveModels          *bool           `json:"liveModels"`
					AuthMode            string          `json:"authMode"`
					BaseURL             string          `json:"baseUrl"`
					APIKey              string          `json:"apiKey"`
					AllowPrivateNetwork bool            `json:"allowPrivateNetwork"`
					Adapter             string          `json:"adapter"`
					CredentialRef       credentials.Ref `json:"credentialRef"`
				}
				_ = json.Unmarshal(raw, &rec)
				req.LiveModels = rec.LiveModels
				req.AuthMode = rec.AuthMode
				if rec.Disabled {
					req.AuthMode = "disabled"
				}
				req.BaseURL = rec.BaseURL
				req.APIKey = rec.APIKey
				req.AllowPrivate = rec.AllowPrivateNetwork
				req.Adapter = rec.Adapter
				req.CredentialSlot = strings.TrimSpace(rec.CredentialRef.ID)
				if req.CredentialSlot == "" {
					req.CredentialSlot = "primary"
				}
				if h.credentials != nil && (strings.TrimSpace(rec.CredentialRef.ID) != "" || rec.CredentialRef.Source != "") {
					if secret, err := h.credentials.Get(rec.CredentialRef); err == nil {
						req.APIKey = string(secret)
					}
				}
			}
		}
	}
	if !found {
		if _, ok := h.providers[provider]; !ok {
			return modelprobe.Request{}, false
		}
	}
	return req, true
}

func destinationHost(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
