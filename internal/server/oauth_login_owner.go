package server

import (
	"errors"
	"net/http"

	"github.com/Wibias/Benes/internal/authpublic"
	"github.com/Wibias/Benes/internal/oauth/loginown"
	"github.com/Wibias/Benes/internal/providers/antigravity"
)

func writeOAuthPublicError(w http.ResponseWriter, status int, err error) {
	if status <= 0 {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, map[string]any{"ok": false, "error": authpublic.ProjectOAuth(err)})
}

func beginOAuthLogin(w http.ResponseWriter, provider string) bool {
	if _, err := loginown.TryClaim(provider); err != nil {
		writeOAuthPublicError(w, http.StatusConflict, err)
		return false
	}
	return true
}

func abortOAuthLogin(provider string) {
	loginown.Cancel(provider)
}

func ownerToken(w http.ResponseWriter, provider string) (loginown.Token, bool) {
	token, ok := loginown.Current(provider)
	if !ok {
		writeOAuthPublicError(w, http.StatusConflict, authpublic.LoginSupersededError{})
		return loginown.Token{}, false
	}
	return token, true
}

func persistOwnedAccount(w http.ResponseWriter, provider, path string, account antigravity.StoredAccount, token loginown.Token) bool {
	if err := antigravity.AppendStoredAccountChecked(path, provider, account, func() error {
		return loginown.Assert(token)
	}); err != nil {
		var superseded authpublic.LoginSupersededError
		if errors.As(err, &superseded) {
			writeOAuthPublicError(w, http.StatusConflict, err)
			return false
		}
		writeOAuthPublicError(w, http.StatusInternalServerError, err)
		return false
	}
	loginown.Release(token)
	return true
}
