package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/authpublic"
	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/oauth/chatgpt"
	"github.com/Wibias/Benes/internal/oauth/loginown"
)

type codexLoginFlow struct {
	Status    string         `json:"status"`
	AccountID string         `json:"accountId,omitempty"`
	Email     string         `json:"email,omitempty"`
	Error     string         `json:"error,omitempty"`
	Reauth    bool           `json:"-"`
	TargetID  string         `json:"-"`
	Owner     loginown.Token `json:"-"`
}

var (
	codexLoginMu    sync.Mutex
	codexLoginState = map[string]*codexLoginFlow{}
)

func (h *handler) serveCodexAuthLogin(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		ID     string `json:"id"`
		Reauth bool   `json:"reauth"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	targetID := strings.TrimSpace(body.ID)
	if targetID != "" && !codexauth.IsValidManagedAccountID(targetID) {
		writeError(w, http.StatusBadRequest, "invalid_body", "Invalid account id format")
		return true
	}
	if body.Reauth && targetID == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "id required for reauth")
		return true
	}
	accounts, err := h.loadManagedAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be opened")
		return true
	}
	if body.Reauth && !selectableAccountExists(accounts, targetID) {
		writeError(w, http.StatusNotFound, "account_not_found", "Unknown pool account for reauth")
		return true
	}
	if targetID == "" {
		targetID = fmt.Sprintf("chatgpt-%d", time.Now().UnixMilli())
	}
	home := h.benesHome()
	if home == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	login, err := chatgpt.NewPendingLogin(targetID, body.Reauth)
	if err != nil {
		writeOAuthPublicError(w, http.StatusInternalServerError, err)
		return true
	}
	authURL, err := login.AuthURL()
	if err != nil {
		writeOAuthPublicError(w, http.StatusInternalServerError, err)
		return true
	}
	if err := (chatgpt.PendingFile{Path: filepath.Join(home, chatgpt.PendingFileName)}).Save(login); err != nil {
		writeOAuthPublicError(w, http.StatusInternalServerError, err)
		return true
	}
	flowID := "flow-" + randomHex(8)
	token := loginown.Claim("chatgpt")
	codexLoginMu.Lock()
	for _, flow := range codexLoginState {
		if flow.Status == "pending" {
			flow.Status = "expired"
			flow.Error = authpublic.SupersededMessage
		}
	}
	codexLoginState[flowID] = &codexLoginFlow{Status: "pending", TargetID: targetID, Reauth: body.Reauth, Owner: token}
	codexLoginMu.Unlock()
	go h.listenChatGPTCallback(home, flowID, login.State)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"flowId":       flowID,
		"url":          authURL,
		"instructions": "Complete ChatGPT login in your browser.",
	})
	return true
}

func (h *handler) serveCodexAuthLoginCode(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		FlowID string `json:"flowId"`
		Input  string `json:"input"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "Invalid JSON")
		return true
	}
	flowID := strings.TrimSpace(body.FlowID)
	if flowID == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "flowId required")
		return true
	}
	if len(body.Input) > 4096 {
		writeError(w, http.StatusBadRequest, "invalid_body", "input too long")
		return true
	}
	codexLoginMu.Lock()
	flow := codexLoginState[flowID]
	codexLoginMu.Unlock()
	if flow == nil || flow.Status != "pending" {
		writeError(w, http.StatusBadRequest, "invalid_body", "login flow expired or unknown")
		return true
	}
	home := h.benesHome()
	completed, err := (chatgpt.PendingFile{Path: filepath.Join(home, chatgpt.PendingFileName)}).Complete(r.Context(), body.Input)
	if err != nil {
		h.setCodexLoginError(flowID, authpublic.ProjectOAuth(err))
		writeOAuthPublicError(w, http.StatusBadRequest, err)
		return true
	}
	if err := h.commitChatGPTLogin(flowID, completed); err != nil {
		h.setCodexLoginError(flowID, authpublic.ProjectOAuth(err))
		writeOAuthPublicError(w, http.StatusBadRequest, err)
		return true
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
	return true
}

