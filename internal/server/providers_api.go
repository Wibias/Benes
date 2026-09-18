package server

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentials"
)

//go:embed provider_presets.json
var providerPresetsJSON []byte

var publicOAuthProviders = []string{
	"command-code",
	"xai",
	"anthropic",
	"kimi",
	"nous",
	"kiro",
	"google-antigravity",
	"cursor",
	"github-copilot",
}

type keyLoginProvider struct {
	ID            string `json:"id"`
	Label         string `json:"label,omitempty"`
	Adapter       string `json:"adapter,omitempty"`
	BaseURL       string `json:"baseUrl,omitempty"`
	DashboardURL  string `json:"dashboardUrl,omitempty"`
	DefaultModel  string `json:"defaultModel,omitempty"`
	ResponsesPath string `json:"responsesPath,omitempty"`
}

var (
	keyLoginProvidersOnce sync.Once
	keyLoginProviders     []keyLoginProvider
)

func listKeyLoginProviders() []keyLoginProvider {
	keyLoginProvidersOnce.Do(func() {
		var file struct {
			Providers []struct {
				ID            string `json:"id"`
				Label         string `json:"label"`
				Adapter       string `json:"adapter"`
				BaseURL       string `json:"baseUrl"`
				Auth          string `json:"auth"`
				DashboardURL  string `json:"dashboardUrl"`
				DefaultModel  string `json:"defaultModel"`
				ResponsesPath string `json:"responsesPath"`
			} `json:"providers"`
		}
		if json.Unmarshal(providerPresetsJSON, &file) != nil {
			return
		}
		for _, p := range file.Providers {
			if p.Auth != "key" || p.ID == "" {
				continue
			}
			keyLoginProviders = append(keyLoginProviders, keyLoginProvider{
				ID:            p.ID,
				Label:         p.Label,
				Adapter:       p.Adapter,
				BaseURL:       p.BaseURL,
				DashboardURL:  p.DashboardURL,
				DefaultModel:  p.DefaultModel,
				ResponsesPath: p.ResponsesPath,
			})
		}
	})
	return keyLoginProviders
}

var providerTestDo = defaultProviderTestDo

func defaultProviderTestDo(method, url string) (int, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, nil
}

// providerHealthFailureActive asks the activity log only whether a successful
// manual validation is genuinely recovering an earlier failed validation. It
// does not derive fleet lifecycle here; providersWorkspaceView remains the one
// projection authority for that state.
func (h *handler) providerHealthFailureActive(provider string) bool {
	if h == nil || h.activity == nil {
		return false
	}
	events := h.activity.Recent(64)
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Provider != provider {
			continue
		}
		switch events[i].Type {
		case "provider_health_failure":
			return true
		case "provider_health_recovered":
			return false
		}
	}
	return false
}

func (h *handler) serveProvidersAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/api/oauth/providers" || r.URL.Path == "/api/key-providers" {
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
		if r.URL.Path == "/api/oauth/providers" {
			writeJSON(w, http.StatusOK, map[string]any{"providers": publicOAuthProviders})
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": listKeyLoginProviders()})
		return true
	}
	if r.URL.Path == "/api/provider-presets" {
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(providerPresetsJSON)
		return true
	}
	if r.URL.Path == "/api/providers/test" {
		if !isLoopbackRequestHost(r.Host) {
			return false
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		h.serveProviderTest(w, r)
		return true
	}
	if h.serveProviderKeysAPI(w, r) {
		return true
	}
	if h.serveProvidersWorkspace(w, r) {
		return true
	}
	if r.URL.Path != "/api/providers" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	case http.MethodPost:
		h.serveProvidersPOST(w, r)
		return true
	case http.MethodPatch:
		h.serveProvidersPATCH(w, r)
		return true
	case http.MethodDelete:
		h.serveProvidersDELETE(w, r)
		return true
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	present := map[string]bool{}
	if lister, ok := h.credentials.(credentialLister); ok && lister != nil {
		if refs, err := lister.List(); err == nil {
			for _, ref := range refs {
				present[ref.ID] = true
			}
		}
	}
	names := make([]string, 0, len(h.providers))
	for name := range h.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		item := map[string]any{"name": name, "hasApiKey": present[name]}
		if present[name] {
			item["credential"] = map[string]any{"id": name, "source": credentials.SourceSecureStore}
		}
		out = append(out, item)
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	writeJSON(w, http.StatusOK, out)
	return true
}

func (h *handler) serveProviderTest(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown provider"})
		return
	}
	var rec struct {
		Disabled   bool   `json:"disabled"`
		LiveModels *bool  `json:"liveModels"`
		AuthMode   string `json:"authMode"`
		BaseURL    string `json:"baseUrl"`
	}
	found := false
	if strings.TrimSpace(h.configPath) != "" {
		disk, err := config.LoadDiskConfig(h.configPath, 0)
		if err == nil {
			if raw, ok := disk.Providers[name]; ok {
				found = true
				_ = json.Unmarshal(raw, &rec)
			}
		}
	}
	if !found {
		if _, ok := h.providers[name]; !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown provider"})
			return
		}
	}
	if rec.Disabled {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "Provider is disabled", "latencyMs": 0})
		return
	}
	if strings.EqualFold(rec.AuthMode, "forward") {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        true,
			"latencyMs": 0,
			"message":   "Passthrough provider is configured (forwards your Codex login; no upstream /models).",
		})
		return
	}
	if rec.LiveModels != nil && !*rec.LiveModels {
		writeJSON(w, http.StatusOK, map[string]any{"applicable": false, "reason": "static_catalog", "latencyMs": 0})
		return
	}
	base := strings.TrimRight(strings.TrimSpace(rec.BaseURL), "/")
	if base == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "latencyMs": 0, "error": "provider has no baseUrl"})
		return
	}
	started := time.Now()
	status, err := providerTestDo(http.MethodGet, base+"/models")
	latency := time.Since(started).Milliseconds()
	if err != nil {
		h.recordProviderActivity(name, "provider_health_failure", "", "error")
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "latencyMs": latency, "error": "upstream model discovery failed"})
		return
	}
	if status < 200 || status >= 300 {
		h.recordProviderActivity(name, "provider_health_failure", "", "error")
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":        false,
			"latencyMs": latency,
			"error":     fmt.Sprintf("upstream model discovery returned %d", status),
		})
		return
	}
	if h.providerHealthFailureActive(name) {
		h.recordProviderActivity(name, "provider_health_recovered", "", "info")
	}
	h.recordProviderActivity(name, "connection_validated", "", "info")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "latencyMs": latency, "message": "connected"})
}
