package server

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/usage"
	"github.com/Wibias/Benes/internal/usageledger"
)

func (h *handler) serveUsageAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/usage" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	rng := usage.ParseRange(r.URL.Query().Get("range"))
	surface := usage.ParseSurface(r.URL.Query().Get("surface"))
	now := time.Now().UnixMilli()
	query := usage.Query{
		Range:    rng,
		Surface:  surface,
		Provider: r.URL.Query().Get("provider"),
		Model:    r.URL.Query().Get("model"),
		Account:  r.URL.Query().Get("account"),
		Now:      now,
		Location: usage.ParseLocation(r.URL.Query().Get("tz"), r.URL.Query().Get("tzOffsetMinutes")),
		Date:     r.URL.Query().Get("date"),
		Start:    r.URL.Query().Get("start"),
		End:      r.URL.Query().Get("end"),
	}
	if err := usage.ResolveWindow(query); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_range", err.Error())
		return true
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	home := h.resolvedUsageHome()
	table := usage.DefaultTable()
	table.AddOperatorOverlays(h.newPriceOverlaySnapshot().Get())
	summary, err := usage.SummarizeHomeQuery(home, query, table)
	if err != nil {
		if errors.Is(err, usage.ErrInvalidRange) || errors.Is(err, usage.ErrReversedRange) || errors.Is(err, usage.ErrRangeTooLong) {
			writeError(w, http.StatusBadRequest, "invalid_range", err.Error())
			return true
		}
		body := usage.EmptyQuerySummary(query)
		writeJSON(w, http.StatusOK, map[string]any{
			"range":                body.Range,
			"surface":              body.Surface,
			"since":                body.Since,
			"until":                body.Until,
			"generatedAt":          body.GeneratedAt,
			"summary":              body.Summary,
			"days":                 body.Days,
			"models":               body.Models,
			"providers":            body.Providers,
			"accounts":             body.Accounts,
			"historyTruncated":     false,
			"truncatedPrefixBytes": 0,
			"snapshotWindowStart":  nil,
			"snapshotWindowEnd":    nil,
			"surfaceAttribution":   body.SurfaceAttribution,
			"cost":                 body.Cost,
			"error":                "read_failed",
		})
		return true
	}
	writeJSON(w, http.StatusOK, summary)
	return true
}

func usageHomeFromOptions(options Options) string {
	if home := usageledger.HomeFromPath(options.UsageLogPath); home != "" {
		return home
	}
	if strings.TrimSpace(options.ConfigPath) != "" {
		return filepath.Dir(options.ConfigPath)
	}
	return ""
}

func (h *handler) resolvedUsageHome() string {
	if h == nil {
		return ""
	}
	if home := strings.TrimSpace(h.usageHome); home != "" {
		return home
	}
	if home := usageledger.HomeFromPath(h.usageLogPath); home != "" {
		return home
	}
	if strings.TrimSpace(h.configPath) != "" {
		return filepath.Dir(h.configPath)
	}
	if resolved, err := config.ResolvePaths(config.PathOptions{}); err == nil {
		return resolved.Home
	}
	return ""
}
