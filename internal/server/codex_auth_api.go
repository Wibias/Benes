package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/config"
)

func (h *handler) serveCodexAuthAPI(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/codex-auth/active":
		if r.Method == http.MethodPut {
			return h.serveCodexAuthActivePUT(w, r)
		}
		return h.serveCodexAuthActive(w, r)
	case "/api/codex-auth/auto-switch":
		return h.serveCodexAuthAutoSwitch(w, r)
	case "/api/codex-auth/accounts":
		if r.Method == http.MethodDelete {
			return h.serveCodexAuthAccountsDelete(w, r)
		}
		return h.serveCodexAuthAccountsGET(w, r)
	case "/api/codex-auth/accounts/alias":
		return h.serveCodexAuthAccountsAlias(w, r)
	case "/api/codex-auth/accounts/priority":
		return h.serveCodexAuthAccountsPriority(w, r)
	case "/api/codex-auth/accounts/clear-cooldown":
		return h.serveCodexAuthClearCooldown(w, r)
	case "/api/codex-auth/accounts/pause":
		return h.serveCodexAuthAccountsPause(w, r)
	case "/api/codex-auth/pool-strategy":
		return h.serveCodexAuthPoolStrategy(w, r)
	case "/api/codex-auth/failover":
		return h.serveCodexAuthFailover(w, r)
	case "/api/codex-auth/accounts/pause-exhausted":
		return h.serveCodexAuthPauseExhausted(w, r)
	case "/api/codex-auth/reset-credits":
		return h.serveCodexAuthResetCredits(w, r)
	case "/api/codex-auth/reset-credits/consume":
		return h.serveCodexAuthResetCreditsConsume(w, r)
	case "/api/codex-auth/login":
		return h.serveCodexAuthLogin(w, r)
	case "/api/codex-auth/login/code":
		return h.serveCodexAuthLoginCode(w, r)
	case "/api/codex-auth/login/cancel":
		return h.serveCodexAuthLoginCancel(w, r)
	case "/api/codex-auth/login-status":
		return h.serveCodexAuthLoginStatus(w, r)
	default:
		return false
	}
}

func (h *handler) serveCodexAuthActive(w http.ResponseWriter, r *http.Request) bool {
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
	writeJSON(w, http.StatusOK, h.codexAuthActiveView())
	return true
}

