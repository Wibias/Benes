package kimi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/providers/antigravity"
)

const (
	ProviderID      = "kimi"
	clientID        = "17e5f671-d194-4dfb-9706-5516cb48c098"
	defaultHost     = "https://auth.kimi.com"
	cliVersion      = "0.14.0"
	deviceFileName  = "kimi-device-id"
	PendingFileName = "oauth-pending-kimi.json"
	defaultTTL      = 15 * time.Minute
	defaultInterval = 5 * time.Second
	expirySkew      = 5 * time.Minute
	grantType       = "urn:ietf:params:oauth:grant-type:device_code"
)

var (
	HTTPClient HTTPDoer      = http.DefaultClient
	Sleep                    = time.Sleep
	Now                      = time.Now
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
	CreatedAt  time.Time `json:"createdAt"`
}

type PendingFile struct{ Path string }

type CompletedLogin struct {
	Account antigravity.StoredAccount
}

func oauthHost() string {
	if v := strings.TrimSpace(os.Getenv("KIMI_CODE_OAUTH_HOST")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("KIMI_OAUTH_HOST")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultHost
}

func NewPendingLogin(ctx context.Context, home string) (PendingLogin, error) {
	form := url.Values{"client_id": {clientID}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthHost()+"/api/oauth/device_authorization", strings.NewReader(form.Encode()))
	if err != nil {
		return PendingLogin{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range commonHeaders(home) {
		req.Header.Set(k, v)
	}
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return PendingLogin{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PendingLogin{}, fmt.Errorf("Kimi device authorization failed: %d", resp.StatusCode)
	}
	var payload struct {
		UserCode                string `json:"user_code"`
		DeviceCode              string `json:"device_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.UserCode == "" || payload.DeviceCode == "" || payload.VerificationURI == "" {
		return PendingLogin{}, fmt.Errorf("Kimi device authorization response missing required fields")
	}
	verify := payload.VerificationURIComplete
	if verify == "" {
		verify = payload.VerificationURI
	}
	ttl := defaultTTL
	if payload.ExpiresIn > 0 {
		ttl = time.Duration(payload.ExpiresIn) * time.Second
	}
	interval := defaultInterval
	if payload.Interval > 0 {
		interval = time.Duration(payload.Interval) * time.Second
	}
	return PendingLogin{
		DeviceCode: payload.DeviceCode,
		UserCode:   payload.UserCode,
		VerifyURL:  verify,
		ExpiresAt:  Now().Add(ttl),
		IntervalMS: int(interval / time.Millisecond),
		CreatedAt:  Now().UTC(),
	}, nil
}

func (p PendingLogin) AuthURL() string { return p.VerifyURL }

func (s PendingFile) Save(pending PendingLogin) error {
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("Kimi pending login path is required")
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
			return PendingLogin{}, fmt.Errorf("Kimi login was already consumed")
		}
		return PendingLogin{}, err
	}
	var pending PendingLogin
	if json.Unmarshal(raw, &pending) != nil || pending.DeviceCode == "" {
		return PendingLogin{}, fmt.Errorf("Kimi login state is invalid")
	}
	return pending, nil
}

func (s PendingFile) Consume() error {
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s PendingFile) Complete(ctx context.Context, home string) (CompletedLogin, error) {
	pending, err := s.Load()
	if err != nil {
		return CompletedLogin{}, err
	}
	if Now().After(pending.ExpiresAt) {
		_ = s.Consume()
		return CompletedLogin{}, fmt.Errorf("Kimi device authorization expired")
	}
	account, err := pollToken(ctx, pending, home)
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

func pollToken(ctx context.Context, pending PendingLogin, home string) (antigravity.StoredAccount, error) {
	wait := time.Duration(pending.IntervalMS) * time.Millisecond
	if wait < time.Second {
		wait = defaultInterval
	}
	if Interval != 0 {
		wait = Interval
	}
	for Now().Before(pending.ExpiresAt) {
		if err := waitCtx(ctx, wait); err != nil {
			return antigravity.StoredAccount{}, err
		}
		form := url.Values{"client_id": {clientID}, "device_code": {pending.DeviceCode}, "grant_type": {grantType}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthHost()+"/api/oauth/token", strings.NewReader(form.Encode()))
		if err != nil {
			return antigravity.StoredAccount{}, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for k, v := range commonHeaders(home) {
			req.Header.Set(k, v)
		}
		resp, err := HTTPClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return antigravity.StoredAccount{}, fmt.Errorf("Login cancelled")
			}
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		var payload struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
			Error        string `json:"error"`
			Interval     int    `json:"interval"`
		}
		_ = json.Unmarshal(body, &payload)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && payload.AccessToken != "" {
			if payload.RefreshToken == "" || payload.ExpiresIn < 0 {
				return antigravity.StoredAccount{}, fmt.Errorf("Kimi token response missing required fields")
			}
			id, email := identityFromTokens(payload.AccessToken, payload.RefreshToken)
			if id == "" {
				id = ProviderID
			}
			return antigravity.StoredAccount{ID: id, Token: payload.AccessToken, Refresh: payload.RefreshToken, Email: email, Expires: Now().Add(time.Duration(payload.ExpiresIn)*time.Second - expirySkew).UnixMilli()}, nil
		}
		switch payload.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			wait += 5 * time.Second
		case "expired_token":
			return antigravity.StoredAccount{}, fmt.Errorf("Kimi device authorization expired")
		case "access_denied":
			return antigravity.StoredAccount{}, fmt.Errorf("Kimi device authorization denied")
		default:
			if payload.Error != "" {
				return antigravity.StoredAccount{}, fmt.Errorf("Kimi device flow failed: %s", payload.Error)
			}
			return antigravity.StoredAccount{}, fmt.Errorf("Kimi device flow failed: %d", resp.StatusCode)
		}
	}
	return antigravity.StoredAccount{}, fmt.Errorf("Kimi device flow timed out")
}

func identityFromTokens(access, refresh string) (id, email string) {
	parse := func(token string) map[string]any {
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
	a, r := parse(access), parse(refresh)
	str := func(m map[string]any, key string) string {
		if m == nil {
			return ""
		}
		v, _ := m[key].(string)
		return strings.TrimSpace(v)
	}
	id = str(a, "user_id")
	if id == "" {
		id = str(r, "user_id")
	}
	if id == "" {
		id = str(a, "sub")
	}
	if id == "" {
		id = str(r, "sub")
	}
	email = strings.ToLower(str(a, "email"))
	if email == "" {
		email = strings.ToLower(str(r, "email"))
	}
	return id, email
}

func commonHeaders(home string) map[string]string {
	host, _ := os.Hostname()
	return map[string]string{
		"User-Agent": "KimiCLI/" + cliVersion, "X-Msh-Platform": "kimi_code_cli", "X-Msh-Version": cliVersion,
		"X-Msh-Device-Name": host, "X-Msh-Device-Model": runtime.GOOS + " " + runtime.GOARCH, "X-Msh-Os-Version": runtime.GOOS, "X-Msh-Device-Id": deviceID(home),
	}
}

func deviceID(home string) string {
	path := filepath.Join(home, deviceFileName)
	if raw, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(raw)); id != "" {
			return id
		}
	}
	id := fmt.Sprintf("%d", Now().UnixNano())
	_ = os.MkdirAll(home, 0o700)
	_ = os.WriteFile(path, []byte(id+"\n"), 0o600)
	return id
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
