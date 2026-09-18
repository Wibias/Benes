package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/providers/antigravity"
	"github.com/Wibias/Benes/internal/providers/kiro"
	"github.com/Wibias/Benes/internal/quota"
)

func (h *handler) serveProviderQuotasAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/provider-quotas" {
		return false
	}
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
	force := r.URL.Query().Get("refresh") == "1" || strings.EqualFold(r.URL.Query().Get("refresh"), "true")
	providers, err := loadQuotaProviders(h.configPath, h.authStorePath)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	out := quota.FetchReports(h.quotaStore, providers, force)
	if h.codexQuota != nil {
		out.Reports = mergeQuotaReports(out.Reports, h.codexQuota())
	}
	writeJSON(w, http.StatusOK, out)
	return true
}

func mergeQuotaReports(existing, extra []quota.Report) []quota.Report {
	if len(extra) == 0 {
		return existing
	}
	byProvider := map[string]quota.Report{}
	for _, item := range existing {
		byProvider[item.Identity()] = item
	}
	for _, item := range extra {
		if item.Provider == "" {
			continue
		}
		byProvider[item.Identity()] = item
	}
	names := make([]string, 0, len(byProvider))
	for name := range byProvider {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]quota.Report, 0, len(names))
	for _, name := range names {
		out = append(out, byProvider[name])
	}
	return out
}

func loadQuotaProviders(path, authStorePath string) ([]quota.Provider, error) {
	disk, err := config.LoadDiskConfig(path, 0)
	if err != nil {
		return nil, err
	}
	out := make([]quota.Provider, 0, len(disk.Providers))
	for name, raw := range disk.Providers {
		var rec struct {
			APIKey   string `json:"apiKey"`
			BaseURL  string `json:"baseUrl"`
			AuthMode string `json:"authMode"`
			Disabled bool   `json:"disabled"`
		}
		if json.Unmarshal(raw, &rec) != nil {
			continue
		}
		item := quota.Provider{
			Name:     name,
			APIKey:   rec.APIKey,
			BaseURL:  rec.BaseURL,
			AuthMode: rec.AuthMode,
			Disabled: rec.Disabled,
		}
		if authStorePath != "" {
			if account, ok := antigravity.ActiveStoredAccount(authStorePath, name); ok {
				item.AccessToken = account.Token
				item.ProjectID = account.ProjectID
				item.AccountID = account.ID
				if name == kiro.ProviderID {
					if dest := kiro.RuntimeURL(account.APIRegion); dest != "" {
						item.BaseURL = dest
					}
				}
				if strings.TrimSpace(item.AuthMode) != "forward" {
					item.AuthMode = "oauth"
				}
			}
		}
		out = append(out, item)
	}
	return out, nil
}
