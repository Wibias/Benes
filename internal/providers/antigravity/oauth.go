package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	tokenEndpoint     = "https://oauth2.googleapis.com/token"
	onboardAttempts   = 5
	onboardPoll       = 2 * time.Second
	oauthClientID     = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	oauthClientSecret = "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"
)

var HTTPClient *http.Client

func ExtractProjectID(body []byte) string {
	var root map[string]any
	if json.Unmarshal(body, &root) != nil {
		return ""
	}
	if id := projectFromMap(root); id != "" {
		return id
	}
	if nested, ok := root["response"].(map[string]any); ok {
		return projectFromMap(nested)
	}
	return ""
}

func DiscoverProject(ctx context.Context, client *http.Client, endpoint, token string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("Cloud Code Assist HTTP client is required")
	}
	if _, err := ResolveDestination(endpoint); err != nil {
		return "", err
	}
	if id, err := postProjectRPC(ctx, client, ProdAPI, token, "/v1internal:loadCodeAssist", map[string]any{
		"metadata": map[string]any{"ideType": "ANTIGRAVITY"},
	}); err == nil && id != "" {
		return id, nil
	}
	for attempt := 0; attempt < onboardAttempts; attempt++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		id, status, err := postProjectRPCStatus(ctx, client, DailyAPI, token, "/v1internal:onboardUser", map[string]any{
			"tier_id": "free-tier",
			"metadata": map[string]any{
				"ide_type":    "ANTIGRAVITY",
				"ide_name":    "antigravity",
				"ide_version": RequestUserAgent,
			},
		})
		if err == nil && id != "" {
			return id, nil
		}
		if status != 0 && status != http.StatusTooManyRequests && status < 500 {
			break
		}
		timer := time.NewTimer(onboardPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", fmt.Errorf("Cloud Code Assist project discovery failed")
}

func RefreshAccessToken(ctx context.Context, client *http.Client, refreshToken string) (string, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return "", fmt.Errorf("Cloud Code Assist refresh token is required")
	}
	if client == nil {
		return "", fmt.Errorf("Cloud Code Assist HTTP client is required")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {oauthClientID},
		"client_secret": {oauthClientSecret},
		"refresh_token": {refreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Cloud Code Assist token refresh returned HTTP %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(raw, &payload) != nil || strings.TrimSpace(payload.AccessToken) == "" {
		return "", fmt.Errorf("Cloud Code Assist token refresh did not include an access token")
	}
	return payload.AccessToken, nil
}

func postProjectRPC(ctx context.Context, client *http.Client, dest, token, path string, body map[string]any) (string, error) {
	id, _, err := postProjectRPCStatus(ctx, client, dest, token, path, body)
	return id, err
}

func postProjectRPCStatus(ctx context.Context, client *http.Client, dest, token, path string, body map[string]any) (string, int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest+path, bytes.NewReader(raw))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", RequestUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, DefaultQuotaMaxBytes))
	if err != nil {
		return "", resp.StatusCode, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.StatusCode, fmt.Errorf("Cloud Code Assist project RPC returned HTTP %d", resp.StatusCode)
	}
	if id := ExtractProjectID(payload); id != "" {
		return id, resp.StatusCode, nil
	}
	var root map[string]any
	if json.Unmarshal(payload, &root) == nil {
		if done, _ := root["done"].(bool); done {
			if nested, ok := root["response"].(map[string]any); ok {
				if id := projectFromMap(nested); id != "" {
					return id, resp.StatusCode, nil
				}
			}
		}
	}
	return "", resp.StatusCode, fmt.Errorf("Cloud Code Assist project RPC omitted project id")
}

func projectFromMap(root map[string]any) string {
	for _, key := range []string{"cloudaicompanionProject", "projectId", "project"} {
		switch value := root[key].(type) {
		case string:
			if id := strings.TrimSpace(value); id != "" {
				return id
			}
		case map[string]any:
			if id, _ := value["id"].(string); strings.TrimSpace(id) != "" {
				return strings.TrimSpace(id)
			}
		}
	}
	return ""
}
