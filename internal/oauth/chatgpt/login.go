package chatgpt

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ProviderID      = "chatgpt"
	clientID        = "app_EMoamEEZ73f0CkXaXp7hrann"
	authorizeURL    = "https://auth.openai.com/oauth/authorize"
	tokenURL        = "https://auth.openai.com/oauth/token"
	scope           = "openid profile email offline_access api.connectors.read api.connectors.invoke"
	RedirectURI     = "http://localhost:1455/auth/callback"
	PendingFileName = "oauth-pending-chatgpt.json"
	pendingTTL      = 15 * time.Minute
	originator      = "benes"
)

var HTTPClient HTTPDoer = http.DefaultClient

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type PendingLogin struct {
	State       string    `json:"state"`
	Verifier    string    `json:"verifier"`
	RedirectURI string    `json:"redirectUri"`
	AccountID   string    `json:"accountId,omitempty"`
	Reauth      bool      `json:"reauth,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type PendingFile struct{ Path string }

type CompletedLogin struct {
	AccessToken      string
	RefreshToken     string
	ExpiresAtMS      int64
	ChatGPTAccountID string
	Email            string
}

func NewPendingLogin(accountID string, reauth bool) (PendingLogin, error) {
	state, err := randomB64(16)
	if err != nil {
		return PendingLogin{}, err
	}
	verifier, err := randomB64(32)
	if err != nil {
		return PendingLogin{}, err
	}
	return PendingLogin{
		State:       state,
		Verifier:    verifier,
		RedirectURI: RedirectURI,
		AccountID:   strings.TrimSpace(accountID),
		Reauth:      reauth,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func (p PendingLogin) AuthURL() (string, error) {
	if p.State == "" || p.Verifier == "" {
		return "", fmt.Errorf("ChatGPT login state is incomplete")
	}
	sum := sha256.Sum256([]byte(p.Verifier))
	query := url.Values{
		"response_type":              {"code"},
		"client_id":                  {clientID},
		"redirect_uri":               {p.RedirectURI},
		"scope":                      {scope},
		"code_challenge":             {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method":      {"S256"},
		"state":                      {p.State},
		"codex_cli_simplified_flow":  {"true"},
		"originator":                 {originator},
		"id_token_add_organizations": {"true"},
		"prompt":                     {"login"},
	}
	return authorizeURL + "?" + query.Encode(), nil
}

func (s PendingFile) Save(pending PendingLogin) error {
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("ChatGPT pending login path is required")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	return os.WriteFile(s.Path, raw, 0o600)
}

func (s PendingFile) Load() (PendingLogin, error) {
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return PendingLogin{}, fmt.Errorf("ChatGPT login was already consumed")
		}
		return PendingLogin{}, err
	}
	var pending PendingLogin
	if json.Unmarshal(raw, &pending) != nil || pending.Verifier == "" {
		return PendingLogin{}, fmt.Errorf("ChatGPT login state is invalid")
	}
	if time.Since(pending.CreatedAt) > pendingTTL {
		_ = os.Remove(s.Path)
		return PendingLogin{}, fmt.Errorf("ChatGPT login expired")
	}
	return pending, nil
}

func (s PendingFile) Consume() error {
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s PendingFile) Complete(ctx context.Context, callback string) (CompletedLogin, error) {
	pending, err := s.Load()
	if err != nil {
		return CompletedLogin{}, err
	}
	code, state, err := parseCallback(callback, pending.State)
	if err != nil {
		return CompletedLogin{}, err
	}
	completed, err := exchange(ctx, pending, code, state)
	if err != nil {
		return CompletedLogin{}, err
	}
	if err := s.Consume(); err != nil {
		return CompletedLogin{}, err
	}
	return completed, nil
}

func parseCallback(raw, expectedState string) (code, state string, err error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", "", fmt.Errorf("ChatGPT callback is required")
	}
	state = expectedState
	if strings.Contains(value, "://") || strings.HasPrefix(value, "/") {
		parsed, perr := url.Parse(value)
		if perr != nil {
			return "", "", perr
		}
		code = parsed.Query().Get("code")
		if parsed.Query().Get("state") != "" {
			state = parsed.Query().Get("state")
		}
	} else {
		code = value
	}
	if code == "" {
		return "", "", fmt.Errorf("ChatGPT authorization code is missing")
	}
	if state != expectedState {
		return "", "", fmt.Errorf("ChatGPT login state mismatch")
	}
	return code, state, nil
}

func exchange(ctx context.Context, pending PendingLogin, code, state string) (CompletedLogin, error) {
	_ = state
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {pending.RedirectURI},
		"code_verifier": {pending.Verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return CompletedLogin{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return CompletedLogin{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return CompletedLogin{}, fmt.Errorf("ChatGPT token exchange failed: %s", strings.TrimSpace(string(body)))
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return CompletedLogin{}, fmt.Errorf("ChatGPT token payload is invalid")
	}
	access, _ := payload["access_token"].(string)
	refresh, _ := payload["refresh_token"].(string)
	idToken, _ := payload["id_token"].(string)
	if strings.TrimSpace(access) == "" {
		return CompletedLogin{}, fmt.Errorf("ChatGPT access token is missing")
	}
	expiresIn := 3600.0
	if n, ok := payload["expires_in"].(float64); ok && n >= 0 {
		expiresIn = n
	}
	accountID := extractAccountID(idToken, access)
	if accountID == "" {
		return CompletedLogin{}, fmt.Errorf("Could not determine account identity from OAuth tokens. Please retry OAuth login.")
	}
	return CompletedLogin{
		AccessToken:      access,
		RefreshToken:     refresh,
		ExpiresAtMS:      time.Now().UTC().Add(time.Duration(expiresIn) * time.Second).UnixMilli(),
		ChatGPTAccountID: accountID,
		Email:            extractEmail(idToken, access),
	}, nil
}

func extractAccountID(tokens ...string) string {
	for _, token := range tokens {
		claims := decodeJWT(token)
		if claims == nil {
			continue
		}
		if id, _ := claims["chatgpt_account_id"].(string); strings.TrimSpace(id) != "" {
			return id
		}
		if ns, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
			if id, _ := ns["chatgpt_account_id"].(string); strings.TrimSpace(id) != "" {
				return id
			}
		}
	}
	return ""
}

func extractEmail(tokens ...string) string {
	for _, token := range tokens {
		claims := decodeJWT(token)
		if claims == nil {
			continue
		}
		if email, _ := claims["email"].(string); strings.TrimSpace(email) != "" {
			return strings.ToLower(email)
		}
	}
	return ""
}

func decodeJWT(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[1] == "" {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if json.Unmarshal(payload, &claims) != nil {
		return nil
	}
	return claims
}

func randomB64(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