func (h *handler) serveCodexAuthLoginCancel(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	var body struct {
		FlowID string `json:"flowId"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	home := h.benesHome()
	loginown.Cancel("chatgpt")
	if home != "" {
		_ = os.Remove(filepath.Join(home, chatgpt.PendingFileName))
	}
	codexLoginMu.Lock()
	if flow := codexLoginState[strings.TrimSpace(body.FlowID)]; flow != nil {
		flow.Status = "expired"
		flow.Error = "Login cancelled"
	}
	codexLoginMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cancelled": true})
	return true
}

func (h *handler) serveCodexAuthLoginStatus(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	flowID := strings.TrimSpace(r.URL.Query().Get("flowId"))
	codexLoginMu.Lock()
	flow := codexLoginState[flowID]
	codexLoginMu.Unlock()
	if flow == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "expired", "error": "login flow expired or unknown"})
		return true
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    flow.Status,
		"accountId": emptyToNil(flow.AccountID),
		"email":     emptyToNil(flow.Email),
		"error":     emptyToNil(flow.Error),
	})
	return true
}

func (h *handler) listenChatGPTCallback(home, flowID, expectedState string) {
	ln, err := net.Listen("tcp", "127.0.0.1:1455")
	if err != nil {
		return
	}
	defer ln.Close()
	srv := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/callback" {
			http.NotFound(w, r)
			return
		}
		completed, err := (chatgpt.PendingFile{Path: filepath.Join(home, chatgpt.PendingFileName)}).Complete(r.Context(), chatgpt.RedirectURI+"?"+r.URL.RawQuery)
		if err != nil {
			h.setCodexLoginError(flowID, authpublic.ProjectOAuth(err))
			http.Error(w, "login failed", http.StatusBadRequest)
			return
		}
		if err := h.commitChatGPTLogin(flowID, completed); err != nil {
			h.setCodexLoginError(flowID, authpublic.ProjectOAuth(err))
			http.Error(w, "login failed", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, "ChatGPT login complete. You can close this tab.")
	})}
	_ = expectedState
	_ = srv.Serve(ln)
}

func (h *handler) commitChatGPTLogin(flowID string, completed chatgpt.CompletedLogin) error {
	codexLoginMu.Lock()
	flow := codexLoginState[flowID]
	codexLoginMu.Unlock()
	if flow == nil || flow.Status != "pending" {
		return authpublic.LoginSupersededError{}
	}
	accounts, err := h.loadManagedAccounts()
	if err != nil {
		return err
	}
	home := h.benesHome()
	store, err := codexauth.NewManagedCredentialStore(home)
	if err != nil {
		return err
	}
	snapshot := store.Read()
	if flow.Reauth {
		existing := snapshot.Records[flow.TargetID]
		pool := managedAccountByID(accounts, flow.TargetID)
		expectedID := ""
		if existing.Credential != nil {
			expectedID = strings.TrimSpace(existing.Credential.ChatGPTAccountID)
		}
		expectedEmail := strings.ToLower(strings.TrimSpace(pool.Email))
		gotEmail := strings.ToLower(strings.TrimSpace(completed.Email))
		if expectedID != "" {
			if expectedID != completed.ChatGPTAccountID {
				return authpublic.ReauthIdentityMismatchError{}
			}
		} else if expectedEmail != "" {
			if gotEmail == "" || gotEmail != expectedEmail {
				return authpublic.ReauthIdentityMismatchError{}
			}
		} else {
			return authpublic.ReauthIdentityUnverifiedError{}
		}
	}
	exclude := ""
	if flow.Reauth {
		exclude = flow.TargetID
	}
	if reason := chatgptAccountCollision(accounts, snapshot, completed.ChatGPTAccountID, completed.Email, exclude); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	now := time.Now().UnixMilli()
	token := flow.Owner
	if err := loginown.Assert(token); err != nil {
		return err
	}
	if err := store.PutChecked(context.Background(), flow.TargetID, codexauth.ManagedCredential{
		AccessToken:      completed.AccessToken,
		RefreshToken:     completed.RefreshToken,
		ExpiresAtMS:      completed.ExpiresAtMS,
		ChatGPTAccountID: completed.ChatGPTAccountID,
	}, &now, func() error { return loginown.Assert(token) }); err != nil {
		return err
	}
	email := completed.Email
	if email == "" {
		email = flow.TargetID
	}
	next := accounts.Accounts
	found := false
	for i, account := range next {
		if account.ID == flow.TargetID && !account.IsMain {
			account.Email = email
			account.ChatGPTAccountID = completed.ChatGPTAccountID
			next[i] = account
			found = true
			break
		}
	}
	if !found {
		next = append(next, codexauth.ManagedAccount{
			ID:               flow.TargetID,
			Email:            email,
			ChatGPTAccountID: completed.ChatGPTAccountID,
		})
	}
	payload, err := json.Marshal(managedAccountsJSON(next))
	if err != nil {
		return err
	}
	tx, err := h.beginConfigTx()
	if err != nil {
		return err
	}
	if err := tx.Set(config.JSONPath("codexAccounts"), payload); err != nil {
		return err
	}
	if _, err := tx.Commit(); err != nil {
		return err
	}
	codexLoginMu.Lock()
	if err := loginown.Assert(flow.Owner); err != nil {
		codexLoginMu.Unlock()
		return err
	}
	flow.Status = "done"
	flow.AccountID = flow.TargetID
	flow.Email = email
	flow.Error = ""
	codexLoginMu.Unlock()
	loginown.Release(flow.Owner)
	return nil
}

func chatgptAccountCollision(accounts codexauth.ManagedAccountConfig, snapshot codexauth.ManagedCredentialSnapshot, chatgptID, email, exclude string) string {
	gotEmail := strings.ToLower(strings.TrimSpace(email))
	for _, account := range accounts.Accounts {
		if account.ID == exclude || !codexauth.IsSelectableManagedAccount(account) {
			continue
		}
		record := snapshot.Records[account.ID]
		if record.Credential == nil {
			continue
		}
		poolEmail := strings.ToLower(strings.TrimSpace(account.Email))
		if record.Credential.ChatGPTAccountID == chatgptID && (gotEmail == "" || poolEmail == "" || poolEmail == gotEmail) {
			return "Account is already in the pool (" + account.ID + ")."
		}
	}
	return ""
}

func managedAccountByID(accounts codexauth.ManagedAccountConfig, id string) codexauth.ManagedAccount {
	for _, account := range accounts.Accounts {
		if account.ID == id {
			return account
		}
	}
	return codexauth.ManagedAccount{}
}

func (h *handler) setCodexLoginError(flowID, message string) {
	codexLoginMu.Lock()
	defer codexLoginMu.Unlock()
	if flow := codexLoginState[flowID]; flow != nil {
		flow.Status = "error"
		flow.Error = message
	}
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
