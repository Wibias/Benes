package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/config"
)

type CodexAccountRuntime struct {
	Store      *codexauth.ManagedCredentialStore
	Quotas     *codexauth.QuotaState
	Main       *codexauth.MainCredentialSource
	MainQuotas *codexauth.MainQuotaState
	Reauth     *codexauth.ReauthState
	Tokens     *codexauth.ManagedTokenSource
	Prime      func(context.Context)
	Now        func() time.Time
}

func (h *handler) serveCodexAuthPauseExhausted(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPut {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if h.codexAccounts != nil && h.codexAccounts.Prime != nil {
		h.codexAccounts.Prime(r.Context())
	}
	accounts, err := h.loadManagedAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
		return true
	}
	paused := []string{}
	checked := 0
	failed := 0
	if reading, plan, ok, credOK := h.mainQuotaView(); credOK {
		if ok {
			checked++
			if !accounts.PausedAccountIDs[codexauth.MainAccountID] && codexauth.QuotaExhausted(reading, plan) {
				paused = append(paused, codexauth.MainAccountID)
			}
		} else {
			failed++
		}
	}
	creds := h.managedCredentialSnapshot()
	for _, account := range accounts.Accounts {
		if !codexauth.IsSelectableManagedAccount(account) {
			continue
		}
		record := creds.Records[account.ID]
		if record.Credential == nil || record.DeletedAtMS != nil {
			continue
		}
		quota := h.poolQuota(account.ID)
		if quota == nil {
			failed++
			continue
		}
		checked++
		if !accounts.PausedAccountIDs[account.ID] && codexauth.QuotaExhausted(&quota.QuotaReading, account.Plan) {
			paused = append(paused, account.ID)
		}
	}
	if checked == 0 && failed > 0 {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok":                  false,
			"error":               "Failed to refresh any Codex account quota",
			"checkedAccountCount": 0,
			"failedAccountCount":  failed,
		})
		return true
	}
	if len(paused) > 0 {
		ids := map[string]bool{}
		for key, value := range accounts.PausedAccountIDs {
			if value {
				ids[key] = true
			}
		}
		for _, id := range paused {
			ids[id] = true
		}
		list := make([]string, 0, len(ids))
		for id := range ids {
			list = append(list, id)
		}
		sort.Strings(list)
		payload, err := json.Marshal(list)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "pause could not be stored")
			return true
		}
		tx, err := h.beginConfigTx()
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
			return true
		}
		if tx.Set(config.JSONPath("pausedCodexAccountIds"), payload) != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "pause could not be stored")
			return true
		}
		for _, id := range paused {
			if accounts.ActiveAccountID == id || accounts.PinnedAccountID == id {
				_ = tx.Delete(config.JSONPath("activeCodexAccountId"))
				_ = tx.Delete(config.JSONPath("activeCodexAccountPinned"))
			}
		}
		if _, err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, "config_unreadable", "pause could not be stored")
			return true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"pausedAccountIds":     paused,
		"pausedCount":          len(paused),
		"checkedAccountCount":  checked,
		"failedAccountCount":   failed,
		"complete":             failed == 0,
		"activeCodexAccountId": h.codexAuthActiveView()["activeCodexAccountId"],
		"appliesImmediately":   true,
	})
	return true
}

