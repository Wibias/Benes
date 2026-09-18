package openairesponses

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/providers"
)

const imageResponseMaxBytes = 64 << 20

var ErrImageResponseTooLarge = errors.New("image response exceeded 64 MiB")

func (c *Client) SupportsImageRelay() bool {
	if c == nil {
		return false
	}
	return providers.ImageCapable(c.endpoint, c.capability.AuthClass)
}

func (c *Client) RelayImage(ctx context.Context, dispatch providers.DispatchRequest, req providers.ImageRelayRequest) (int, http.Header, []byte, error) {
	if c == nil {
		return 0, nil, nil, fmt.Errorf("openai image client is not configured")
	}
	endpoint, err := providers.ImageURL(c.endpoint, req.Kind)
	if err != nil {
		return 0, nil, nil, err
	}
	httpReq, err := newImageRelayRequest(ctx, endpoint, req)
	if err != nil {
		return 0, nil, nil, err
	}
	_, apiKey := c.selectAPIKey()
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := c.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		return 0, nil, nil, fmt.Errorf("OpenAI image request failed: %w", err)
	}
	return bufferImageHTTP(ctx, response)
}

func (c *ForwardClient) SupportsImageRelay() bool {
	return c != nil && c.nativeForward
}

func (c *ForwardClient) RelayImage(ctx context.Context, dispatch providers.DispatchRequest, req providers.ImageRelayRequest) (int, http.Header, []byte, error) {
	if c == nil || !c.nativeForward {
		return 0, nil, nil, fmt.Errorf("provider does not support image relay")
	}
	attempt, err := resolveForwardAttempt(ctx, c.credentialAuthority, dispatch)
	if err != nil {
		return 0, nil, nil, err
	}
	credential := attempt.Credential
	if strings.TrimSpace(credential.Authorization) == "" {
		abandonForwardObserver(attempt.Observer)
		return 0, nil, nil, ErrForwardAuthorizationRequired
	}
	endpoint, err := providers.ImageURL(c.endpoint, req.Kind)
	if err != nil {
		abandonForwardObserver(attempt.Observer)
		return 0, nil, nil, err
	}
	httpReq, err := newImageRelayRequest(ctx, endpoint, req)
	if err != nil {
		abandonForwardObserver(attempt.Observer)
		return 0, nil, nil, err
	}
	for name, value := range dispatch.ForwardHeaders.Values() {
		switch strings.ToLower(name) {
		case "authorization", "chatgpt-account-id":
			continue
		default:
			httpReq.Header.Set(name, value)
		}
	}
	httpReq.Header.Set("Authorization", credential.Authorization)
	if credential.ChatGPTAccountID != "" {
		httpReq.Header.Set("ChatGPT-Account-Id", credential.ChatGPTAccountID)
	}
	response, err := c.httpClient.Do(httpReq)
	if err != nil {
		if attempt.Observer != nil {
			if ctx.Err() != nil {
				abandonForwardObserver(attempt.Observer)
			} else {
				attempt.Observer.Observe(ForwardOutcome{Kind: ForwardOutcomeTransportError, TimedOut: errors.Is(err, context.DeadlineExceeded)})
			}
		}
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		return 0, nil, nil, fmt.Errorf("OpenAI image forward request failed: %w", err)
	}
	status, header, payload, bufferErr := bufferImageHTTP(ctx, response)
	if attempt.Observer != nil {
		if bufferErr != nil && ctx.Err() == nil {
			attempt.Observer.Observe(ForwardOutcome{Kind: ForwardOutcomeTransportError})
		} else {
			attempt.Observer.Observe(ForwardOutcome{Kind: ForwardOutcomeHTTP, StatusCode: status, RetryAfter: header.Get("Retry-After"), ResetAt: forwardCodexResetAt(header)})
		}
	}
	return status, header, payload, bufferErr
}

func newImageRelayRequest(ctx context.Context, endpoint string, req providers.ImageRelayRequest) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(req.Body))
	if err != nil {
		return nil, fmt.Errorf("build OpenAI image request: %w", err)
	}
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = "application/json"
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(req.Body)), nil
	}
	httpReq.ContentLength = int64(len(req.Body))
	return httpReq, nil
}

func bufferImageHTTP(ctx context.Context, response *http.Response) (int, http.Header, []byte, error) {
	if response == nil {
		return 0, nil, nil, fmt.Errorf("image upstream returned no response")
	}
	defer response.Body.Close()
	header := response.Header.Clone()
	if declared := response.ContentLength; declared > imageResponseMaxBytes {
		_, _ = io.Copy(io.Discard, response.Body)
		return 0, nil, nil, ErrImageResponseTooLarge
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, imageResponseMaxBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		return 0, nil, nil, fmt.Errorf("failed to read image response: %w", err)
	}
	if int64(len(payload)) > imageResponseMaxBytes {
		return 0, nil, nil, ErrImageResponseTooLarge
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, nil, err
	}
	return response.StatusCode, header, payload, nil
}
