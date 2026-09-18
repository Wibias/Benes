package anthropic

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
	ProviderID      = "anthropic"
	clientID        = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	authorizeURL    = "https://claude.ai/oauth/authorize"
	tokenURL        = "https://api.anthropic.com/v1/oauth/token"
	redirectURI     = "http://localhost:54545/callback"
	scopes          = "org:create_api_key user:profile user:inference"
	PendingFileName = "oauth-pending-anthropic.json"
	pendingTTL      = 15 * time.Minute
	expirySkew      = 5 * time.Minute
)

var HTTPClient HTTPDoer = http.DefaultClient

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type PendingLogin struct {
	State       string    `json:"state"`
	Verifier    string    `json:"verifier"`
	RedirectURI string    `json:"redirectUri"`
	CreatedAt   time.Time `json:"createdAt"`
}

type PendingFile struct{ Path string }

type CompletedLogin struct {
	Account antigravity.StoredAccount
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
	return PendingLogin{State: state, Verifier: verifier, RedirectURI: redirectURI, CreatedAt: time.Now().UTC()}, nil
}

func (p PendingLogin) AuthURL() (string, error) {
	if p.State == "" || p.Verifier == "" {
		return "", fmt.Errorf("Anthropic login state is incomplete")
	}
	sum := sha256.Sum256([]byte(p.Verifier))
	query := url.Values{
		"code":                  {"true"},
		"client_id":             {clientID},
		"response_type":         {"code"},
		"redirect_uri":          {p.RedirectURI},
		"scope":                 {scopes},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"state":                 {p.State},
	}
	return authorizeURL + "?" + query.Encode(), nil
}

func (s PendingFile) Save(pending PendingLogin) error {
	if strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("Anthropic pending login path is required")
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
			return PendingLogin{}, fmt.Errorf("Anthropic login was already consumed")
		}
		return PendingLogin{}, err
	}
	var pending PendingLogin
	if json.Unmarshal(raw, &pending) != nil || pending.Verifier == "" {
		return PendingLogin{}, fmt.Errorf("Anthropic login state is invalid")
	}
	if time.Since(pending.CreatedAt) > pendingTTL {
		_ = os.Remove(s.Path)
		return PendingLogin{}, fmt.Errorf("Anthropic login expired")
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
	account, err := exchange(ctx, pending, code, state)
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

func parseCallback(raw, expectedState string) (code, state string, err error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", "", fmt.Errorf("Anthropic callback is required")
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
		if hash := strings.Index(code, "#"); hash >= 0 {
			if frag := code[hash+1:]; frag != "" {
				state = frag
			}
			code = code[:hash]
		}
	}
	if code == "" {
		return "", "", fmt.Errorf("Anthropic callback is missing a code")
	}
	return code, state, nil
}

func exchange(ctx context.Context, pending PendingLogin, code, state string) (antigravity.StoredAccount, error) {
	bodyMap := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     clientID,
		"code":          code,
		"state":         state,
		"redirect_uri":  pending.RedirectURI,
		"code_verifier": pending.Verifier,
	}
	raw, _ := json.Marshal(bodyMap)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(string(raw)))
	if err != nil {
		return antigravity.StoredAccount{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := HTTPClient.Do(req)
	if err != nil {
		return antigravity.StoredAccount{}, err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return antigravity.StoredAccount{}, fmt.Errorf("Anthropic OAuth HTTP %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Account      struct {
			UUID         string `json:"uuid"`
			EmailAddress string `json:"email_address"`
		} `json:"account"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.AccessToken == "" || payload.RefreshToken == "" {
		return antigravity.StoredAccount{}, fmt.Errorf("Anthropic OAuth returned invalid JSON")
	}
	expiresIn := payload.ExpiresIn
	if expiresIn < 0 {
		expiresIn = 3600
	}
	id := strings.TrimSpace(payload.Account.UUID)
	if id == "" {
		id = ProviderID
	}
	return antigravity.StoredAccount{ID: id, Token: payload.AccessToken, Refresh: payload.RefreshToken, Email: payload.Account.EmailAddress, Expires: time.Now().Add(time.Duration(expiresIn)*time.Second - expirySkew).UnixMilli()}, nil
}

func randomB64(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