func (h *handler) serveCodexAuthActivePUT(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	var body struct {
		AccountID *string `json:"accountId"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Invalid JSON")
		return true
	}
	accounts, err := h.loadManagedAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
		return true
	}
	target := codexauth.MainAccountID
	if body.AccountID != nil {
		target = strings.TrimSpace(*body.AccountID)
	}
	if body.AccountID != nil && *body.AccountID == codexauth.MainAccountID && hasLegacyMainPoolRow(accounts) {
		writeError(w, http.StatusConflict, "legacy_main_row", "Remove the legacy __main__ pool row before selecting the Desktop account")
		return true
	}
	if accounts.PausedAccountIDs[target] {
		writeError(w, http.StatusConflict, "account_paused", "Account is paused")
		return true
	}
	if body.AccountID != nil && *body.AccountID != codexauth.MainAccountID {
		if !codexauth.IsValidManagedAccountID(*body.AccountID) {
			writeError(w, http.StatusBadRequest, "invalid_body", "Invalid account id format")
			return true
		}
		if !selectableAccountExists(accounts, *body.AccountID) {
			writeError(w, http.StatusBadRequest, "account_not_found", "Account not found")
			return true
		}
	}
	tx, err := h.beginConfigTx()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	if body.AccountID == nil {
		_ = tx.Delete(config.JSONPath("activeCodexAccountId"))
		_ = tx.Delete(config.JSONPath("activeCodexAccountPinned"))
	} else {
		payload, _ := json.Marshal(*body.AccountID)
		if tx.Set(config.JSONPath("activeCodexAccountId"), payload) != nil || tx.Set(config.JSONPath("activeCodexAccountPinned"), payload) != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "active account could not be stored")
			return true
		}
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "active account could not be stored")
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "activeCodexAccountId": body.AccountID, "appliesImmediately": true})
	return true
}

func (h *handler) serveCodexAuthAutoSwitch(w http.ResponseWriter, r *http.Request) bool {
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
		writeError(w, http.StatusBadRequest, "invalid_body", "Threshold must be an integer 0-100")
		return true
	}
	if *body.Threshold < 0 || *body.Threshold > 100 {
		writeError(w, http.StatusBadRequest, "invalid_body", "Threshold must be an integer 0-100")
		return true
	}
	tx, err := h.beginConfigTx()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	payload, err := json.Marshal(*body.Threshold)
	if err != nil || tx.Set(config.JSONPath("autoSwitchThreshold"), payload) != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "auto-switch could not be stored")
		return true
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "auto-switch could not be stored")
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "autoSwitchThreshold": *body.Threshold})
	return true
}

func (h *handler) serveCodexAuthAccountsGET(w http.ResponseWriter, r *http.Request) bool {
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
	if refresh := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("refresh"))); (refresh == "1" || refresh == "true") && h.codexAccounts != nil && h.codexAccounts.Prime != nil {
		h.codexAccounts.Prime(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": h.codexAccountRows()})
	return true
}

func (h *handler) serveCodexAuthAccountsDelete(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "Missing id")
		return true
	}
	if id == codexauth.MainAccountID {
		writeError(w, http.StatusBadRequest, "invalid_body", "the main Codex App login cannot be removed")
		return true
	}
	accounts, err := h.loadManagedAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
		return true
	}
	legacy := false
	next := make([]codexauth.ManagedAccount, 0, len(accounts.Accounts))
	for _, account := range accounts.Accounts {
		if account.ID == id && !account.IsMain {
			legacy = true
			continue
		}
		next = append(next, account)
	}
	if !codexauth.IsValidManagedAccountID(id) && !legacy {
		writeError(w, http.StatusBadRequest, "invalid_body", "Invalid account id format")
		return true
	}
	payload, err := json.Marshal(managedAccountsJSON(next))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "account could not be removed")
		return true
	}
	tx, err := h.beginConfigTx()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	if tx.Set(config.JSONPath("codexAccounts"), payload) != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "account could not be removed")
		return true
	}
	if accounts.ActiveAccountID == id {
		_ = tx.Delete(config.JSONPath("activeCodexAccountId"))
	}
	if accounts.PinnedAccountID == id {
		_ = tx.Delete(config.JSONPath("activeCodexAccountPinned"))
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "account could not be removed")
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return true
}

func (h *handler) serveCodexAuthAccountsAlias(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPut {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		ID    string `json:"id"`
		Alias string `json:"alias"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Invalid JSON")
		return true
	}
	id := strings.TrimSpace(body.ID)
	alias := strings.TrimSpace(body.Alias)
	if id == codexauth.MainAccountID {
		writeError(w, http.StatusBadRequest, "invalid_body", "Main Codex account alias is not configurable")
		return true
	}
	if !codexauth.IsValidManagedAccountID(id) {
		writeError(w, http.StatusBadRequest, "invalid_body", "Invalid account id format")
		return true
	}
	if len(alias) > 80 || aliasHasControl(alias) {
		writeError(w, http.StatusBadRequest, "invalid_body", "Alias must be a string of at most 80 printable characters")
		return true
	}
	accounts, err := h.loadManagedAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
		return true
	}
	found := false
	next := make([]codexauth.ManagedAccount, 0, len(accounts.Accounts))
	for _, account := range accounts.Accounts {
		if account.ID == id && !account.IsMain {
			found = true
			account.Alias = alias
		}
		next = append(next, account)
	}
	if !found {
		writeError(w, http.StatusNotFound, "account_not_found", "Account not found")
		return true
	}
	payload, err := json.Marshal(managedAccountsJSON(next))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "alias could not be stored")
		return true
	}
	tx, err := h.beginConfigTx()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	if tx.Set(config.JSONPath("codexAccounts"), payload) != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "alias could not be stored")
		return true
	}
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "alias could not be stored")
		return true
	}
	var outAlias any
	if alias != "" {
		outAlias = alias
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "alias": outAlias})
	return true
}

func (h *handler) serveCodexAuthAccountsPriority(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPut {
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
	var body struct {
		ID       string `json:"id"`
		Priority *int   `json:"priority"`
	}
	if json.Unmarshal(raw, &body) != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Invalid JSON")
		return true
	}
	id := strings.TrimSpace(body.ID)
	if !codexPriorityKey(id) {
		writeError(w, http.StatusBadRequest, "invalid_body", "Invalid account id format")
		return true
	}
	priority := 0
	if pri, ok := parsed["priority"]; ok && string(pri) != "null" {
		if body.Priority == nil || *body.Priority < -100 || *body.Priority > 100 {
			writeError(w, http.StatusBadRequest, "invalid_body", "priority must be null or an integer -100-100")
			return true
		}
		priority = *body.Priority
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
	next := map[string]int{}
	for key, value := range accounts.Priorities {
		next[key] = value
	}
	next[id] = priority
	payload, err := json.Marshal(next)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "selection order could not be stored")
		return true
	}
	tx, err := h.beginConfigTx()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	if tx.Set(config.JSONPath("codexAccountPriorities"), payload) != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "selection order could not be stored")
		return true
	}
	_ = tx.Delete(config.JSONPath("activeCodexAccountPinned"))
	if _, err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "selection order could not be stored")
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "priority": priority, "activeCodexAccountId": h.codexAuthActiveView()["activeCodexAccountId"]})
	return true
}

func (h *handler) serveCodexAuthClearCooldown(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	id := strings.TrimSpace(body.ID)
	if id != codexauth.MainAccountID && !codexauth.IsValidManagedAccountID(id) {
		writeError(w, http.StatusBadRequest, "invalid_body", "Invalid account id format")
		return true
	}
	cleared := false
	if h.codexHealth != nil {
		cleared = h.codexHealth.ClearLiveCooldowns(id)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id, "cleared": cleared})
	return true
}

