package antigravity

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
	authEndpoint    = "https://accounts.google.com/o/oauth2/v2/auth"
	defaultRedirect = "http://127.0.0.1:51121/callback"
	pendingTTL      = 15 * time.Minute
	loginScopes     = "https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile https://www.googleapis.com/auth/cclog https://www.googleapis.com/auth/experimentsandconfigs"
)

type PendingLogin struct {
	State       string    `json:"state"`
	Verifier    string    `json:"verifier"`
	RedirectURI string    `json:"redirectUri"`
	CreatedAt   time.Time `json:"createdAt"`
}

type PendingFile struct {
	Path string
}

func NewPendingLogin() (PendingLogin, error) {
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
		RedirectURI: defaultRedirect,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func (p PendingLogin) AuthURL() (string, error) {
	if strings.TrimSpace(p.State) == "" || strings.TrimSpace(p.Verifier) == "" {
		return "", fmt.Errorf("Cloud Code Assist login state is incomplete")
	}
	sum := sha256.Sum256([]byte(p.Verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {oauthClientID},
		"redirect_uri":          {p.RedirectURI},
		"scope":                 {loginScopes},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"state":                 {p.State},
	}
	return authEndpoint + "?" + query.Encode(), nil
}

func (s PendingFile) Save(pending PendingLogin) error {
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("Cloud Code Assist pending login path is required")
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
			return PendingLogin{}, fmt.Errorf("Cloud Code Assist login was already consumed")
		}
		return PendingLogin{}, err
	}
	var pending PendingLogin
	if json.Unmarshal(raw, &pending) != nil || strings.TrimSpace(pending.State) == "" || strings.TrimSpace(pending.Verifier) == "" {
		return PendingLogin{}, fmt.Errorf("Cloud Code Assist login state is invalid")
	}
	return pending, nil
}

func (s PendingFile) Consume() error {
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

type CompletedLogin struct {
	Account Account
	Refresh string
}

func (s PendingFile) Complete(ctx context.Context, client *http.Client, raw string) (CompletedLogin, error) {
	pending, err := s.Load()
	if err != nil {
		return CompletedLogin{}, err
	}
	if time.Since(pending.CreatedAt) > pendingTTL {
		_ = s.Consume()
		return CompletedLogin{}, fmt.Errorf("Cloud Code Assist login expired")
	}
	code, state, err := parseCallback(raw)
	if err != nil {
		return CompletedLogin{}, err
	}
	if state != "" && state != pending.State {
		return CompletedLogin{}, fmt.Errorf("Cloud Code Assist login state did not match")
	}
	if state == "" && looksLikeURL(raw) {
		return CompletedLogin{}, fmt.Errorf("Cloud Code Assist login state did not match")
	}
	access, refresh, err := ExchangeAuthorizationCode(ctx, client, code, pending.Verifier, pending.RedirectURI)
	if err != nil {
		return CompletedLogin{}, err
	}
	project, err := DiscoverProject(ctx, client, DailyAPI, access)
	if err != nil || strings.TrimSpace(project) == "" {
		return CompletedLogin{}, fmt.Errorf("Cloud Code Assist login could not discover a project")
	}
	if err := s.Consume(); err != nil {
		return CompletedLogin{}, err
	}
	return CompletedLogin{
		Account: Account{ID: project, Token: access, ProjectID: project},
		Refresh: refresh,
	}, nil
}

func ExchangeAuthorizationCode(ctx context.Context, client *http.Client, code, verifier, redirectURI string) (access, refresh string, err error) {
	if client == nil {
		return "", "", fmt.Errorf("Cloud Code Assist HTTP client is required")
	}
	code = strings.TrimSpace(code)
	if code == "" || strings.TrimSpace(verifier) == "" {
		return "", "", fmt.Errorf("Cloud Code Assist authorization code is required")
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {oauthClientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("Cloud Code Assist token exchange returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if json.Unmarshal(raw, &payload) != nil || strings.TrimSpace(payload.AccessToken) == "" {
		return "", "", fmt.Errorf("Cloud Code Assist token exchange did not include an access token")
	}
	return payload.AccessToken, payload.RefreshToken, nil
}

func AppendAccount(path string, account Account, refresh string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("auth store path is required")
	}
	if strings.TrimSpace(account.Token) == "" {
		return fmt.Errorf("Cloud Code Assist account token is required")
	}
	existing := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &existing)
	} else if !os.IsNotExist(err) {
		return err
	}
	id := strings.TrimSpace(account.ID)
	if id == "" {
		id = strings.TrimSpace(account.ProjectID)
	}
	if id == "" {
		id = AuthStoreProvider
	}
	entry, _ := json.Marshal(map[string]any{
		"activeAccountId": id,
		"accounts": []map[string]any{{
			"id": id,
			"credential": map[string]any{
				"access":    account.Token,
				"refresh":   refresh,
				"expires":   time.Now().Add(50 * time.Minute).UnixMilli(),
				"projectId": account.ProjectID,
			},
		}},
	})
	existing[AuthStoreProvider] = entry
	out, err := json.Marshal(existing)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

func parseCallback(raw string) (code, state string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("Cloud Code Assist authorization code is required")
	}
	if !looksLikeURL(raw) {
		return raw, "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("Cloud Code Assist login callback is invalid")
	}
	code = strings.TrimSpace(parsed.Query().Get("code"))
	state = strings.TrimSpace(parsed.Query().Get("state"))
	if code == "" {
		return "", "", fmt.Errorf("Cloud Code Assist authorization code is required")
	}
	return code, state, nil
}

func looksLikeURL(raw string) bool {
	return strings.Contains(raw, "://") || strings.HasPrefix(raw, "http")
}

func randomB64(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
