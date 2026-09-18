package nous

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
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/providers/antigravity"
)

const (
	ProviderID      = "nous"
	clientID        = "hermes-cli"
	scope           = "inference:invoke"
	portalURL       = "https://portal.nousresearch.com"
	PendingFileName = "oauth-pending-nous.json"
	defaultTTL      = 15 * time.Minute
	defaultInterval = 5 * time.Second
	expirySkew      = 2 * time.Minute
	grantType       = "urn:ietf:params:oauth:grant-type:device_code"
)

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
	form := url.Values{"client_id": {clientID}, "scope": {scope}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, portalURL+"/api/oauth/device/code", strings.NewReader(form.Encode()))
	if err != nil {
		return PendingLogin{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return PendingLogin{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PendingLogin{}, fmt.Errorf("Nous Portal device authorization failed: %d", resp.StatusCode)
	}
	var payload struct {
		UserCode                string `json:"user_code"`
		DeviceCode              string `json:"device_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.UserCode == "" || payload.DeviceCode == "" {
		return PendingLogin{}, fmt.Errorf("Nous Portal device authorization response missing required fields")
	}
	verify := payload.VerificationURIComplete
	if verify == "" {
		verify = payload.VerificationURI
	}
	if verify == "" {
		return PendingLogin{}, fmt.Errorf("Nous Portal device authorization response missing required fields")
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

func (p PendingLogin) AuthURL() string { return p.VerifyURL }

func (s PendingFile) Save(pending PendingLogin) error {
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("Nous pending login path is required")
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
			return PendingLogin{}, fmt.Errorf("Nous login was already consumed")
		}
		return PendingLogin{}, err
	}
	var pending PendingLogin
	if json.Unmarshal(raw, &pending) != nil || pending.DeviceCode == "" {
		return PendingLogin{}, fmt.Errorf("Nous login state is invalid")
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
		return CompletedLogin{}, fmt.Errorf("Nous Portal device authorization expired")
	}
	account, err := pollToken(ctx, pending)
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

func pollToken(ctx context.Context, pending PendingLogin) (antigravity.StoredAccount, error) {
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
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, portalURL+"/api/oauth/token", strings.NewReader(form.Encode()))
		if err != nil {
			return antigravity.StoredAccount{}, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
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
		}
		_ = json.Unmarshal(body, &payload)
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && payload.AccessToken != "" {
			if payload.RefreshToken == "" {
				return antigravity.StoredAccount{}, fmt.Errorf("Nous Portal token response did not include a refresh token")
			}
			id, email := identity(payload.AccessToken)
			if id == "" {
				id = ProviderID
			}
			exp := Now().Add(12 * time.Hour)
			if payload.ExpiresIn > 0 {
				exp = Now().Add(time.Duration(payload.ExpiresIn) * time.Second)
			}
			if jwtExp := jwtExpiry(payload.AccessToken); jwtExp > 0 {
				exp = time.UnixMilli(jwtExp)
			}
			return antigravity.StoredAccount{ID: id, Token: payload.AccessToken, Refresh: payload.RefreshToken, Email: email, Expires: exp.Add(-expirySkew).UnixMilli()}, nil
		}
		switch payload.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			wait += 5 * time.Second
		case "expired_token":
			return antigravity.StoredAccount{}, fmt.Errorf("Nous Portal device authorization expired")
		case "access_denied":
			return antigravity.StoredAccount{}, fmt.Errorf("Nous Portal device authorization denied")
		default:
			if payload.Error != "" {
				return antigravity.StoredAccount{}, fmt.Errorf("Nous Portal device authorization failed (%s)", payload.Error)
			}
			return antigravity.StoredAccount{}, fmt.Errorf("Nous Portal token request failed: %d", resp.StatusCode)
		}
	}
	return antigravity.StoredAccount{}, fmt.Errorf("Nous Portal device flow timed out")
}

func identity(access string) (id, email string) {
	payload := decodeJWT(access)
	if payload == nil {
		return "", ""
	}
	id, _ = payload["sub"].(string)
	email, _ = payload["email"].(string)
	return strings.TrimSpace(id), strings.ToLower(strings.TrimSpace(email))
}

func jwtExpiry(token string) int64 {
	payload := decodeJWT(token)
	if payload == nil {
		return 0
	}
	exp, ok := payload["exp"].(float64)
	if !ok || exp <= 0 {
		return 0
	}
	return int64(exp * 1000)
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
