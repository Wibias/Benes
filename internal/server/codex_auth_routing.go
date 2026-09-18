package server

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/config"
)

func (h *handler) serveCodexAuthAccountsPause(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPut {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		ID     string `json:"id"`
		Paused *bool  `json:"paused"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || body.Paused == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "paused must be a boolean")
		return true
	}
	id := strings.TrimSpace(body.ID)
	if id != codexauth.MainAccountID && !codexauth.IsValidManagedAccountID(id) {
		writeError(w, http.StatusBadRequest, "invalid_body", "Invalid account id format")
		return true
	}
	accounts, err := h.loadManagedAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
		return true
	}
	if id != codexauth.MainAccountID && !selectableAccountExists(accounts, id) {
		writeError(w, http.StatusNotFound, "account_not_found", "Account not found")
		return true
	}
	paused := map[string]bool{}
	for key, value := range accounts.PausedAccountIDs {
		paused[key] = value
	}
	if *body.Paused {
		paused[id] = true
	} else {
		delete(paused, id)
	}
	ids := make([]string, 0, len(paused))
	for key, value := range paused {
		if value {
			ids = append(ids, key)
		}
	}
	sort.Strings(ids)
	tx, err := h.beginConfigTx()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	if len(ids) == 0 {
		_ = tx.Delete(config.JSONPath("pausedCodexAccountIds"))
	} else {
		payload, err := json.Marshal(ids)
		if err != nil || tx.Set(config.JSONPath("pausedCodexAccountIds"), payload) != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "pause could not be stored")
			return true
		}
	}
	if *body.Paused && (accounts.ActiveAccountID == id || accounts.PinnedAccountID == id) {
		_ = tx.Delete(config.JSONPath("activeCodexAccountId"))
		_ = tx.Delete(config.JSONPath("activeCodexAccountPinned"))
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "pause could not be stored")
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"id":                   id,
		"paused":               *body.Paused,
		"activeCodexAccountId": h.codexAuthActiveView()["activeCodexAccountId"],
		"appliesImmediately":   true,
	})
	return true
}

func (h *handler) serveCodexAuthPoolStrategy(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPut && r.Method != http.MethodPatch {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Invalid JSON")
		return true
	}
	var parsed map[string]json.RawMessage
	if json.Unmarshal(raw, &parsed) != nil || parsed == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "body must be an object")
		return true
	}
	if _, hasStrategy := parsed["strategy"]; !hasStrategy {
		if _, hasSticky := parsed["stickyLimit"]; !hasSticky {
			if _, hasOrder := parsed["resetOrder"]; !hasOrder {
				writeError(w, http.StatusBadRequest, "invalid_body", "strategy, resetOrder, or stickyLimit required")
				return true
			}
		}
	}
	var body struct {
		Strategy    *string `json:"strategy"`
		ResetOrder  *string `json:"resetOrder"`
		StickyLimit *int    `json:"stickyLimit"`
	}
	if json.Unmarshal(raw, &body) != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Invalid JSON")
		return true
	}
	tx, err := h.beginConfigTx()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	policy, _ := h.loadPoolPolicy()
	strategy := policy.Strategy
	if strategy == "" {
		strategy = codexauth.PoolStrategyQuota
	}
	sticky := policy.StickyLimit
	if sticky == 0 {
		sticky = 1
	}
	if body.Strategy != nil {
		next := codexauth.PoolStrategy(strings.TrimSpace(*body.Strategy))
		switch next {
		case codexauth.PoolStrategyQuota, codexauth.PoolStrategyRoundRobin, codexauth.PoolStrategyFillFirst, codexauth.PoolStrategyResetWindow:
			payload, _ := json.Marshal(string(next))
			if tx.Set(config.JSONPath("accountPoolStrategy"), payload) != nil {
				writeError(w, http.StatusInternalServerError, "config_unreadable", "pool strategy could not be stored")
				return true
			}
			strategy = next
		default:
			writeError(w, http.StatusBadRequest, "invalid_body", "strategy must be one of: quota, round-robin, fill-first, reset-window")
			return true
		}
	}
	order := policy.ResetOrder
	if order == "" {
		order = codexauth.ResetOrderSoonest
	}
	if body.ResetOrder != nil {
		next := codexauth.ResetOrder(strings.TrimSpace(*body.ResetOrder))
		switch next {
		case codexauth.ResetOrderSoonest, codexauth.ResetOrderLatest:
			payload, _ := json.Marshal(string(next))
			if tx.Set(config.JSONPath("accountPoolResetOrder"), payload) != nil {
				writeError(w, http.StatusInternalServerError, "config_unreadable", "pool strategy could not be stored")
				return true
			}
			order = next
		default:
			writeError(w, http.StatusBadRequest, "invalid_body", "resetOrder must be soonest or latest")
			return true
		}
	}
	if body.StickyLimit != nil {
		if *body.StickyLimit < 1 || *body.StickyLimit > 100 {
			writeError(w, http.StatusBadRequest, "invalid_body", "stickyLimit must be an integer 1-100")
			return true
		}
		payload, _ := json.Marshal(*body.StickyLimit)
		if tx.Set(config.JSONPath("accountPoolStickyLimit"), payload) != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "pool strategy could not be stored")
			return true
		}
		sticky = *body.StickyLimit
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "pool strategy could not be stored")
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                     true,
		"accountPoolStrategy":    string(strategy),
		"accountPoolResetOrder":  string(order),
		"accountPoolStickyLimit": sticky,
	})
	return true
}

func (h *handler) serveCodexAuthFailover(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPut {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		Threshold *int `json:"threshold"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || body.Threshold == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Threshold must be an integer 0-20")
		return true
	}
	if *body.Threshold < 0 || *body.Threshold > 20 {
		writeError(w, http.StatusBadRequest, "invalid_body", "Threshold must be an integer 0-20")
		return true
	}
	tx, err := h.beginConfigTx()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	payload, err := json.Marshal(*body.Threshold)
	if err != nil || tx.Set(config.JSONPath("upstreamFailoverThreshold"), payload) != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "failover could not be stored")
		return true
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "failover could not be stored")
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return true
}

func (h *handler) loadPoolPolicy() (codexauth.PoolRoutingPolicy, error) {
	if strings.TrimSpace(h.configPath) == "" {
		return codexauth.PoolRoutingPolicy{}, nil
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return codexauth.PoolRoutingPolicy{}, err
	}
	return codexauth.ProjectPoolRoutingPolicy(disk.Raw)
}
