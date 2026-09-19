package capability

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers/xaicapability"
	"github.com/Wibias/Benes/internal/requestpolicy"
)

var ErrStructuredOutputUnsupported = errors.New("structured output is unsupported")

type Policy struct {
	ProviderID                    string
	Protocol                      string
	Endpoint                      string
	AuthClass                     string
	SupportsServiceTier           *bool
	ModelSupportsServiceTier      map[string]bool
	ChatServiceTier               bool
	SupportsStructuredOutput      *bool
	ModelSupportsStructuredOutput map[string]bool
	NoStructuredOutputModels      []string
}

// Apply resolves the admitted request tier against explicit client intent and then
// applies the provider/model capability gate. configuredServiceTier is the policy
// value admitted once for this logical request; Apply never reads live settings.
func Apply(req *protocol.ParsedRequest, policy Policy, configuredServiceTier string) error {
	if req == nil {
		return nil
	}
	model := firstNonEmpty(req.UpstreamModelID, req.ModelID)
	if err := RequireStructuredOutput(req, policy, model); err != nil {
		return err
	}
	req.Options.ServiceTier = requestpolicy.ResolveServiceTier(req.Options.ServiceTier, configuredServiceTier)
	req.Options.ServiceTier = DecideServiceTier(policy, model, req.Options.ServiceTier)
	tier, err := xaicapability.ApplyPriority(req.Options.ServiceTier, policy.AuthClass, policy.Endpoint)
	if err != nil {
		return err
	}
	req.Options.ServiceTier = tier
	return nil
}

func DecideServiceTier(policy Policy, modelID string, requested *string) *string {
	if requested == nil || strings.TrimSpace(*requested) == "" {
		return requested
	}
	if !canForwardServiceTier(policy, modelID, requested) {
		return nil
	}
	return requested
}

func StructuredOutputSupport(policy Policy, modelID string) (supported bool, known bool) {
	if exactListed(policy.NoStructuredOutputModels, modelID) {
		return false, true
	}
	if value, ok := exactModelValue(policy.ModelSupportsStructuredOutput, modelID); ok {
		return value, true
	}
	if policy.SupportsStructuredOutput != nil {
		return *policy.SupportsStructuredOutput, true
	}
	switch strings.ToLower(strings.TrimSpace(policy.Protocol)) {
	case "openai-responses", "openai-chat":
		return true, true
	case "":
		return true, false
	default:
		return false, false
	}
}

func AllowStructuredOutput(policy Policy, modelID string) bool {
	supported, known := StructuredOutputSupport(policy, modelID)
	return supported || !known
}

func RequireStructuredOutput(req *protocol.ParsedRequest, policy Policy, modelID string) error {
	if req == nil || (!req.StructuredOutput && req.Options.TextFormat == nil) {
		return nil
	}
	supported, known := StructuredOutputSupport(policy, modelID)
	if known && supported {
		return nil
	}
	provider := strings.TrimSpace(policy.ProviderID)
	if provider == "" {
		provider = strings.TrimSpace(policy.Protocol)
	}
	if provider == "" {
		provider = "provider"
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return fmt.Errorf("%w for %s", ErrStructuredOutputUnsupported, provider)
	}
	return fmt.Errorf("%w for %s model %s", ErrStructuredOutputUnsupported, provider, modelID)
}

func SupportsServiceTier(policy Policy, modelID string) *bool {
	if policy.SupportsServiceTier != nil && !*policy.SupportsServiceTier {
		return boolPtr(false)
	}
	if value, ok := exactModelValue(policy.ModelSupportsServiceTier, modelID); ok {
		return boolPtr(value)
	}
	return policy.SupportsServiceTier
}

func canSerializeChatServiceTier(policy Policy, modelID string) bool {
	exact, hasExact := exactModelValue(policy.ModelSupportsServiceTier, modelID)
	if (policy.SupportsServiceTier != nil && !*policy.SupportsServiceTier) || (hasExact && !exact) {
		return false
	}
	return policy.ChatServiceTier || (hasExact && exact)
}

func canForwardServiceTier(policy Policy, modelID string, requested *string) bool {
	if denied(policy, modelID) {
		return false
	}
	if openRouterPriorityException(policy, modelID, requested) {
		return true
	}
	if policy.Protocol == "openai-chat" && !canSerializeChatServiceTier(policy, modelID) {
		return false
	}
	if isFlex(requested) && exactFastOnly(policy, modelID) {
		return false
	}
	return true
}

func denied(policy Policy, modelID string) bool {
	if policy.SupportsServiceTier != nil && !*policy.SupportsServiceTier {
		return true
	}
	value, ok := exactModelValue(policy.ModelSupportsServiceTier, modelID)
	return ok && !value
}

func exactFastOnly(policy Policy, modelID string) bool {
	value, ok := exactModelValue(policy.ModelSupportsServiceTier, modelID)
	providerWide := policy.SupportsServiceTier != nil && *policy.SupportsServiceTier
	return ok && value && !providerWide && !policy.ChatServiceTier
}

func isFlex(requested *string) bool {
	return requested != nil && strings.EqualFold(strings.TrimSpace(*requested), "flex")
}

func openRouterPriorityException(policy Policy, modelID string, requested *string) bool {
	if requested == nil || !strings.EqualFold(strings.TrimSpace(*requested), "priority") {
		return false
	}
	if !canonicalOpenRouter(policy.Endpoint) {
		return false
	}
	if policy.SupportsServiceTier != nil && !*policy.SupportsServiceTier {
		return false
	}
	if value, ok := exactModelValue(policy.ModelSupportsServiceTier, modelID); ok && !value {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(modelID)), "openai/")
}

func exactListed(list []string, modelID string) bool {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return false
	}
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), modelID) {
			return true
		}
	}
	return false
}

func exactModelValue(record map[string]bool, modelID string) (bool, bool) {
	if record == nil {
		return false, false
	}
	if value, ok := record[modelID]; ok {
		return value, true
	}
	folded := strings.ToLower(strings.TrimSpace(modelID))
	for key, value := range record {
		if strings.ToLower(strings.TrimSpace(key)) == folded {
			return value, true
		}
	}
	return false, false
}

func canonicalOpenRouter(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "https") || !strings.EqualFold(parsed.Hostname(), "openrouter.ai") {
		return false
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	return path == "/api/v1" || strings.HasPrefix(path, "/api/v1/")
}

func boolPtr(v bool) *bool { return &v }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