func (h *handler) codexAuthActiveView() map[string]any {
	raw := []byte("{}")
	if disk, err := config.LoadDiskConfig(h.configPath, 0); err == nil {
		raw = disk.Raw
	}
	accounts, _ := codexauth.ProjectManagedAccountConfig(raw)
	policy, _ := codexauth.ProjectPoolRoutingPolicy(raw)
	threshold := int(codexauth.DefaultAutoSwitchThreshold)
	if policy.AutoSwitchThreshold != nil {
		threshold = int(*policy.AutoSwitchThreshold)
	}
	failover := 3
	if policy.FailoverThreshold != nil {
		failover = *policy.FailoverThreshold
	}
	strategy := string(policy.Strategy)
	if strategy == "" {
		strategy = string(codexauth.PoolStrategyQuota)
	}
	sticky := policy.StickyLimit
	if sticky == 0 {
		sticky = 1
	}
	order := string(policy.ResetOrder)
	if order == "" {
		order = string(codexauth.ResetOrderSoonest)
	}
	pinnedID := strings.TrimSpace(accounts.PinnedAccountID)
	activeID := strings.TrimSpace(accounts.ActiveAccountID)
	if activeID == "" {
		activeID = pinnedID
	}
	var active any
	if activeID != "" {
		active = activeID
	}
	var pinnedAccount any
	if pinnedID != "" {
		pinnedAccount = pinnedID
	}
	return map[string]any{
		"activeCodexAccountId":      active,
		"pinned":                    pinnedID != "",
		"pinnedAccountId":           pinnedAccount,
		"autoSwitchThreshold":       threshold,
		"upstreamFailoverThreshold": failover,
		"accountPoolStrategy":       strategy,
		"accountPoolResetOrder":     order,
		"accountPoolStickyLimit":    sticky,
	}
}

func (h *handler) codexAccountRows() []map[string]any {
	accounts, _ := h.loadManagedAccounts()
	rows := []map[string]any{h.fillCodexAccountDTO(codexauth.ManagedAccount{
		ID:     codexauth.MainAccountID,
		Email:  "Codex App login",
		IsMain: true,
	}, accounts)}
	for _, account := range accounts.Accounts {
		if account.IsMain || account.ID == codexauth.MainAccountID {
			continue
		}
		rows = append(rows, h.fillCodexAccountDTO(account, accounts))
	}
	return rows
}

func (h *handler) codexAccountDTO(account codexauth.ManagedAccount, cfg codexauth.ManagedAccountConfig) map[string]any {
	id := account.ID
	email := strings.TrimSpace(account.Email)
	if email == "" {
		if account.IsMain {
			email = "Codex App login"
		} else {
			email = id
		}
	}
	return map[string]any{
		"id":            id,
		"alias":         emptyToNil(account.Alias),
		"email":         email,
		"plan":          emptyToNil(account.Plan),
		"isMain":        account.IsMain || id == codexauth.MainAccountID,
		"paused":        cfg.PausedAccountIDs[id],
		"priority":      cfg.Priorities[id],
		"quota":         nil,
		"hasCredential": false,
		"needsReauth":   false,
	}
}

func (h *handler) loadManagedAccounts() (codexauth.ManagedAccountConfig, error) {
	if h != nil && h.onManagedAccountLoad != nil {
		h.onManagedAccountLoad()
	}
	empty := codexauth.ManagedAccountConfig{PausedAccountIDs: map[string]bool{}, Priorities: map[string]int{}}
	if strings.TrimSpace(h.configPath) == "" {
		return empty, nil
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return empty, err
	}
	return codexauth.ProjectManagedAccountConfig(disk.Raw)
}

func (h *handler) beginConfigTx() (*config.Transaction, error) {
	if strings.TrimSpace(h.configPath) == "" {
		return nil, io.EOF
	}
	return config.NewTransactionStore(h.configPath, 0).Begin()
}

func selectableAccountExists(cfg codexauth.ManagedAccountConfig, id string) bool {
	for _, account := range cfg.Accounts {
		if account.ID == id && codexauth.IsSelectableManagedAccount(account) {
			return true
		}
	}
	return false
}

func hasLegacyMainPoolRow(cfg codexauth.ManagedAccountConfig) bool {
	for _, account := range cfg.Accounts {
		if !account.IsMain && account.ID == codexauth.MainAccountID {
			return true
		}
	}
	return false
}

func managedAccountsJSON(accounts []codexauth.ManagedAccount) []map[string]any {
	out := make([]map[string]any, 0, len(accounts))
	for _, account := range accounts {
		row := map[string]any{"id": account.ID}
		if account.Email != "" {
			row["email"] = account.Email
		}
		if account.Alias != "" {
			row["alias"] = account.Alias
		}
		if account.Plan != "" {
			row["plan"] = account.Plan
		}
		if account.IsMain {
			row["isMain"] = true
		}
		out = append(out, row)
	}
	return out
}

func codexPriorityKey(id string) bool {
	return id == codexauth.MainAccountID || codexauth.IsValidManagedAccountID(id)
}

func emptyToNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func aliasHasControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
