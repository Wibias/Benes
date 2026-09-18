package openairesponses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Wibias/Benes/internal/providers"
	benesreasoning "github.com/Wibias/Benes/internal/responses/reasoning"
)

const compactResponseMaxBytes = 32 << 20

var ErrCompactResponseTooLarge = errors.New("compact response exceeded 32 MiB")

func (c *Client) SupportsNativeCompact() bool {
	return officialOpenAIResponsesEndpoint(c.endpoint)
}

func (c *ForwardClient) SupportsNativeCompact() bool {
	return c != nil && c.nativeForward
}

func (c *ForwardClient) NativeCodexForward() bool {
	return c != nil && c.nativeForward
}

func officialOpenAIResponsesEndpoint(endpoint string) bool {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if !strings.EqualFold(parsed.Hostname(), "api.openai.com") {
		return false
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return false
	}
	return strings.TrimRight(parsed.EscapedPath(), "/") == "/v1/responses"
}

func compactURL(responsesEndpoint string) string {
	return strings.TrimRight(strings.TrimSpace(responsesEndpoint), "/") + "/compact"
}

func searchURL(responsesEndpoint string) string {
	return strings.TrimRight(strings.TrimSpace(responsesEndpoint), "/") + "/alpha/search"
}

func (c *Client) Compact(ctx context.Context, dispatch providers.DispatchRequest, body []byte) (int, http.Header, []byte, error) {
	prepared, err := prepareNativeCompactBody(body)
	if err != nil {
		return 0, nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, compactURL(c.endpoint), bytes.NewReader(prepared))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("build OpenAI compact request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	_, apiKey := c.selectAPIKey()
	req.Header.Set("Authorization", "Bearer "+apiKey)
	response, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		return 0, nil, nil, fmt.Errorf("OpenAI compact request failed: %w", err)
	}
	return bufferCompactHTTP(ctx, response)
}

func (c *ForwardClient) Compact(ctx context.Context, dispatch providers.DispatchRequest, body []byte) (int, http.Header, []byte, error) {
	attempt, err := resolveForwardAttempt(ctx, c.credentialAuthority, dispatch)
	if err != nil {
		return 0, nil, nil, err
	}
	return c.compactAttempt(ctx, dispatch, attempt, body, true)
}

func (c *ForwardClient) compactAttempt(
	ctx context.Context,
	dispatch providers.DispatchRequest,
	attempt ForwardAttempt,
	body []byte,
	allowAuthRetry bool,
) (int, http.Header, []byte, error) {
	credential := attempt.Credential
	if strings.TrimSpace(credential.Authorization) == "" {
		abandonForwardObserver(attempt.Observer)
		return 0, nil, nil, ErrForwardAuthorizationRequired
	}
	prepared, err := prepareNativeCompactBody(body)
	if err != nil {
		abandonForwardObserver(attempt.Observer)
		return 0, nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, compactURL(c.endpoint), bytes.NewReader(prepared))
	if err != nil {
		abandonForwardObserver(attempt.Observer)
		return 0, nil, nil, fmt.Errorf("build OpenAI compact forward request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for name, value := range dispatch.ForwardHeaders.Values() {
		switch strings.ToLower(name) {
		case "authorization", "chatgpt-account-id":
			continue
		default:
			req.Header.Set(name, value)
		}
	}
	req.Header.Set("Authorization", credential.Authorization)
	if credential.ChatGPTAccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", credential.ChatGPTAccountID)
	}
	response, err := c.httpClient.Do(req)
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
		return 0, nil, nil, fmt.Errorf("OpenAI compact forward request failed: %w", err)
	}
	status, header, payload, bufferErr := bufferCompactHTTP(ctx, response)
	if allowAuthRetry && bufferErr == nil && status == http.StatusUnauthorized && attempt.RetryAuth != nil {
		if attempt.Observer != nil {
			attempt.Observer.Observe(ForwardOutcome{Kind: ForwardOutcomeHTTP, StatusCode: status, RetryAfter: header.Get("Retry-After"), ResetAt: forwardCodexResetAt(header)})
		}
		retryAttempt, retry, retryErr := attempt.RetryAuth(ctx)
		if retryErr != nil {
			return 0, nil, nil, retryErr
		}
		if retry {
			return c.compactAttempt(ctx, dispatch, retryAttempt, body, false)
		}
	}
	if attempt.Observer != nil {
		if bufferErr != nil && ctx.Err() == nil {
			attempt.Observer.Observe(ForwardOutcome{Kind: ForwardOutcomeTransportError})
		} else {
			attempt.Observer.Observe(ForwardOutcome{Kind: ForwardOutcomeHTTP, StatusCode: status, RetryAfter: header.Get("Retry-After"), ResetAt: forwardCodexResetAt(header)})
		}
	}
	return status, header, payload, bufferErr
}

