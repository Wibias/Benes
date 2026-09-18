package server

import (
	"net/http"
	"path/filepath"

	"github.com/Wibias/Benes/internal/oauth/cursor"
	"github.com/Wibias/Benes/internal/oauth/githubcopilot"
	"github.com/Wibias/Benes/internal/oauth/kimi"
	"github.com/Wibias/Benes/internal/oauth/nous"
	"github.com/Wibias/Benes/internal/providers/antigravity"
)

func loginImplemented(provider string) bool {
	return provider == antigravity.AuthStoreProvider || provider == cursor.ProviderID || isDeviceOAuthProvider(provider) || isCallbackOAuthProvider(provider)
}

func isDeviceOAuthProvider(provider string) bool {
	return provider == kimi.ProviderID || provider == nous.ProviderID || provider == githubcopilot.ProviderID
}

func startDeviceOAuthLogin(w http.ResponseWriter, r *http.Request, home, provider string) bool {
	switch provider {
	case kimi.ProviderID:
		login, err := kimi.NewPendingLogin(r.Context(), home)
		if err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		if err := (kimi.PendingFile{Path: filepath.Join(home, kimi.PendingFileName)}).Save(login); err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": provider, "url": login.AuthURL(), "userCode": login.UserCode})
		return true
	case nous.ProviderID:
		login, err := nous.NewPendingLogin(r.Context())
		if err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		if err := (nous.PendingFile{Path: filepath.Join(home, nous.PendingFileName)}).Save(login); err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": provider, "url": login.AuthURL(), "userCode": login.UserCode})
		return true
	case githubcopilot.ProviderID:
		login, err := githubcopilot.NewPendingLogin(r.Context())
		if err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		if err := (githubcopilot.PendingFile{Path: filepath.Join(home, githubcopilot.PendingFileName)}).Save(login); err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": provider, "url": login.AuthURL(), "userCode": login.UserCode})
		return true
	default:
		return false
	}
}

func completeDeviceOAuthLogin(w http.ResponseWriter, r *http.Request, home, provider string) bool {
	if !isDeviceOAuthProvider(provider) {
		return false
	}
	token, ok := ownerToken(w, provider)
	if !ok {
		return true
	}
	authPath := filepath.Join(home, "auth.json")
	var (
		account antigravity.StoredAccount
		err     error
	)
	switch provider {
	case kimi.ProviderID:
		var completed kimi.CompletedLogin
		completed, err = (kimi.PendingFile{Path: filepath.Join(home, kimi.PendingFileName)}).Complete(r.Context(), home)
		if err == nil {
			account = completed.Account
		}
	case nous.ProviderID:
		var completed nous.CompletedLogin
		completed, err = (nous.PendingFile{Path: filepath.Join(home, nous.PendingFileName)}).Complete(r.Context())
		if err == nil {
			account = completed.Account
		}
	case githubcopilot.ProviderID:
		var completed githubcopilot.CompletedLogin
		completed, err = (githubcopilot.PendingFile{Path: filepath.Join(home, githubcopilot.PendingFileName)}).Complete(r.Context())
		if err == nil {
			account = completed.Account
		}
	default:
		return false
	}
	if err != nil {
		writeOAuthPublicError(w, http.StatusBadRequest, err)
		return true
	}
	if !persistOwnedAccount(w, provider, authPath, account, token) {
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": provider, "id": account.ID})
	return true
}
