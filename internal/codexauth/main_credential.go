package codexauth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	MainAccountID    = "__main__"
	maxMainAuthBytes = 1 << 20
)

type MainCredentialStatus string

const (
	MainCredentialOK         MainCredentialStatus = "ok"
	MainCredentialMissing    MainCredentialStatus = "missing"
	MainCredentialInvalid    MainCredentialStatus = "invalid"
	MainCredentialUnreadable MainCredentialStatus = "unreadable"
	MainCredentialExpired    MainCredentialStatus = "expired"
)

type MainCredential struct {
	AccessToken      string
	ChatGPTAccountID string
}

type MainCredentialResult struct {
	Status      MainCredentialStatus
	Credential  MainCredential
	Identity    string
	Email       string
	Refreshable bool
}

func (r MainCredentialResult) Selectable() bool {
	switch r.Status {
	case MainCredentialOK:
		return true
	case MainCredentialExpired:
		return r.Refreshable
	default:
		return false
	}
}

type MainCredentialSource struct {
	authPath           string
	openFile           func(string) (*os.File, error)
	httpClient         *http.Client
	refreshURL         string
	refreshLockWait    time.Duration
	refreshHTTPTimeout time.Duration
}

type mainAuthDocument struct {
	result       MainCredentialResult
	refreshToken string
	raw          []byte
}

func NewMainCredentialSource(codexHome string) (*MainCredentialSource, error) {
	if strings.TrimSpace(codexHome) == "" || !filepath.IsAbs(codexHome) {
		return nil, fmt.Errorf("absolute Codex home is required")
	}
	return &MainCredentialSource{
		authPath:           filepath.Join(filepath.Clean(codexHome), "auth.json"),
		openFile:           os.Open,
		refreshURL:         managedTokenRefreshEndpoint,
		refreshLockWait:    defaultManagedRefreshWait,
		refreshHTTPTimeout: defaultManagedRefreshHTTP,
	}, nil
}

func (s *MainCredentialSource) Read(now time.Time) MainCredentialResult {
	return s.load(now).result
}

func (s *MainCredentialSource) load(now time.Time) mainAuthDocument {
	file, err := s.openFile(s.authPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mainAuthDocument{result: MainCredentialResult{Status: MainCredentialMissing}}
		}
		return mainAuthDocument{result: MainCredentialResult{Status: MainCredentialUnreadable}}
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return mainAuthDocument{result: MainCredentialResult{Status: MainCredentialUnreadable}}
	}
	if !info.Mode().IsRegular() || info.Size() > maxMainAuthBytes {
		return mainAuthDocument{result: MainCredentialResult{Status: MainCredentialInvalid}}
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return mainAuthDocument{result: MainCredentialResult{Status: MainCredentialInvalid}}
	}

	raw, err := io.ReadAll(io.LimitReader(file, maxMainAuthBytes+1))
	if err != nil {
		return mainAuthDocument{result: MainCredentialResult{Status: MainCredentialUnreadable}}
	}
	if len(raw) > maxMainAuthBytes {
		return mainAuthDocument{result: MainCredentialResult{Status: MainCredentialInvalid}}
	}

	var document struct {
		Tokens *struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			IDToken      string `json:"id_token"`
			AccountID    string `json:"account_id"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &document); err != nil || document.Tokens == nil || document.Tokens.AccessToken == "" {
		return mainAuthDocument{result: MainCredentialResult{Status: MainCredentialInvalid}, raw: raw}
	}
	refreshable := strings.TrimSpace(document.Tokens.RefreshToken) != ""
	if jwtExpired(document.Tokens.AccessToken, now) {
		return mainAuthDocument{
			result:       MainCredentialResult{Status: MainCredentialExpired, Refreshable: refreshable},
			refreshToken: document.Tokens.RefreshToken,
			raw:          raw,
		}
	}

	return mainAuthDocument{
		result: MainCredentialResult{
			Status: MainCredentialOK,
			Credential: MainCredential{
				AccessToken:      document.Tokens.AccessToken,
				ChatGPTAccountID: document.Tokens.AccountID,
			},
			Identity:    mainAccountIdentity(document.Tokens.IDToken, document.Tokens.AccessToken, document.Tokens.AccountID),
			Email:       jwtEmail(document.Tokens.IDToken, document.Tokens.AccessToken),
			Refreshable: refreshable,
		},
		refreshToken: document.Tokens.RefreshToken,
		raw:          raw,
	}
}

func (s *MainCredentialSource) IsIdentityLive(identity string, now time.Time) bool {
	current := s.Read(now)
	return current.Status == MainCredentialOK && current.Identity == identity
}

func mainAccountIdentity(idToken, accessToken, accountID string) string {
	for _, token := range []string{idToken, accessToken} {
		if token == "" {
			continue
		}
		if identity, ok := jwtAccountIdentity(token); ok {
			return identity
		}
	}
	return accountID
}

func jwtEmail(tokens ...string) string {
	for _, token := range tokens {
		if token == "" {
			continue
		}
		parts := strings.Split(token, ".")
		if len(parts) != 3 || parts[1] == "" {
			continue
		}
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			continue
		}
		var claims map[string]any
		if json.Unmarshal(payload, &claims) != nil || claims == nil {
			continue
		}
		email, _ := claims["email"].(string)
		email = strings.ToLower(strings.TrimSpace(email))
		if email != "" && strings.Contains(email, "@") {
			return email
		}
	}
	return ""
}

func jwtAccountIdentity(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[1] == "" {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil || claims == nil {
		return "", false
	}
	if value, ok := claims["chatgpt_account_id"].(string); ok {
		return value, true
	}
	if namespace, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if value, ok := namespace["chatgpt_account_id"].(string); ok {
			return value, true
		}
	}
	if organizations, ok := claims["organizations"].([]any); ok && len(organizations) > 0 {
		if first, ok := organizations[0].(map[string]any); ok {
			if value, ok := first["id"].(string); ok {
				return value, true
			}
		}
	}
	return "", false
}

func jwtExpired(token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[1] == "" {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Exp *float64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == nil {
		return false
	}
	nowSeconds := float64(now.UnixNano()) / float64(time.Second)
	return *claims.Exp <= nowSeconds
}
