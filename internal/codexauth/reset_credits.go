package codexauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	canonicalResetCreditsURL        = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	canonicalResetCreditsConsumeURL = canonicalResetCreditsURL + "/consume"
	resetCreditsTimeout             = 8 * time.Second
	resetCreditsMaxBody             = 65_536
)

var ResetCreditHTTP HTTPDoer = &http.Client{
	Timeout: resetCreditsTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type ResetCreditsDTO struct {
	Credits        []map[string]string `json:"credits"`
	AvailableCount *float64            `json:"available_count,omitempty"`
}

type ResetCreditConsumeDTO struct {
	Code string `json:"code"`
}

var redeemRequestIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func ValidRedeemRequestID(id string) bool {
	return redeemRequestIDPattern.MatchString(id)
}

func NewRedeemRequestID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:]), nil
}

func FetchResetCredits(ctx context.Context, token ManagedToken) (ResetCreditsDTO, int, error) {
	status, body, err := resetCreditRequest(ctx, http.MethodGet, canonicalResetCreditsURL, token, nil)
	if err != nil {
		return ResetCreditsDTO{}, status, err
	}
	if status != http.StatusOK {
		return ResetCreditsDTO{}, status, fmt.Errorf("upstream error %d", status)
	}
	return decodeResetCredits(body), status, nil
}

func ConsumeResetCredit(ctx context.Context, token ManagedToken, redeemRequestID string) (ResetCreditConsumeDTO, int, error) {
	if !ValidRedeemRequestID(redeemRequestID) {
		return ResetCreditConsumeDTO{}, 0, fmt.Errorf("invalid redeemRequestId")
	}
	payload, _ := json.Marshal(map[string]string{"redeem_request_id": redeemRequestID})
	status, body, err := resetCreditRequest(ctx, http.MethodPost, canonicalResetCreditsConsumeURL, token, payload)
	if err != nil {
		return ResetCreditConsumeDTO{}, status, err
	}
	if status != http.StatusOK {
		return ResetCreditConsumeDTO{}, status, fmt.Errorf("upstream error %d", status)
	}
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)
	code, _ := parsed["code"].(string)
	if code == "" {
		code = "unknown"
	}
	return ResetCreditConsumeDTO{Code: code}, status, nil
}

func resetCreditRequest(ctx context.Context, method, rawURL string, token ManagedToken, body []byte) (int, []byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "chatgpt.com") {
		return 0, nil, fmt.Errorf("refusing reset-credit request to non-canonical host")
	}
	access := strings.TrimSpace(token.AccessToken)
	accountID := strings.TrimSpace(token.ChatGPTAccountID)
	if access == "" || accountID == "" {
		return 0, nil, fmt.Errorf("reset-credit credential is incomplete")
	}
	requestCtx, cancel := context.WithTimeout(ctx, resetCreditsTimeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(requestCtx, method, parsed.String(), reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("ChatGPT-Account-Id", accountID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := ResetCreditHTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, resetCreditsMaxBody+1))
	if len(raw) > resetCreditsMaxBody {
		return resp.StatusCode, nil, fmt.Errorf("reset-credit response too large")
	}
	return resp.StatusCode, raw, nil
}

func decodeResetCredits(raw []byte) ResetCreditsDTO {
	var obj map[string]any
	_ = json.Unmarshal(raw, &obj)
	out := ResetCreditsDTO{Credits: []map[string]string{}}
	credits, _ := obj["credits"].([]any)
	for _, item := range credits {
		row, _ := item.(map[string]any)
		granted, _ := row["granted_at"].(string)
		expires, _ := row["expires_at"].(string)
		if granted != "" && expires != "" {
			out.Credits = append(out.Credits, map[string]string{"granted_at": granted, "expires_at": expires})
		}
	}
	available := obj["available_count"]
	if nested, ok := obj["rate_limit_reset_credits"].(map[string]any); ok {
		if available == nil {
			available = nested["available_count"]
		}
	}
	if n, ok := available.(float64); ok {
		out.AvailableCount = &n
	}
	return out
}
