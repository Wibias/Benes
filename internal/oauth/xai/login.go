package xai

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

	"github.com/Wibias/Benes/internal/providers/antigravity"
)

const (
	ProviderID      = "xai"
	clientID        = "b1a00492-073a-47ea-816f-4c329264a828"
	scope           = "openid profile email offline_access grok-cli:access api:access"
	discoveryURL    = "https://auth.x.ai/.well-known/openid-configuration"
	redirectURI     = "http://127.0.0.1:56121/callback"
	PendingFileName = "oauth-pending-xai.json"
	pendingTTL      = 15 * time.Minute
	expirySkew      = 2 * time.Minute
)

var HTTPClient HTTPDoer = http.DefaultClient

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type PendingLogin struct {
	State       string    `json:"state"`
	Verifier    string    `json:"verifier"`
	RedirectURI string    `json:"redirectUri"`
	AuthEP      string    `json:"authorizationEndpoint"`
	TokenEP     string    `json:"tokenEndpoint"`
	CreatedAt   time.Time `json:"createdAt"`
}

type PendingFile struct{ Path string }

type CompletedLogin struct {
	Account antigravity.StoredAccount
}

func NewPendingLogin(ctx context.Context) (PendingLogin, error) {
	state, err := randomB64(16)
	if err != nil {
		return PendingLogin{}, err
	}
	verifier, err := randomB64(32)
	if err != nil {
		return PendingLogin{}, err
	}
	authEP, tokenEP, err := discover(ctx)
	if err != nil {
		return PendingLogin{}, err
	}
	return PendingLogin{State: state, Verifier: verifier, RedirectURI: redirectURI, AuthEP: authEP, TokenEP: tokenEP, CreatedAt: time.Now().UTC()}, nil
}

func discover(ctx context.Context) (authEP, tokenEP string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("xAI OAuth discovery failed: %d", resp.StatusCode)
	}
	var payload struct {
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return "", "", fmt.Errorf("xAI OAuth discovery response missing authorization/token endpoints")
	}
	authEP, err = validateEndpoint(payload.AuthorizationEndpoint)
	if err != nil {
		return "", "", err
	}
	tokenEP, err = validateEndpoint(payload.TokenEndpoint)
	if err != nil {
		return "", "", err
	}
	return authEP, tokenEP, nil
}

func validateEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme != "https" || (host != "x.ai" && !strings.HasSuffix(host, ".x.ai")) {
		return "", fmt.Errorf("xAI OAuth discovery returned an unexpected endpoint: %s", raw)
	}
	return parsed.String(), nil
}

func (p PendingLogin) AuthURL() (string, error) {
	if p.State == "" || p.Verifier == "" || p.AuthEP == "" {
		return "", fmt.Errorf("xAI login state is incomplete")
	}
	sum := sha256.Sum256([]byte(p.Verifier))
	query := url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {p.RedirectURI},
		"scope":                 {scope},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"state":                 {p.State},
	}
	return p.AuthEP + "?" + query.Encode(), nil
}

func (s PendingFile) Save(pending PendingLogin) error {
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("xAI pending login path is required")
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
			return PendingLogin{}, fmt.Errorf("xAI login was already consumed")
		}
		return PendingLogin{}, err
	}
	var pending PendingLogin
	if json.Unmarshal(raw, &pending) != nil || pending.Verifier == "" {
		return PendingLogin{}, fmt.Errorf("xAI login state is invalid")
	}
	if time.Since(pending.CreatedAt) > pendingTTL {
		_ = os.Remove(s.Path)
		return PendingLogin{}, fmt.Errorf("xAI login expired")
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
	code, state, err := parseCallback(callback)
	if err != nil {
		return CompletedLogin{}, err
	}
	if state != "" && state != pending.State {
		return CompletedLogin{}, fmt.Errorf("xAI login state mismatch")
	}
	account, err := exchange(ctx, pending, code)
	if err != nil {
		return CompletedLogin{}, err
	}
	if err := s.Consume(); err != nil {
		return CompletedLogin{}, err
	}
	return CompletedLogin{Account: account}, nil
}

func Persist(path string, account antigravity.StoredAccount) error {
	return antigravity.AppendStoredAccount(path, ProviderID, account)
}

func parseCallback(raw string) (code, state string, err error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", "", fmt.Errorf("xAI callback is required")
	}
	if strings.Contains(value, "://") || strings.HasPrefix(value, "/") {
		parsed, perr := url.Parse(value)
		if perr != nil {
			return "", "", perr
		}
		code = parsed.Query().Get("code")
		state = parsed.Query().Get("state")
		if code == "" && parsed.Fragment != "" {
			q, _ := url.ParseQuery(parsed.Fragment)
			code = q.Get("code")
			if state == "" {
				state = q.Get("state")
			}
		}
	} else {
		code = value
	}
	if code == "" {
		return "", "", fmt.Errorf("xAI callback is missing a code")
	}
	return code, state, nil
}

func exchange(ctx context.Context, pending PendingLogin, code string) (antigravity.StoredAccount, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {pending.RedirectURI},
		"code_verifier": {pending.Verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pending.TokenEP, strings.NewReader(form.Encode()))
	if err != nil {
		return antigravity.StoredAccount{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return antigravity.StoredAccount{}, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return antigravity.StoredAccount{}, fmt.Errorf("xAI token request failed: %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		IDToken      string `json:"id_token"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.AccessToken == "" || payload.RefreshToken == "" {
		return antigravity.StoredAccount{}, fmt.Errorf("xAI token response did not include required tokens")
	}
	expiresIn := payload.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	id, email := identity(payload.AccessToken, payload.IDToken)
	if id == "" {
		id = ProviderID
	}
	return antigravity.StoredAccount{ID: id, Token: payload.AccessToken, Refresh: payload.RefreshToken, Email: email, Expires: time.Now().Add(time.Duration(expiresIn)*time.Second - expirySkew).UnixMilli()}, nil
}

func identity(access, idToken string) (id, email string) {
	payload := decodeJWT(idToken)
	if payload == nil {
		payload = decodeJWT(access)
	}
	if payload == nil {
		return "", ""
	}
	id, _ = payload["sub"].(string)
	email, _ = payload["email"].(string)
	return strings.TrimSpace(id), strings.ToLower(strings.TrimSpace(email))
}

func decodeJWT(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	return payload
}

func randomB64(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
