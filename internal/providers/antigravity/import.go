package antigravity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const userinfoEndpoint = "https://www.googleapis.com/oauth2/v2/userinfo"

type ImportCredential struct {
	AccountID string
	Token     string
	Refresh   string
	ProjectID string
	Email     string
	Expires   int64
}

func ValidateImportCredential(ctx context.Context, refreshToken string) (ImportCredential, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client := HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	access, expires, err := refreshAccessTokenWithExpiry(ctx, client, refreshToken)
	if err != nil {
		return ImportCredential{}, err
	}
	email, accountID, err := fetchUserInfo(ctx, client, access)
	if err != nil {
		return ImportCredential{}, err
	}
	project, err := DiscoverProject(ctx, client, DailyAPI, access)
	if err != nil {
		project = ""
	}
	return ImportCredential{
		AccountID: accountID,
		Token:     access,
		Refresh:   refreshToken,
		ProjectID: project,
		Email:     email,
		Expires:   expires,
	}, nil
}

func refreshAccessTokenWithExpiry(ctx context.Context, client *http.Client, refreshToken string) (string, int64, error) {
	access, err := RefreshAccessToken(ctx, client, refreshToken)
	if err != nil {
		return "", 0, err
	}
	return access, time.Now().Add(50 * time.Minute).UnixMilli(), nil
}

func fetchUserInfo(ctx context.Context, client *http.Client, access string) (email, id string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userinfoEndpoint, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+access)
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
		return "", "", fmt.Errorf("Antigravity identity request failed: %d", resp.StatusCode)
	}
	var body struct {
		Email string `json:"email"`
		ID    string `json:"id"`
	}
	if json.Unmarshal(raw, &body) != nil || strings.TrimSpace(body.Email) == "" || strings.TrimSpace(body.ID) == "" {
		return "", "", fmt.Errorf("Antigravity identity response did not include email and id")
	}
	return strings.ToLower(strings.TrimSpace(body.Email)), strings.TrimSpace(body.ID), nil
}
