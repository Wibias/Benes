package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/credentialpool"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/transport"
)

const (
	kiroPoolDestination  = "kiro"
	kiroPoolAuthClass    = "oauth"
	maxKiroRetryCooldown = time.Hour
)

func applyKiroAuth(req *http.Request, account AccountSnapshot) {
	if req == nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+account.AccessToken)
	if profile := strings.TrimSpace(account.ProfileARN); profile != "" {
		req.Header.Set("x-amzn-kiro-profile-arn", profile)
		return
	}
	req.Header.Del("x-amzn-kiro-profile-arn")
}

func marshalGenerateBody(payload map[string]any, account AccountSnapshot) ([]byte, error) {
	if profile := strings.TrimSpace(account.ProfileARN); profile != "" {
		payload["profileArn"] = profile
	} else {
		delete(payload, "profileArn")
	}
	return json.Marshal(payload)
}

func (c *Client) buildCredentialPool() *credentialpool.Pool {
	if c == nil {
		return nil
	}
	region := c.account.EffectiveRegion()
	now := c.now
	if now.IsZero() {
		now = time.Now()
	}
	candidates := make([]credentialpool.Candidate, 0, len(c.accounts))
	seen := map[string]struct{}{}
	for _, account := range c.accounts {
		ref := account.Ref()
		if ref == "" || account.EffectiveRegion() != region {
			continue
		}
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		candidates = append(candidates, credentialpool.Candidate{
			Ref:         ref,
			Destination: kiroPoolDestination,
			AuthClass:   kiroPoolAuthClass,
			Evidence:    c.evidenceFor(ref, now),
		})
	}
	if len(candidates) < 2 {
		return nil
	}
	return credentialpool.New(candidates)
}

func (c *Client) credentialPool() *credentialpool.Pool {
	if c == nil {
		return nil
	}
	return c.pool
}

func (c *Client) evidenceFor(ref string, now time.Time) credentialpool.Evidence {
	if c != nil {
		if evidence, ok := c.evidence[ref]; ok {
			return evidence
		}
	}
	return credentialpool.Evidence{
		Auth:       credentialpool.AuthUsable,
		Quota:      credentialpool.QuotaUnknown,
		Limit:      credentialpool.LimitAvailable,
		ObservedAt: now,
	}
}

func (c *Client) poolRequest(now time.Time) credentialpool.Request {
	return credentialpool.Request{
		Destination: kiroPoolDestination,
		AuthClass:   kiroPoolAuthClass,
		Portable:    true,
		Now:         now,
	}
}

func (c *Client) failoverAccount(pool *credentialpool.Pool, account AccountSnapshot) (AccountSnapshot, bool) {
	if c == nil || pool == nil {
		return AccountSnapshot{}, false
	}
	now := c.now
	if now.IsZero() {
		now = time.Now()
	}
	retryAt := now.Add(maxKiroRetryCooldown)
	if evidence, ok := c.evidence[account.Ref()]; ok {
		retryAt = clampRetryAt(now, evidence.ResetAt)
	}
	decision, err := pool.NextAfterFailure(c.poolRequest(now), account.Ref(), credentialpool.Failure{
		Class:   credentialpool.FailureRateLimited,
		RetryAt: retryAt,
	})
	if err != nil {
		return AccountSnapshot{}, false
	}
	return c.accountByRef(decision.Candidate.Ref)
}

func (c *Client) accountByRef(ref string) (AccountSnapshot, bool) {
	if c == nil || strings.TrimSpace(ref) == "" {
		return AccountSnapshot{}, false
	}
	for _, account := range c.accounts {
		if account.Ref() == ref {
			return account, true
		}
	}
	if c.account.Ref() == ref {
		return c.account, true
	}
	return AccountSnapshot{}, false
}

func clampRetryAt(now, resetAt time.Time) time.Time {
	limit := now.Add(maxKiroRetryCooldown)
	if resetAt.IsZero() || resetAt.After(limit) {
		return limit
	}
	if resetAt.Before(now) {
		return now
	}
	return resetAt
}

func (c *Client) postGenerate(ctx context.Context, req *http.Request, payload map[string]any, body []byte, turn *resourcebudget.Turn) (*http.Response, error) {
	account := c.account
	pool := c.credentialPool()
	attempt := 1
	for {
		attemptBody := body
		if account.Ref() != c.account.Ref() {
			nextBody, err := marshalGenerateBody(payload, account)
			if err != nil {
				return nil, err
			}
			attemptBody = nextBody
		}
		attemptReq := req.Clone(ctx)
		attemptReq.Body = io.NopCloser(bytes.NewReader(attemptBody))
		attemptReq.ContentLength = int64(len(attemptBody))
		attemptReq.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(attemptBody)), nil
		}
		applyKiroAuth(attemptReq, account)
		resp, err := transport.DoPhysicalSend(ctx, c.httpClient, attemptReq, turn, "kiro")
		if err != nil {
			if errors.Is(err, resourcebudget.ErrPhysicalSendBudgetExceeded) {
				return nil, err
			}
			if attempt >= maxTransientAttempts {
				return nil, err
			}
			attempt++
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		status := resp.StatusCode
		_ = resp.Body.Close()
		if terminalAuthStatus(status) || !retryableStatus(status) {
			return nil, fmt.Errorf("Kiro GenerateAssistantResponse returned HTTP %d", status)
		}
		if status == http.StatusTooManyRequests {
			if next, ok := c.failoverAccount(pool, account); ok {
				account = next
				attempt = 1
				continue
			}
		}
		if attempt >= maxTransientAttempts {
			return nil, fmt.Errorf("Kiro GenerateAssistantResponse returned HTTP %d", status)
		}
		attempt++
	}
}