func (h *handler) serveCodexAuthResetCredits(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("accountId"))
	if accountID == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "accountId required")
		return true
	}
	token, err := h.resetCreditToken(r.Context(), accountID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": err.Error()})
		return true
	}
	dto, status, err := codexauth.FetchResetCredits(r.Context(), token)
	if err != nil {
		if status == 0 {
			status = http.StatusBadGateway
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return true
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	writeJSON(w, http.StatusOK, dto)
	return true
}

func (h *handler) serveCodexAuthResetCreditsConsume(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		AccountID       string `json:"accountId"`
		RedeemRequestID string `json:"redeemRequestId"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body) != nil || strings.TrimSpace(body.AccountID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "accountId required")
		return true
	}
	redeemRequestID := strings.TrimSpace(body.RedeemRequestID)
	if redeemRequestID == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "redeemRequestId required")
		return true
	}
	if !codexauth.ValidRedeemRequestID(redeemRequestID) {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid redeemRequestId")
		return true
	}
	token, err := h.resetCreditToken(r.Context(), strings.TrimSpace(body.AccountID))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": err.Error()})
		return true
	}
	dto, status, err := codexauth.ConsumeResetCredit(r.Context(), token, redeemRequestID)
	if err != nil {
		if status == 0 {
			status = http.StatusBadGateway
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return true
	}
	out := map[string]any{"code": dto.Code}
	if dto.Code == "reset" || dto.Code == "already_redeemed" {
		if h.codexAccounts != nil && h.codexAccounts.Prime != nil {
			h.codexAccounts.Prime(r.Context())
		}
		if reading, _, ok, _ := h.accountQuotaView(strings.TrimSpace(body.AccountID)); ok && reading != nil && reading.ResetCredits != nil {
			out["remaining"] = *reading.ResetCredits
		}
	}
	writeJSON(w, http.StatusOK, out)
	return true
}

func (h *handler) resetCreditToken(ctx context.Context, accountID string) (codexauth.ManagedToken, error) {
	if accountID == "main" {
		accountID = codexauth.MainAccountID
	}
	if accountID == codexauth.MainAccountID {
		if h.codexAccounts == nil || h.codexAccounts.Main == nil {
			return codexauth.ManagedToken{}, errString("Main Codex account not logged in")
		}
		now := time.Now()
		if h.codexAccounts.Now != nil {
			now = h.codexAccounts.Now()
		}
		result := h.codexAccounts.Main.Read(now)
		if result.Status != codexauth.MainCredentialOK {
			return codexauth.ManagedToken{}, errString("Main Codex account not logged in")
		}
		return codexauth.ManagedToken{AccessToken: result.Credential.AccessToken, ChatGPTAccountID: result.Credential.ChatGPTAccountID}, nil
	}
	if h.codexAccounts == nil || h.codexAccounts.Tokens == nil {
		return codexauth.ManagedToken{}, errString("Codex account not logged in")
	}
	return h.codexAccounts.Tokens.Get(ctx, accountID)
}

type stringError string

func (e stringError) Error() string { return string(e) }

func errString(message string) error { return stringError(message) }

func (h *handler) fillCodexAccountDTO(account codexauth.ManagedAccount, cfg codexauth.ManagedAccountConfig) map[string]any {
	row := h.codexAccountDTO(account, cfg)
	id := account.ID
	hasCredential := false
	needsReauth := false
	var quota *codexauth.QuotaReading
	updated := int64(0)
	if account.IsMain || id == codexauth.MainAccountID {
		reading, plan, ok, credOK := h.mainQuotaView()
		hasCredential = credOK
		needsReauth = !credOK
		if ok {
			quota = reading
			if h.codexAccounts != nil && h.codexAccounts.MainQuotas != nil && h.codexAccounts.Main != nil {
				now := time.Now()
				if h.codexAccounts.Now != nil {
					now = h.codexAccounts.Now()
				}
				snap := h.codexAccounts.MainQuotas.Snapshot(h.codexAccounts.Main.Read(now).Identity)
				updated = snap.UpdatedAt.UnixMilli()
			}
		}
		if plan != "" {
			row["plan"] = plan
		}
	} else {
		creds := h.managedCredentialSnapshot()
		record := creds.Records[id]
		hasCredential = record.Credential != nil && record.DeletedAtMS == nil
		needsReauth = !hasCredential
		if stored := h.poolQuota(id); stored != nil {
			q := stored.QuotaReading
			quota = &q
			updated = stored.UpdatedAt.UnixMilli()
		}
	}
	if h.codexAccounts != nil && h.codexAccounts.Reauth != nil && h.codexAccounts.Reauth.Needs(id) {
		needsReauth = true
	}
	row["hasCredential"] = hasCredential
	row["needsReauth"] = needsReauth
	if (account.IsMain || id == codexauth.MainAccountID) && h.codexAccounts != nil && h.codexAccounts.Main != nil {
		now := time.Now()
		if h.codexAccounts.Now != nil {
			now = h.codexAccounts.Now()
		}
		if email := strings.TrimSpace(h.codexAccounts.Main.Read(now).Email); email != "" {
			row["email"] = email
		}
	}
	if quota == nil {
		row["quota"] = nil
	} else {
		row["quota"] = quotaJSON(quota, updated)
	}
	return row
}

func quotaJSON(quota *codexauth.QuotaReading, updatedAt int64) map[string]any {
	out := map[string]any{}
	if quota.WeeklyPercent != nil {
		out["weeklyPercent"] = *quota.WeeklyPercent
	}
	if quota.MonthlyPercent != nil {
		out["monthlyPercent"] = *quota.MonthlyPercent
	}
	if quota.ShortPercent != nil {
		out["shortPercent"] = *quota.ShortPercent
		out["fiveHourPercent"] = *quota.ShortPercent
	}
	if quota.WeeklyResetAt != nil {
		out["weeklyResetAt"] = *quota.WeeklyResetAt
	}
	if quota.MonthlyResetAt != nil {
		out["monthlyResetAt"] = *quota.MonthlyResetAt
	}
	if quota.ShortResetAt != nil {
		out["shortResetAt"] = *quota.ShortResetAt
		out["fiveHourResetAt"] = *quota.ShortResetAt
	}
	if quota.ShortWindowSeconds != nil {
		out["shortWindowSeconds"] = *quota.ShortWindowSeconds
	}
	if quota.ResetCredits != nil {
		out["resetCredits"] = *quota.ResetCredits
	}
	if quota.MonthlyIsPrimaryWindow {
		out["monthlyIsPrimaryWindow"] = true
	}
	if updatedAt != 0 {
		out["updatedAt"] = updatedAt
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (h *handler) managedCredentialSnapshot() codexauth.ManagedCredentialSnapshot {
	if h.codexAccounts == nil || h.codexAccounts.Store == nil {
		return codexauth.ManagedCredentialSnapshot{Records: map[string]codexauth.ManagedCredentialRecord{}}
	}
	return h.codexAccounts.Store.Read()
}

func (h *handler) poolQuota(accountID string) *codexauth.StoredQuota {
	if h.codexAccounts == nil || h.codexAccounts.Quotas == nil {
		return nil
	}
	return h.codexAccounts.Quotas.Get(accountID)
}

func (h *handler) mainQuotaView() (*codexauth.QuotaReading, string, bool, bool) {
	if h.codexAccounts == nil || h.codexAccounts.Main == nil {
		return nil, "", false, false
	}
	now := time.Now()
	if h.codexAccounts.Now != nil {
		now = h.codexAccounts.Now()
	}
	result := h.codexAccounts.Main.Read(now)
	if result.Status != codexauth.MainCredentialOK {
		return nil, "", false, false
	}
	if h.codexAccounts.MainQuotas == nil {
		return nil, "", false, true
	}
	snap := h.codexAccounts.MainQuotas.Snapshot(result.Identity)
	if snap.Quota == nil {
		return nil, snap.Plan, false, true
	}
	return snap.Quota, snap.Plan, true, true
}

func (h *handler) accountQuotaView(accountID string) (*codexauth.QuotaReading, string, bool, bool) {
	if accountID == "main" || accountID == codexauth.MainAccountID {
		return h.mainQuotaView()
	}
	quota := h.poolQuota(accountID)
	if quota == nil {
		return nil, "", false, false
	}
	reading := quota.QuotaReading
	return &reading, "", true, true
}
