package cursor

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
	ProviderID      = "cursor"
	loginURL        = "https://cursor.com/loginDeepControl"
	pollURL         = "https://api2.cursor.sh/auth/poll"
	pendingTTL      = 15 * time.Minute
	pollMaxAttempts = 150
	pollBaseDelay   = time.Second
	pollMaxDelay    = 10 * time.Second
	expirySkew      = 5 * time.Minute
	fallbackTTL     = time.Hour
	PendingFileName = "oauth-pending-cursor.json"
)

var (
	HTTPClient  HTTPDoer      = http.DefaultClient
	MaxAttempts               = pollMaxAttempts
	BaseDelay   time.Duration = pollBaseDelay
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type PendingLogin struct {
	UUID      string    `json:"uuid"`
	Verifier  string    `json:"verifier"`
	Challenge string    `json:"challenge"`
	CreatedAt time.Time `json:"createdAt"`
}

type PendingFile struct {
	Path string
}

type CompletedLogin struct {
	Account antigravity.StoredAccount
}

func NewPendingLogin() (PendingLogin, error) {
	uuid, err := randomUUID()
	if err != nil {
		return PendingLogin{}, err
	}
	verifierBytes := make([]byte, 96)
	if _, err := rand.Read(verifierBytes); err != nil {
		return PendingLogin{}, err
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	sum := sha256.Sum256([]byte(verifier))
	return PendingLogin{
		UUID:      uuid,
		Verifier:  verifier,
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]),
		CreatedAt: time.Now().UTC(),
	}, nil
}

func (p PendingLogin) AuthURL() string {
	query := url.Values{
		"challenge":      {p.Challenge},
		"uuid":           {p.UUID},
		"mode":           {"login"},
		"redirectTarget": {"cli"},
	}
	return loginURL + "?" + query.Encode()
}

func (s PendingFile) Save(pending PendingLogin) error {
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("Cursor pending login path is required")
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
			return PendingLogin{}, fmt.Errorf("Cursor login was already consumed")
		}
		return PendingLogin{}, err
	}
	var pending PendingLogin
	if json.Unmarshal(raw, &pending) != nil || strings.TrimSpace(pending.UUID) == "" || strings.TrimSpace(pending.Verifier) == "" {
		return PendingLogin{}, fmt.Errorf("Cursor login state is invalid")
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
	if time.Since(pending.CreatedAt) > pendingTTL {
		_ = s.Consume()
		return CompletedLogin{}, fmt.Errorf("Cursor login expired")
	}
	access, refresh, err := pollAuth(ctx, pending.UUID, pending.Verifier)
	if err != nil {
		return CompletedLogin{}, err
	}
	account := credentialsFromTokens(access, refresh)
	if err := s.Consume(); err != nil {
		return CompletedLogin{}, err
	}
	return CompletedLogin{Account: account}, nil
}

func Persist(path string, account antigravity.StoredAccount) error {
	return antigravity.AppendStoredAccount(path, ProviderID, account)
}

func pollAuth(ctx context.Context, uuid, verifier string) (access, refresh string, err error) {
	delay := BaseDelay
	consecutiveErrors := 0
	for attempt := 0; attempt < MaxAttempts; attempt++ {
		if err := wait(ctx, delay); err != nil {
			return "", "", err
		}
		reqURL := pollURL + "?uuid=" + url.QueryEscape(uuid) + "&verifier=" + url.QueryEscape(verifier)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return "", "", err
		}
		resp, err := HTTPClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return "", "", fmt.Errorf("Cursor login cancelled")
			}
			consecutiveErrors++
			if consecutiveErrors >= 3 {
				return "", "", fmt.Errorf("Too many consecutive errors during Cursor auth polling")
			}
			delay = nextDelay(delay)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			consecutiveErrors = 0
			delay = nextDelay(delay)
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var payload struct {
				AccessToken  string `json:"accessToken"`
				RefreshToken string `json:"refreshToken"`
			}
			if json.Unmarshal(body, &payload) != nil || strings.TrimSpace(payload.AccessToken) == "" || strings.TrimSpace(payload.RefreshToken) == "" {
				return "", "", fmt.Errorf("Cursor auth response missing tokens")
			}
			return payload.AccessToken, payload.RefreshToken, nil
		}
		return "", "", fmt.Errorf("Cursor auth poll failed: %d", resp.StatusCode)
	}
	return "", "", fmt.Errorf("Cursor authentication polling timeout")
}

func credentialsFromTokens(access, refresh string) antigravity.StoredAccount {
	payload := decodeJWT(access)
	if payload == nil {
		payload = decodeJWT(refresh)
	}
	id := jwtIdentity(payload)
	if id == "" {
		id = ProviderID
	}
	return antigravity.StoredAccount{
		ID:      id,
		Token:   access,
		Refresh: refresh,
		Email:   jwtEmail(payload),
		Expires: tokenExpiry(access),
	}
}

func decodeJWT(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		padded := parts[1]
		if m := len(padded) % 4; m != 0 {
			padded += strings.Repeat("=", 4-m)
		}
		raw, err = base64.URLEncoding.DecodeString(padded)
		if err != nil {
			return nil
		}
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	return payload
}

func jwtIdentity(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	switch v := payload["sub"].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%.0f", v)
		}
	}
	return ""
}

func jwtEmail(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	email, _ := payload["email"].(string)
	return strings.ToLower(strings.TrimSpace(email))
}

func tokenExpiry(token string) int64 {
	payload := decodeJWT(token)
	if payload != nil {
		if exp, ok := payload["exp"].(float64); ok && exp > 0 {
			return int64(exp*1000) - expirySkew.Milliseconds()
		}
	}
	return time.Now().Add(fallbackTTL - expirySkew).UnixMilli()
}

func nextDelay(current time.Duration) time.Duration {
	next := time.Duration(float64(current) * 1.2)
	if next > pollMaxDelay {
		return pollMaxDelay
	}
	return next
}

func wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		if ctx.Err() != nil {
			return fmt.Errorf("Cursor login cancelled")
		}
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("Cursor login cancelled")
	case <-timer.C:
		return nil
	}
}

func randomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
