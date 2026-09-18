package githubcopilot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/providers/antigravity"
)

const (
	ProviderID      = "github-copilot"
	clientID        = "Iv1.b507a08c87ecfe98"
	deviceCodeURL   = "https://github.com/login/device/code"
	accessTokenURL  = "https://github.com/login/oauth/access_token"
	copilotTokenURL = "https://api.github.com/copilot_internal/v2/token"
	githubUserURL   = "https://api.github.com/user"
	verifyOrigin    = "https://github.com"
	verifyPath      = "/login/device"
	PendingFileName = "oauth-pending-github-copilot.json"
	defaultTTL      = 15 * time.Minute
	defaultInterval = 5 * time.Second
	expirySkew      = 2 * time.Minute
	grantType       = "urn:ietf:params:oauth:grant-type:device_code"
)

var userCodeOK = regexp.MustCompile(`(?i)^[A-Z0-9-]+$`)

var (
	HTTPClient HTTPDoer = http.DefaultClient
	Sleep               = time.Sleep
	Now                 = time.Now
	Interval   time.Duration
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type PendingLogin struct {
	DeviceCode string    `json:"deviceCode"`
	UserCode   string    `json:"userCode"`
	VerifyURL  string    `json:"verifyUrl"`
	ExpiresAt  time.Time `json:"expiresAt"`
	IntervalMS int       `json:"intervalMs"`
}

type PendingFile struct{ Path string }

type CompletedLogin struct {
	Account antigravity.StoredAccount
}

func NewPendingLogin(ctx context.Context) (PendingLogin, error) {
	form := url.Values{"client_id": {clientID}, "scope": {"read:user"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deviceCodeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return PendingLogin{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "benes")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return PendingLogin{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PendingLogin{}, fmt.Errorf("GitHub Copilot device authorization failed (%d)", resp.StatusCode)
	}
	var payload struct {
		UserCode    string `json:"user_code"`
		DeviceCode  string `json:"device_code"`
		ExpiresIn   int    `json:"expires_in"`
		Interval    int    `json:"interval"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.UserCode == "" || payload.DeviceCode == "" {
		return PendingLogin{}, fmt.Errorf("GitHub Copilot device authorization response missing required fields")
	}
	verify, err := buildVerifyURL(payload.UserCode)
	if err != nil {
		return PendingLogin{}, err
	}
	ttl := defaultTTL
	if payload.ExpiresIn > 0 {
		ttl = time.Duration(payload.ExpiresIn) * time.Second
	}
	interval := defaultInterval
	if payload.Interval > 0 {
		interval = time.Duration(payload.Interval) * time.Second
	}
	return PendingLogin{DeviceCode: payload.DeviceCode, UserCode: payload.UserCode, VerifyURL: verify, ExpiresAt: Now().Add(ttl), IntervalMS: int(interval / time.Millisecond)}, nil
}

func buildVerifyURL(userCode string) (string, error) {
	code := strings.TrimSpace(userCode)
	if code == "" || !userCodeOK.MatchString(code) {
		return "", fmt.Errorf("GitHub Copilot device flow returned an invalid user code")
	}
	return verifyOrigin + verifyPath + "?user_code=" + url.QueryEscape(code), nil
}

func (p PendingLogin) AuthURL() string { return p.VerifyURL }

func (s PendingFile) Save(pending PendingLogin) error {
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("GitHub Copilot pending login path is required")
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
			return PendingLogin{}, fmt.Errorf("GitHub Copilot login was already consumed")
		}
		return PendingLogin{}, err
	}
	var pending PendingLogin
	if json.Unmarshal(raw, &pending) != nil || pending.DeviceCode == "" {
		return PendingLogin{}, fmt.Errorf("GitHub Copilot login state is invalid")
	}
	return pending, nil
}

func (s PendingFile) Consume() error {
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s PendingFile) Complete(ctx context.Context) (CompletedLogin, error) {
	pending, err := s.Load()
	if err != nil {
		return CompletedLogin{}, err
	}
	if Now().After(pending.ExpiresAt) {
		_ = s.Consume()
		return CompletedLogin{}, fmt.Errorf("GitHub Copilot device authorization expired")
	}
	githubAccess, durable, err := pollGithub(ctx, pending)
	if err != nil {
		return CompletedLogin{}, err
	}
	account, err := credentialsFromGithub(ctx, githubAccess, durable)
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

func pollGithub(ctx context.Context, pending PendingLogin) (access, durable string, err error) {
	wait := time.Duration(pending.IntervalMS) * time.Millisecond
	if wait < time.Second {
		wait = defaultInterval
	}
	if Interval != 0 {
		wait = Interval
	}
	for Now().Before(pending.ExpiresAt) {
		if err := waitCtx(ctx, wait); err != nil {
			return "", "", err
		}
		form := url.Values{"client_id": {clientID}, "device_code": {pending.DeviceCode}, "grant_type": {grantType}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, accessTokenURL, strings.NewReader(form.Encode()))
		if err != nil {
			return "", "", err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "benes")
		resp, err := HTTPClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return "", "", fmt.Errorf("Login cancelled")
			}
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		var payload struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			Error        string `json:"error"`
		}
		_ = json.Unmarshal(body, &payload)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && payload.AccessToken != "" {
			durable := payload.RefreshToken
			if durable == "" {
				durable = payload.AccessToken
			}
			return payload.AccessToken, durable, nil
		}
		switch payload.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			wait += 5 * time.Second
		case "expired_token":
			return "", "", fmt.Errorf("GitHub Copilot device authorization expired")
		case "access_denied":
			return "", "", fmt.Errorf("GitHub Copilot device authorization denied")
		default:
			if payload.Error != "" {
				return "", "", fmt.Errorf("GitHub Copilot device flow failed (%s)", payload.Error)
			}
			return "", "", fmt.Errorf("GitHub Copilot device token poll failed (%d)", resp.StatusCode)
		}
	}
	return "", "", fmt.Errorf("GitHub Copilot device flow timed out")
}

func credentialsFromGithub(ctx context.Context, githubAccess, durable string) (antigravity.StoredAccount, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotTokenURL, nil)
	if err != nil {
		return antigravity.StoredAccount{}, err
	}
	req.Header.Set("Authorization", "token "+githubAccess)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "benes")
	req.Header.Set("Editor-Version", "benes/0.1.0")
	req.Header.Set("Editor-Plugin-Version", "benes/0.1.0")
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return antigravity.StoredAccount{}, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return antigravity.StoredAccount{}, fmt.Errorf("GitHub Copilot token exchange failed (%d)", resp.StatusCode)
	}
	var copilot struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
		RefreshIn int    `json:"refresh_in"`
	}
	if json.Unmarshal(body, &copilot) != nil || strings.TrimSpace(copilot.Token) == "" {
		return antigravity.StoredAccount{}, fmt.Errorf("GitHub Copilot token exchange missing token")
	}
	expires := Now().Add(25*time.Minute - expirySkew).UnixMilli()
	if copilot.ExpiresAt > 0 {
		expires = copilot.ExpiresAt*1000 - expirySkew.Milliseconds()
	} else if copilot.RefreshIn > 0 {
		expires = Now().Add(time.Duration(copilot.RefreshIn)*time.Second - expirySkew).UnixMilli()
	}
	id, email, err := githubIdentity(ctx, githubAccess)
	if err != nil {
		return antigravity.StoredAccount{}, err
	}
	return antigravity.StoredAccount{ID: id, Token: copilot.Token, Refresh: durable, Email: email, Expires: expires}, nil
}

func githubIdentity(ctx context.Context, githubAccess string) (id, email string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubUserURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+githubAccess)
	req.Header.Set("User-Agent", "benes")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", "", err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("GitHub Copilot identity lookup failed (%d)", resp.StatusCode)
	}
	var user struct {
		Login string  `json:"login"`
		ID    float64 `json:"id"`
		Email string  `json:"email"`
	}
	if json.Unmarshal(body, &user) != nil {
		return "", "", fmt.Errorf("Could not verify GitHub account identity — retry the login")
	}
	if user.ID != 0 {
		id = fmt.Sprintf("%.0f", user.ID)
	} else {
		id = strings.TrimSpace(user.Login)
	}
	if id == "" {
		return "", "", fmt.Errorf("Could not verify GitHub account identity — retry the login")
	}
	if strings.Contains(user.Email, "@") {
		email = user.Email
	}
	return id, email, nil
}

func waitCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		if ctx.Err() != nil {
			return fmt.Errorf("Login cancelled")
		}
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("Login cancelled")
	case <-timer.C:
		Sleep(0)
		return nil
	}
}
