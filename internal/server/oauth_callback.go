package server

import (
	"net/http"
	"path/filepath"

	"github.com/Wibias/Benes/internal/oauth/anthropic"
	"github.com/Wibias/Benes/internal/oauth/commandcode"
	"github.com/Wibias/Benes/internal/oauth/xai"
	"github.com/Wibias/Benes/internal/providers/antigravity"
)

func isCallbackOAuthProvider(provider string) bool {
	return provider == xai.ProviderID || provider == anthropic.ProviderID || provider == commandcode.ProviderID
}

func startCallbackOAuthLogin(w http.ResponseWriter, r *http.Request, home, provider string) bool {
	switch provider {
	case xai.ProviderID:
		login, err := xai.NewPendingLogin(r.Context())
		if err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		authURL, err := login.AuthURL()
		if err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		if err := (xai.PendingFile{Path: filepath.Join(home, xai.PendingFileName)}).Save(login); err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": provider, "url": authURL})
		return true
	case anthropic.ProviderID:
		login, err := anthropic.NewPendingLogin()
		if err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		authURL, err := login.AuthURL()
		if err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		if err := (anthropic.PendingFile{Path: filepath.Join(home, anthropic.PendingFileName)}).Save(login); err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": provider, "url": authURL})
		return true
	case commandcode.ProviderID:
		login, err := commandcode.NewPendingLogin()
		if err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		if err := (commandcode.PendingFile{Path: filepath.Join(home, commandcode.PendingFileName)}).Save(login); err != nil {
			abortOAuthLogin(provider)
			writeOAuthPublicError(w, http.StatusInternalServerError, err)
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": provider, "url": login.AuthURL()})
		return true
	default:
		return false
	}
}

func completeCallbackOAuthLogin(w http.ResponseWriter, r *http.Request, home, provider, callback string) bool {
	if !isCallbackOAuthProvider(provider) {
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
	case xai.ProviderID:
		var completed xai.CompletedLogin
		completed, err = (xai.PendingFile{Path: filepath.Join(home, xai.PendingFileName)}).Complete(r.Context(), callback)
		if err == nil {
			account = completed.Account
		}
	case anthropic.ProviderID:
		var completed anthropic.CompletedLogin
		completed, err = (anthropic.PendingFile{Path: filepath.Join(home, anthropic.PendingFileName)}).Complete(r.Context(), callback)
		if err == nil {
			account = completed.Account
		}
	case commandcode.ProviderID:
		var completed commandcode.CompletedLogin
		completed, err = (commandcode.PendingFile{Path: filepath.Join(home, commandcode.PendingFileName)}).Complete(r.Context(), callback)
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
