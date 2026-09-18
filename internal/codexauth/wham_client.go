package codexauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/transport"
)

const (
	canonicalWHAMUsageEndpoint  = "https://chatgpt.com/backend-api/wham/usage"
	defaultWHAMMaxBodyBytes     = 65_536
	defaultWHAMTimeout          = 8 * time.Second
	mainWHAMAuthEvidenceTimeout = time.Second
)

var terminalMainWHAMAuthCodes = map[string]struct{}{
	"invalid_refresh_token": {},
}

type WHAMClientConfig struct {
	HTTPClient        *http.Client
	Timeout           time.Duration
	MaxBodyBytes      int64
	DestinationPolicy transport.DestinationPolicy
	TransportOptions  transport.ClientOptions
}

type WHAMClient struct {
	httpClient   *http.Client
	timeout      time.Duration
	maxBodyBytes int64
}

type WHAMFetchResult struct {
	StatusCode       int
	Quota            WHAMQuotaResult
	TerminalMainAuth bool
}

func NewWHAMClient(config WHAMClientConfig) (*WHAMClient, error) {
	if config.HTTPClient == nil {
		return nil, fmt.Errorf("hardened HTTP client is required")
	}
	clientCopy := *config.HTTPClient
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if config.Timeout <= 0 {
		config.Timeout = defaultWHAMTimeout
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = defaultWHAMMaxBodyBytes
	}
	return &WHAMClient{httpClient: &clientCopy, timeout: config.Timeout, maxBodyBytes: config.MaxBodyBytes}, nil
}

func NewWHAMClientHardened(ctx context.Context, config WHAMClientConfig) (*WHAMClient, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	target, err := transport.ResolveTarget(ctx, canonicalWHAMUsageEndpoint, config.DestinationPolicy)
	if err != nil {
		return nil, fmt.Errorf("validate Codex WHAM destination: %w", err)
	}
	config.HTTPClient = transport.NewClient(target, config.TransportOptions)
	return NewWHAMClient(config)
}

func (c *WHAMClient) Fetch(ctx context.Context, token ManagedToken, configuredPlan string) (WHAMFetchResult, error) {
	return c.fetch(ctx, token, configuredPlan, false)
}

func (c *WHAMClient) FetchMain(ctx context.Context, token ManagedToken, configuredPlan string) (WHAMFetchResult, error) {
	return c.fetch(ctx, token, configuredPlan, true)
}

func (c *WHAMClient) fetch(ctx context.Context, token ManagedToken, configuredPlan string, inspectMainAuth bool) (WHAMFetchResult, error) {
	if ctx == nil {
		return WHAMFetchResult{}, fmt.Errorf("context is required")
	}
	if err := ctx.Err(); err != nil {
		return WHAMFetchResult{}, err
	}
	accessToken := strings.TrimSpace(token.AccessToken)
	accountID := strings.TrimSpace(token.ChatGPTAccountID)
	if accessToken == "" || accountID == "" {
		return WHAMFetchResult{}, fmt.Errorf("managed Codex WHAM credential is incomplete")
	}

	requestCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, canonicalWHAMUsageEndpoint, nil)
	if err != nil {
		return WHAMFetchResult{}, fmt.Errorf("build Codex WHAM request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("ChatGPT-Account-Id", accountID)

	response, err := c.httpClient.Do(request)
	if err != nil {
		if requestCtx.Err() != nil {
			return WHAMFetchResult{}, requestCtx.Err()
		}
		return WHAMFetchResult{}, fmt.Errorf("Codex WHAM request failed: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		terminal := false
		if inspectMainAuth {
			switch response.StatusCode {
			case http.StatusUnauthorized:
				if !jwtVerifiablyLive(accessToken, time.Now()) {
					terminal = true
					_ = response.Body.Close()
				} else {
					terminal = c.readTerminalMainAuthEvidence(requestCtx, response.Body)
				}
			case http.StatusForbidden:
				terminal = c.readTerminalMainAuthEvidence(requestCtx, response.Body)
			default:
				_ = response.Body.Close()
			}
		} else {
			_ = response.Body.Close()
		}
		return WHAMFetchResult{StatusCode: response.StatusCode, TerminalMainAuth: terminal}, nil
	}

	var body bytes.Buffer
	_, err = transport.CopyBounded(requestCtx, &body, response.Body, transport.StreamLimits{MaxBytes: c.maxBodyBytes})
	if err != nil {
		return WHAMFetchResult{}, fmt.Errorf("read Codex WHAM response: %w", err)
	}
	quota, err := ParseWHAMUsage(body.Bytes(), configuredPlan)
	if err != nil {
		return WHAMFetchResult{}, err
	}
	return WHAMFetchResult{StatusCode: response.StatusCode, Quota: quota}, nil
}

func (c *WHAMClient) readTerminalMainAuthEvidence(parent context.Context, body io.ReadCloser) bool {
	ctx, cancel := context.WithTimeout(parent, mainWHAMAuthEvidenceTimeout)
	defer cancel()
	maxBytes := c.maxBodyBytes
	if maxBytes <= 0 || maxBytes > defaultWHAMMaxBodyBytes {
		maxBytes = defaultWHAMMaxBodyBytes
	}
	var buffer bytes.Buffer
	if _, err := transport.CopyBounded(ctx, &buffer, body, transport.StreamLimits{
		MaxBytes:          maxBytes,
		InactivityTimeout: mainWHAMAuthEvidenceTimeout,
	}); err != nil {
		return false
	}
	if !utf8.Valid(buffer.Bytes()) {
		return false
	}
	return terminalMainAuthJSON(buffer.Bytes())
}

func terminalMainAuthJSON(body []byte) bool {
	var root map[string]any
	if json.Unmarshal(body, &root) != nil || root == nil {
		return false
	}
	var code any
	if value, ok := root["detail"]; ok && jsonObjectLike(value) {
		code = jsonObjectCode(value)
	} else if value, ok := root["error"]; ok && jsonObjectLike(value) {
		code = jsonObjectCode(value)
	} else {
		code = root["code"]
	}
	text, ok := code.(string)
	if !ok {
		return false
	}
	_, ok = terminalMainWHAMAuthCodes[text]
	return ok
}

func jsonObjectLike(value any) bool {
	if value == nil {
		return false
	}
	switch value.(type) {
	case map[string]any, []any:
		return true
	default:
		return false
	}
}

func jsonObjectCode(value any) any {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return object["code"]
}