func (c *ForwardClient) Search(ctx context.Context, dispatch providers.DispatchRequest, body []byte) (int, http.Header, []byte, error) {
	if c == nil || !c.nativeForward {
		return 0, nil, nil, fmt.Errorf("provider does not support native Codex search")
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
	prepared, err := prepareNativeSearchBody(body)
	if err != nil {
		abandonForwardObserver(attempt.Observer)
		return 0, nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchURL(c.endpoint), bytes.NewReader(prepared))
	if err != nil {
		abandonForwardObserver(attempt.Observer)
		return 0, nil, nil, fmt.Errorf("build OpenAI search forward request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for name, value := range dispatch.ForwardHeaders.Values() {
		switch strings.ToLower(name) {
		case "authorization", "chatgpt-account-id":
			continue
		default:
			req.Header.Set(name, value)
		}
	}
	req.Header.Set("Authorization", credential.Authorization)
	if credential.ChatGPTAccountID != "" {
		req.Header.Set("ChatGPT-Account-Id", credential.ChatGPTAccountID)
	}
	response, err := c.httpClient.Do(req)
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
		return 0, nil, nil, fmt.Errorf("OpenAI search forward request failed: %w", err)
	}
	status, header, payload, bufferErr := bufferCompactHTTP(ctx, response)
	if attempt.Observer != nil {
		if bufferErr != nil && ctx.Err() == nil {
			attempt.Observer.Observe(ForwardOutcome{Kind: ForwardOutcomeTransportError})
		} else {
			attempt.Observer.Observe(ForwardOutcome{Kind: ForwardOutcomeHTTP, StatusCode: status, RetryAfter: header.Get("Retry-After"), ResetAt: forwardCodexResetAt(header)})
		}
	}
	return status, header, payload, bufferErr
}

func prepareNativeSearchBody(body []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("search request body must be a JSON object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, fmt.Errorf("search request body must be a JSON object")
	}
	return json.Marshal(fields)
}

func prepareNativeCompactBody(body []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("compact request body is empty")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, fmt.Errorf("compact request body must be a JSON object")
	}
	delete(fields, "reasoning")
	if input, ok := fields["input"]; ok {
		sanitized, changed := sanitizeReasoningInput(input)
		if changed {
			encoded, err := json.Marshal(sanitized)
			if err != nil {
				return nil, err
			}
			fields["input"] = encoded
		}
	}
	return json.Marshal(fields)
}

func sanitizeReasoningInput(raw json.RawMessage) ([]any, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, false
	}
	var items []json.RawMessage
	if json.Unmarshal(trimmed, &items) != nil {
		return nil, false
	}
	out := make([]any, 0, len(items))
	changed := false
	for _, itemRaw := range items {
		var rec map[string]any
		if json.Unmarshal(itemRaw, &rec) != nil {
			out = append(out, json.RawMessage(append([]byte(nil), itemRaw...)))
			continue
		}
		typeName, _ := rec["type"].(string)
		if typeName != "reasoning" {
			out = append(out, rec)
			continue
		}
		encrypted, _ := rec["encrypted_content"].(string)
		hasBenesEnvelope := strings.HasPrefix(encrypted, benesreasoning.Prefix)
		content, _ := rec["content"].([]any)
		hasRawContent := len(content) > 0
		if !hasRawContent && !hasBenesEnvelope {
			out = append(out, rec)
			continue
		}
		changed = true
		if hasBenesEnvelope {
			delete(rec, "encrypted_content")
		}
		rec["content"] = []any{}
		out = append(out, rec)
	}
	return out, changed
}

func bufferCompactHTTP(ctx context.Context, response *http.Response) (int, http.Header, []byte, error) {
	if response == nil {
		return 0, nil, nil, fmt.Errorf("compact upstream returned no response")
	}
	defer response.Body.Close()
	header := response.Header.Clone()
	if declared := response.ContentLength; declared > compactResponseMaxBytes {
		_, _ = io.Copy(io.Discard, response.Body)
		return 0, nil, nil, ErrCompactResponseTooLarge
	}
	limited := io.LimitReader(response.Body, compactResponseMaxBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		return 0, nil, nil, fmt.Errorf("failed to read compact response: %w", err)
	}
	if int64(len(payload)) > compactResponseMaxBytes {
		return 0, nil, nil, ErrCompactResponseTooLarge
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, nil, err
	}
	return response.StatusCode, header, payload, nil
}

func appendCompactionTrigger(body []byte) ([]byte, error) {
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	input, _ := fields["input"].([]any)
	input = append(input, map[string]any{"type": "compaction_trigger"})
	fields["input"] = input
	return json.Marshal(fields)
}
