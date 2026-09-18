package server

import (
	"encoding/json"
	"strings"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/compat"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/protocol"
)

func (h *handler) policyCandidateViews(record routingProfileRecord) []policyCandidateView {
	catalogByID := map[string]catalog.Model{}
	if h != nil {
		for _, model := range h.catalogModels {
			catalogByID[model.ID] = model
		}
	}
	adapters, authModes := h.providerRoutingMeta()
	views := make([]policyCandidateView, 0, len(record.Candidates))
	for _, candidate := range record.Candidates {
		view := policyCandidateView{
			Provider: candidate.Provider,
			Model:    candidate.Model,
			Adapter:  adapters[candidate.Provider],
			AuthMode: authModes[candidate.Provider],
		}
		if model, ok := catalogByID[candidate.Provider+"/"+candidate.Model]; ok {
			view.ContextTokens = model.Context.Tokens
			view.Vision = model.Vision
		}
		views = append(views, view)
	}
	return views
}

func (h *handler) providerRoutingMeta() (adapters map[string]string, authModes map[string]string) {
	adapters = map[string]string{}
	authModes = map[string]string{}
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return adapters, authModes
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return adapters, authModes
	}
	for id, raw := range disk.Providers {
		adapters[id] = compat.ProviderAdapter(raw)
		var body struct {
			AuthMode string `json:"authMode"`
		}
		if json.Unmarshal(raw, &body) == nil {
			authModes[id] = strings.TrimSpace(body.AuthMode)
		}
	}
	return adapters, authModes
}

type policyRequestEvidence struct {
	ContextWindow      int
	Tools              bool
	Image              bool
	Structured         bool
	Encrypted          bool
	PreviousResponseID string
	PromptCacheKey     string
	User               string
	FirstUserText      string
}

type policyCandidateView struct {
	Provider      string
	Model         string
	ContextTokens int
	Vision        catalog.CapabilityState
	Adapter       string
	AuthMode      string
}

func policyEvidenceFromRequest(req protocol.ParsedRequest) policyRequestEvidence {
	ev := policyRequestEvidence{
		Tools:              len(req.Context.Tools) > 0,
		Structured:         req.StructuredOutput,
		PreviousResponseID: strings.TrimSpace(req.PreviousResponseID),
		FirstUserText:      firstUserText(req.Context),
	}
	if req.Options.PromptCacheKey != nil {
		ev.PromptCacheKey = strings.TrimSpace(*req.Options.PromptCacheKey)
	}
	if req.Options.User != nil {
		ev.User = strings.TrimSpace(*req.Options.User)
	}
	for _, msg := range req.Context.Messages {
		if msg.ContainsEncryptedContent {
			ev.Encrypted = true
		}
		for _, part := range msg.Content {
			if part.Type == protocol.ContentImage || strings.TrimSpace(part.ImageURL) != "" {
				ev.Image = true
			}
		}
	}
	return ev
}

func firstUserText(ctx protocol.Context) string {
	for _, msg := range ctx.Messages {
		if msg.Role != protocol.RoleUser {
			continue
		}
		var b strings.Builder
		for _, part := range msg.Content {
			if part.Type == protocol.ContentText && strings.TrimSpace(part.Text) != "" {
				b.WriteString(part.Text)
			}
		}
		if text := strings.TrimSpace(b.String()); text != "" {
			return text
		}
	}
	return ""
}

