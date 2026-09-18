package commandcode

import (
	"context"
	"crypto/rand"
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
	ProviderID      = "command-code"
	studioURL       = "https://commandcode.ai"
	whoamiURL       = "https://api.commandcode.ai/alpha/whoami"
	callbackURL     = "http://127.0.0.1:5959/callback"
	PendingFileName = "oauth-pending-command-code.json"
	pendingTTL      = 15 * time.Minute
)

var HTTPClient HTTPDoer = http.DefaultClient

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type PendingLogin struct {
	State     string    `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
}

type PendingFile struct{ Path string }

type CompletedLogin struct {
	Account antigravity.StoredAccount
}

func NewPendingLogin() (PendingLogin, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return PendingLogin{}, err
	}
	return PendingLogin{State: base64.RawURLEncoding.EncodeToString(buf), CreatedAt: time.Now().UTC()}, nil
}

func (p PendingLogin) AuthURL() string {
	q := url.Values{"callback": {callbackURL}, "state": {p.State}}
	return studioURL + "/studio/auth/cli?" + q.Encode()
}

func (s PendingFile) Save(pending PendingLogin) error {
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("Command Code pending login path is required")
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
			return PendingLogin{}, fmt.Errorf("Command Code login was already consumed")
		}
		return PendingLogin{}, err
	}
	var pending PendingLogin
	if json.Unmarshal(raw, &pending) != nil || pending.State == "" {
		return PendingLogin{}, fmt.Errorf("Command Code login state is invalid")
	}
	if time.Since(pending.CreatedAt) > pendingTTL {
		_ = os.Remove(s.Path)
		return PendingLogin{}, fmt.Errorf("Command Code login expired")
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
	apiKey, userID, err := parseInput(ctx, callback, pending.State)
	if err != nil {
		return CompletedLogin{}, err
	}
	if err := s.Consume(); err != nil {
		return CompletedLogin{}, err
	}
	return CompletedLogin{Account: antigravity.StoredAccount{ID: userID, Token: apiKey, Refresh: apiKey, Expires: 1<<62 - 1}}, nil
}

func Persist(path string, account antigravity.StoredAccount) error {
	return antigravity.AppendStoredAccount(path, ProviderID, account)
}

func parseInput(ctx context.Context, raw, expectedState string) (apiKey, userID string, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", fmt.Errorf("Command Code callback is required")
	}
	if strings.HasPrefix(trimmed, "{") {
		var body struct {
			APIKey   string `json:"apiKey"`
			UserID   string `json:"userId"`
			UserName string `json:"userName"`
			KeyName  string `json:"keyName"`
			State    string `json:"state"`
		}
		if json.Unmarshal([]byte(trimmed), &body) != nil {
			return "", "", fmt.Errorf("Command Code callback must be an object")
		}
		if body.State != expectedState {
			return "", "", fmt.Errorf("Command Code OAuth state mismatch")
		}
		if body.APIKey == "" || body.UserID == "" || body.UserName == "" || body.KeyName == "" {
			return "", "", fmt.Errorf("Command Code callback missing required fields")
		}
		return body.APIKey, body.UserID, nil
	}
	apiKey = trimmed
	if strings.Contains(trimmed, "://") {
		parsed, perr := url.Parse(trimmed)
		if perr != nil {
			return "", "", perr
		}
		apiKey = parsed.Query().Get("code")
		if parsed.Query().Get("state") != expectedState {
			return "", "", fmt.Errorf("Command Code OAuth state mismatch")
		}
	}
	if apiKey == "" {
		return "", "", fmt.Errorf("Command Code callback is missing a key")
	}
	userID, err = whoami(ctx, apiKey)
	if err != nil {
		return "", "", err
	}
	return apiKey, userID, nil
}

func whoami(ctx context.Context, apiKey string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, whoamiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Could not verify Command Code identity")
	}
	var payload struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if json.Unmarshal(body, &payload) != nil || strings.TrimSpace(payload.User.ID) == "" {
		return "", fmt.Errorf("Could not verify Command Code identity")
	}
	return payload.User.ID, nil
}
