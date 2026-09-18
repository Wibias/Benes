package modelprobe

import (
	"strings"
	"time"
)

type State string

const (
	StateAvailable        State = "available"
	StateUnavailable      State = "unavailable"
	StateAuthRequired     State = "auth_required"
	StateQuotaLimited     State = "quota_limited"
	StateUnsupportedProbe State = "unsupported_probe"
	StateUnknown          State = "unknown"
)

const (
	KindCatalog    = "catalog"
	KindGeneration = "generation"
)

type Timing struct {
	HeadersMs     *int64 `json:"headersMs,omitempty"`
	FirstOutputMs *int64 `json:"firstOutputMs,omitempty"`
	TTFTMs        *int64 `json:"ttftMs,omitempty"`
	TotalMs       int64  `json:"totalMs"`
}

type Result struct {
	Provider           string    `json:"provider"`
	Model              string    `json:"model"`
	DestinationHost    string    `json:"destinationHost,omitempty"`
	CredentialSlot     string    `json:"credentialSlot,omitempty"`
	State              State     `json:"state"`
	Kind               string    `json:"kind"`
	TestedAt           time.Time `json:"testedAt,omitempty"`
	Timing             Timing    `json:"timing"`
	Reason             string    `json:"reason,omitempty"`
	GenerationIncurred bool      `json:"generationIncurred"`
}

func (r Result) Safe() Result {
	out := r
	out.Reason = safeReason(r.Reason)
	return out
}

func UnknownResult(provider, model string) Result {
	return Result{
		Provider: strings.TrimSpace(provider),
		Model:    strings.TrimSpace(model),
		State:    StateUnknown,
		Reason:   "unprobed",
	}
}

func ClassifyStatus(status int) State {
	switch {
	case status == 401 || status == 403:
		return StateAuthRequired
	case status == 402 || status == 429:
		return StateQuotaLimited
	case status >= 500:
		return StateUnavailable
	case status == 404:
		return StateUnknown
	case status >= 200 && status < 300:
		return StateAvailable
	default:
		return StateUnavailable
	}
}

func safeReason(reason string) string {
	reason = strings.TrimSpace(reason)
	switch reason {
	case "", "unprobed", "model_not_listed", "timeout", "canceled", "network",
		"static_catalog", "forward_auth", "missing_destination", "private_destination",
		"invalid_config", "generation_required", "http_401", "http_403", "http_402",
		"http_429", "http_404", "http_5xx", "http_error", "lookalike_destination",
		"unsupported_adapter", "stale":
		if reason == "" {
			return ""
		}
		return reason
	}
	if strings.HasPrefix(reason, "http_") {
		return reason
	}
	return "probe_failed"
}