func filterPolicyCandidates(record routingProfileRecord, views []policyCandidateView, evidence policyRequestEvidence) ([]routingProfileCandidate, []map[string]any, any) {
	unknownMode := evidenceMode(record.UnknownEvidence, "capability", "allow")
	compatUnknown := unknownMode
	compatDegraded := "penalize"
	minStatus := ""
	suites := []string{}
	if record.Compatibility != nil {
		if mode, _ := record.Compatibility["unknownEvidence"].(string); mode != "" {
			compatUnknown = mode
		}
		if mode, _ := record.Compatibility["degradedEvidence"].(string); mode != "" {
			compatDegraded = mode
		}
		if status, _ := record.Compatibility["minStatus"].(string); status != "" {
			minStatus = status
		}
		suites = requiredSuiteIDs(record.Compatibility["requiredSuites"])
	}
	needImage := boolValue(record.Require.ImageInput) || evidence.Image
	needTools := boolValue(record.Require.Tools) || evidence.Tools
	needStructured := boolValue(record.Require.StructuredOutput) || evidence.Structured
	needLocal := boolValue(record.Require.LocalOnly) || record.Require.RemoteAllowed != nil && !*record.Require.RemoteAllowed
	needEncrypted := boolValue(record.Require.EncryptedCodexTasks) || evidence.Encrypted
	minWindow := record.Require.MinContextWindow
	if float64(evidence.ContextWindow) > minWindow {
		minWindow = float64(evidence.ContextWindow)
	}

	eligible := make([]routingProfileCandidate, 0, len(views))
	rows := make([]map[string]any, 0, len(views))
	var selected any
	for i, view := range views {
		exclusions := make([]map[string]any, 0)
		if minWindow > 0 {
			if view.ContextTokens <= 0 {
				exclusions = appendUnknown(exclusions, unknownMode, "unknown-capability")
			} else if float64(view.ContextTokens) < minWindow {
				exclusions = append(exclusions, map[string]any{"code": "capability-unsatisfied"})
			}
		}
		if needImage {
			switch view.Vision {
			case catalog.CapabilityTrue:
			case catalog.CapabilityFalse:
				exclusions = append(exclusions, map[string]any{"code": "capability-unsatisfied"})
			default:
				exclusions = appendUnknown(exclusions, unknownMode, "unknown-capability")
			}
		}
		if needTools {
			supported, known := adapterSupportsTools(view.Adapter)
			if !known {
				exclusions = appendUnknown(exclusions, unknownMode, "unknown-capability")
			} else if !supported {
				exclusions = append(exclusions, map[string]any{"code": "capability-unsatisfied"})
			}
		}
		if needStructured {
			supported, known := adapterSupportsStructured(view.Adapter)
			if !known {
				exclusions = appendUnknown(exclusions, unknownMode, "unknown-capability")
			} else if !supported {
				exclusions = append(exclusions, map[string]any{"code": "capability-unsatisfied"})
			}
		}
		if needLocal && !adapterIsLocal(view.Adapter) {
			exclusions = append(exclusions, map[string]any{"code": "capability-unsatisfied"})
		}
		if needEncrypted && !supportsEncryptedCodex(view) {
			exclusions = append(exclusions, map[string]any{"code": "capability-unsatisfied"})
		}
		for _, suite := range suites {
			verdict := compat.Classify(suite, view.Adapter, view.Vision)
			switch verdict {
			case compat.VerdictUnsupported:
				exclusions = append(exclusions, map[string]any{"code": "compatibility-unsatisfied"})
			case compat.VerdictUnknown:
				exclusions = appendUnknown(exclusions, compatUnknown, "compatibility-unsatisfied")
			case compat.VerdictDegraded:
				if minStatus == "VERIFIED" || compatDegraded == "exclude" {
					exclusions = append(exclusions, map[string]any{"code": "compatibility-unsatisfied"})
				}
			}
		}
		ok := len(exclusions) == 0
		if ok {
			eligible = append(eligible, routingProfileCandidate{Provider: view.Provider, Model: view.Model})
			if selected == nil {
				selected = i
			}
		}
		rows = append(rows, map[string]any{
			"provider":   view.Provider,
			"model":      view.Model,
			"eligible":   ok,
			"exclusions": exclusions,
		})
	}
	return eligible, rows, selected
}

func requiredSuiteIDs(raw any) []string {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	seen := map[string]struct{}{}
	for _, row := range list {
		item, ok := row.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(stringValue(item["suiteId"]))
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func evidenceMode(unknown map[string]any, key, fallback string) string {
	if unknown == nil {
		return fallback
	}
	if mode, _ := unknown[key].(string); mode != "" {
		return mode
	}
	return fallback
}

func adapterSupportsTools(adapter string) (supported bool, known bool) {
	switch strings.TrimSpace(strings.ToLower(adapter)) {
	case "openai-responses", "openai-chat", "anthropic", "google", "xai":
		return true, true
	case "":
		return false, false
	default:
		return false, true
	}
}

func adapterSupportsStructured(adapter string) (supported bool, known bool) {
	switch strings.TrimSpace(strings.ToLower(adapter)) {
	case "openai-responses", "openai-chat":
		return true, true
	case "":
		return false, false
	default:
		return false, true
	}
}

func adapterIsLocal(adapter string) bool {
	switch strings.TrimSpace(strings.ToLower(adapter)) {
	case "ollama", "local", "llamacpp", "llama.cpp", "lmstudio":
		return true
	default:
		return false
	}
}

func supportsEncryptedCodex(view policyCandidateView) bool {
	return strings.EqualFold(strings.TrimSpace(view.Adapter), "openai-responses") &&
		strings.EqualFold(strings.TrimSpace(view.AuthMode), "forward")
}
