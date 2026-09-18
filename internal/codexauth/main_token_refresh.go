package codexauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/store/atomicfile"
	"github.com/Wibias/Benes/internal/transport"
)

var ErrMainAuthChangedDuringRefresh = errors.New("native Codex auth.json changed during refresh")

type MainCredentialMaterializer interface {
	MainCredentialReader
	Ensure(ctx context.Context, now time.Time, force bool) (MainCredentialResult, error)
}

func (s *MainCredentialSource) Ensure(ctx context.Context, now time.Time, force bool) (MainCredentialResult, error) {
	if err := ctx.Err(); err != nil {
		return MainCredentialResult{}, err
	}
	loaded := s.load(now)
	if loaded.result.Status == MainCredentialOK && !force {
		return loaded.result, nil
	}
	if strings.TrimSpace(loaded.refreshToken) == "" {
		return loaded.result, nil
	}
	if loaded.result.Status != MainCredentialOK && loaded.result.Status != MainCredentialExpired {
		return loaded.result, nil
	}

	lockCtx, cancelLock := context.WithTimeout(ctx, s.refreshLockWait)
	lock, err := acquireManagedStoreMutationLock(lockCtx, s.refreshLockPath())
	cancelLock()
	if err != nil {
		return MainCredentialResult{}, err
	}
	defer lock.Close()

	loaded = s.load(now)
	if loaded.result.Status == MainCredentialOK && !force {
		return loaded.result, nil
	}
	if strings.TrimSpace(loaded.refreshToken) == "" {
		return loaded.result, nil
	}

	refreshed, err := s.requestRefresh(ctx, loaded.refreshToken, now)
	if err != nil {
		return MainCredentialResult{}, err
	}
	if err := s.publishRefreshedTokens(loaded.raw, refreshed); err != nil {
		if errors.Is(err, ErrMainAuthChangedDuringRefresh) {
			current := s.Read(now)
			if current.Status == MainCredentialOK {
				return current, nil
			}
		}
		return MainCredentialResult{}, err
	}
	return s.Read(now), nil
}

func (s *MainCredentialSource) requestRefresh(ctx context.Context, refreshToken string, now time.Time) (ManagedCredential, error) {
	client := s.httpClient
	if client == nil {
		client = transport.DefaultUnpinnedClient()
	}
	clientCopy := *client
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	refreshCtx, cancel := context.WithTimeout(ctx, s.refreshHTTPTimeout)
	defer cancel()
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {managedTokenClientID},
		"refresh_token": {refreshToken},
	}
	endpoint := s.refreshURL
	if strings.TrimSpace(endpoint) == "" {
		endpoint = managedTokenRefreshEndpoint
	}
	request, err := http.NewRequestWithContext(refreshCtx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return ManagedCredential{}, fmt.Errorf("build native Main token refresh request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := clientCopy.Do(request)
	if err != nil {
		return ManagedCredential{}, fmt.Errorf("native Main token refresh request failed: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, managedTokenMaxBodyBytes+1))
	if err != nil {
		return ManagedCredential{}, fmt.Errorf("read native Main token refresh response: %w", err)
	}
	if len(body) > managedTokenMaxBodyBytes {
		return ManagedCredential{}, ErrManagedCredentialRefreshData
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ManagedCredential{}, &ManagedTokenRefreshError{Reason: classifyManagedRefreshFailure(body)}
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return ManagedCredential{}, ErrManagedCredentialRefreshData
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ManagedCredential{}, ErrManagedCredentialRefreshData
	}
	accessToken, ok := payload["access_token"].(string)
	if !ok || strings.TrimSpace(accessToken) == "" {
		return ManagedCredential{}, ErrManagedCredentialRefreshData
	}
	nextRefresh := refreshToken
	if raw, exists := payload["refresh_token"]; exists && raw != nil {
		value, ok := raw.(string)
		if !ok {
			return ManagedCredential{}, ErrManagedCredentialRefreshData
		}
		nextRefresh = value
	}
	return ManagedCredential{
		AccessToken:  accessToken,
		RefreshToken: nextRefresh,
		ExpiresAtMS:  safeManagedExpiryMillis(now, parseManagedExpiresIn(payload["expires_in"])),
	}, nil
}

func (s *MainCredentialSource) publishRefreshedTokens(snapshot []byte, refreshed ManagedCredential) error {
	current, err := os.ReadFile(s.authPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, snapshot) {
		return ErrMainAuthChangedDuringRefresh
	}
	var root map[string]any
	if err := json.Unmarshal(current, &root); err != nil || root == nil {
		return ErrManagedCredentialRefreshData
	}
	tokens, _ := root["tokens"].(map[string]any)
	if tokens == nil {
		tokens = map[string]any{}
	}
	tokens["access_token"] = refreshed.AccessToken
	if strings.TrimSpace(refreshed.RefreshToken) != "" {
		tokens["refresh_token"] = refreshed.RefreshToken
	}
	root["tokens"] = tokens
	payload, err := json.Marshal(root)
	if err != nil {
		return err
	}
	return atomicfile.Write(s.authPath, payload, atomicfile.Options{Mode: 0o600})
}

func (s *MainCredentialSource) refreshLockPath() string {
	return filepath.Join(filepath.Dir(s.authPath), "auth.json.benes-refresh.lock")
}

var _ MainCredentialMaterializer = (*MainCredentialSource)(nil)
