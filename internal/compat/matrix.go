package compat

import (
	"encoding/json"
	"strings"

	"github.com/Wibias/Benes/internal/catalog"
)

const SpecVersion = "capability-v1"

var Harnesses = []string{"codex", "claude", "grok", "opencode"}

const (
	VerdictUnknown     = "UNKNOWN"
	VerdictVerified    = "VERIFIED"
	VerdictDegraded    = "DEGRADED"
	VerdictUnsupported = "UNSUPPORTED"
	VerdictClaimed     = "CLAIMED"
)

func ProviderAdapter(raw json.RawMessage) string {
	var body struct {
		Adapter string `json:"adapter"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	return strings.TrimSpace(body.Adapter)
}

func SplitModelID(id string) (provider, model string) {
	provider, model, found := strings.Cut(strings.TrimSpace(id), "/")
	if !found {
		return "", id
	}
	return provider, model
}

func Classify(harness, adapter string, vision catalog.CapabilityState) string {
	adapter = strings.TrimSpace(strings.ToLower(adapter))
	switch harness {
	case "codex":
		switch adapter {
		case "openai-responses":
			return VerdictVerified
		case "openai-chat", "anthropic", "google", "xai":
			return VerdictDegraded
		default:
			if adapter == "" {
				return VerdictUnknown
			}
			return VerdictUnsupported
		}
	case "claude":
		switch adapter {
		case "anthropic":
			return VerdictVerified
		case "openai-responses", "openai-chat":
			return VerdictDegraded
		default:
			if adapter == "" {
				return VerdictUnknown
			}
			return VerdictUnsupported
		}
	case "grok":
		switch adapter {
		case "openai-responses", "xai":
			return VerdictVerified
		case "openai-chat":
			return VerdictDegraded
		default:
			if adapter == "" {
				return VerdictUnknown
			}
			return VerdictUnsupported
		}
	case "opencode":
		switch adapter {
		case "openai-chat":
			return VerdictVerified
		case "openai-responses", "anthropic":
			return VerdictDegraded
		default:
			if adapter == "" {
				return VerdictUnknown
			}
			return VerdictUnsupported
		}
	default:
		return VerdictUnknown
	}
}

func VisionNote(vision catalog.CapabilityState) string {
	switch vision {
	case catalog.CapabilityTrue:
		return "vision"
	case catalog.CapabilityFalse:
		return "no-vision"
	default:
		return ""
	}
}
