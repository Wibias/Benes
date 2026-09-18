package combo

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/providers/google"
	"github.com/Wibias/Benes/internal/timeline"
)

const (
	DecisionHop       = "hop"
	DecisionStop      = "stop"
	DecisionCommitted = "committed"

	CodeIncompletePrecommitFailover = "incomplete_precommit_failover"
)

type Member struct {
	ID       string
	Protocol string
}

type Result struct {
	Status  int
	Message string
	Code    string
}

type AttemptFunc func(ctx context.Context, member Member, attempts int) (Result, error)

func AttemptsFor(protocol string) int {
	if google.AppliesTransientRetry(protocol) {
		return 3
	}
	return 1
}

func FailureDecision(status int, message, code string) string {
	if status == 499 {
		return DecisionStop
	}
	lower := strings.ToLower(message)
	if strings.Contains(lower, "origin_rejected") {
		return DecisionStop
	}
	normalized := strings.TrimSpace(code)
	if normalized == "" {
		normalized = classifyCode(status, message)
	}
	if normalized == "input_admission_refused" || normalized == CodeIncompletePrecommitFailover {
		return DecisionHop
	}
	switch normalized {
	case "origin_rejected", "context_length_exceeded", "invalid_request_error":
		return DecisionStop
	}
	if status == 401 || status == 403 || status == 404 || status == 408 || status == 410 || status == 429 || status >= 500 {
		return DecisionHop
	}
	switch normalized {
	case "permission_denied", "subscription_required", "invalid_api_key", "insufficient_quota",
		"rate_limit_exceeded", "server_is_overloaded", "upstream_server_error":
		return DecisionHop
	}
	return DecisionStop
}

func classifyCode(status int, message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "invalid_request_error"):
		return "invalid_request_error"
	case strings.Contains(lower, "context_length_exceeded"):
		return "context_length_exceeded"
	case strings.Contains(lower, "origin_rejected"):
		return "origin_rejected"
	case strings.Contains(lower, "input_admission_refused"):
		return "input_admission_refused"
	case status == 429:
		return "rate_limit_exceeded"
	case status >= 500:
		return "upstream_server_error"
	default:
		return ""
	}
}

func Walk(ctx context.Context, members []Member, attempt AttemptFunc) error {
	if attempt == nil {
		return fmt.Errorf("combo attempt function is required")
	}
	if len(members) == 0 {
		return fmt.Errorf("combo requires at least one member")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var last error
	for i, member := range members {
		if err := ctx.Err(); err != nil {
			return err
		}
		if tr := timeline.FromContext(ctx); tr != nil {
			tr.SetAttempt(i + 1)
		}
		result, err := attempt(ctx, member, AttemptsFor(member.Protocol))
		if err != nil {
			last = err
			if FailureDecision(0, err.Error(), "") == DecisionStop {
				return err
			}
			continue
		}
		if result.Status >= 200 && result.Status < 300 {
			return nil
		}
		last = fmt.Errorf("combo member %q returned HTTP %d", member.ID, result.Status)
		if FailureDecision(result.Status, result.Message, result.Code) == DecisionStop {
			return last
		}
	}
	if last != nil {
		return last
	}
	return fmt.Errorf("combo exhausted all members")
}
