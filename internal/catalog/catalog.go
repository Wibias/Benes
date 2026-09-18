package catalog

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/credentialpool"
)

const ConservativeContextWindow = 128000

type ContextSource string

const (
	ContextDiscovered          ContextSource = "discovered"
	ContextOperator            ContextSource = "operator"
	ContextStatic              ContextSource = "static"
	ContextCap                 ContextSource = "cap"
	ContextConservativeDefault ContextSource = "conservative_default"
)

type WindowMode string

const (
	WindowFallback WindowMode = "fallback"
	WindowOverride WindowMode = "override"
)

type ContextInput struct {
	Discovered   int
	Operator     int
	OperatorMode WindowMode
	Static       int
	Cap          int
	CapDisabled  bool
}

type ContextWindow struct {
	Tokens int           `json:"tokens"`
	Source ContextSource `json:"source"`
}

func EffectiveContextWindow(in ContextInput) ContextWindow {
	var tokens int
	var source ContextSource

	if in.Operator > 0 && in.OperatorMode == WindowOverride {
		tokens = in.Operator
		source = ContextOperator
	} else if in.Discovered > 0 {
		tokens = in.Discovered
		source = ContextDiscovered
	} else if in.Operator > 0 {
		tokens = in.Operator
		source = ContextOperator
	} else if in.Static > 0 {
		tokens = in.Static
		source = ContextStatic
	}

	if !in.CapDisabled && in.Cap > 0 {
		if tokens == 0 || in.Cap < tokens {
			return ContextWindow{Tokens: in.Cap, Source: ContextCap}
		}
	}
	if tokens > 0 {
		return ContextWindow{Tokens: tokens, Source: source}
	}
	return ContextWindow{Tokens: ConservativeContextWindow, Source: ContextConservativeDefault}
}

func EffectiveReasoningEfforts(configured []string, providerExact bool, modelExact *bool) []string {
	exact := providerExact
	if modelExact != nil {
		exact = *modelExact
	}
	out := uniqueStrings(configured)
	if len(out) == 0 {
		return nil
	}
	if exact {
		return out
	}
	for _, rung := range []string{"max", "ultra"} {
		if !containsString(out, rung) {
			out = append(out, rung)
		}
	}
	return out
}

func ResolveAutoReviewModel(raw json.RawMessage) (string, bool) {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil || root == nil {
		return "", false
	}
	value, exists := root["auto_review_model"]
	if !exists {
		return "", false
	}
	var model string
	if json.Unmarshal(value, &model) != nil {
		return "", false
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return "", false
	}
	return model, true
}

func EffectiveAutoCompactLimit(contextWindow, maxInput, configured int) int {
	if contextWindow <= 0 {
		return 0
	}
	derived := contextWindow * 9 / 10
	if maxInput > 0 && maxInput < derived {
		derived = maxInput
	}
	if configured > 0 && configured < derived {
		derived = configured
	}
	return derived
}

type CapabilityState string

const (
	CapabilityUnknown CapabilityState = "unknown"
	CapabilityTrue    CapabilityState = "true"
	CapabilityFalse   CapabilityState = "false"
)

type CapabilityKey struct {
	ProviderID  string
	Destination string
	ModelID     string
}

type ContextPolicyValue struct {
	Tokens int        `json:"tokens"`
	Mode   WindowMode `json:"mode"`
}

type Policy struct {
	RetainModels                     map[string]map[string]bool
	StaticVision                     map[CapabilityKey]CapabilityState
	ProviderContextCaps              map[string]int
	ProviderContextCapDisabled       map[string]bool
	ModelContextWindows              map[string]map[string]ContextPolicyValue
	PreserveExactReasoningRungs      map[string]bool
	ModelPreserveExactReasoningRungs map[string]map[string]*bool
	AutoCompactTokenLimits           map[string]map[string]int
}

type ConfiguredModel struct {
	ID               string
	ContextWindow    int
	MaxInput         int
	ReasoningEfforts []string
	Vision           CapabilityState
}

type DiscoveredModel struct {
	ID               string
	LogicalID        string
	ReasoningTier    string
	DisplayName      string
	ContextWindow    int
	MaxInput         int
	ReasoningEfforts []string
	Vision           CapabilityState
}

type Availability struct {
	Selectable bool   `json:"selectable"`
	Reason     string `json:"reason,omitempty"`
}

type Model struct {
	ID                    string
	DisplayName           string
	Context               ContextWindow
	// AdvertisedTokens is the catalog max (discovered or stored), before operator/cap.
	AdvertisedTokens int
	// StandardTokens is the provider's default window when it is below advertised.
	StandardTokens        int
	MaxInput              int
	AutoCompactTokenLimit int
	AutoReviewModel       string
	APITypes              []string
	ToolUse               CapabilityState
	Streaming             CapabilityState
	Reasoning             CapabilityState
	ReasoningEfforts      []string
	ReasoningWire         map[string]string
	Vision                CapabilityState
	Retained              bool
	Discovered            bool
	ConfirmedCallable     bool
	Availability          Availability
}

type ProjectionInput struct {
	ProviderID  string
	Destination string
	Policy      Policy
	Discovered  []DiscoveredModel
	Configured  []ConfiguredModel
	Candidates  []credentialpool.Candidate
	Now         time.Time
	RootConfig  json.RawMessage
}

func NormalizeDiscovered(rows []DiscoveredModel) []Model {
	type group struct {
		index int
		rows  map[string]DiscoveredModel
	}
	groups := make(map[string]*group)
	for i, row := range rows {
		if row.LogicalID == "" || !isReasoningTier(row.ReasoningTier) {
			continue
		}
		g, ok := groups[row.LogicalID]
		if !ok {
			g = &group{index: i, rows: make(map[string]DiscoveredModel)}
			groups[row.LogicalID] = g
		}
		if _, duplicate := g.rows[row.ReasoningTier]; duplicate {
			g.rows[row.ReasoningTier] = DiscoveredModel{}
			continue
		}
		g.rows[row.ReasoningTier] = row
	}

	complete := make(map[string]*group)
	for id, g := range groups {
		if len(g.rows) != 3 {
			continue
		}
		valid := true
		for _, tier := range []string{"low", "medium", "high"} {
			if g.rows[tier].ID == "" {
				valid = false
				break
			}
		}
		if valid {
			complete[id] = g
		}
	}

	out := make([]Model, 0, len(rows))
	emitted := make(map[string]bool)
	for _, row := range rows {
		if g := complete[row.LogicalID]; g != nil {
			if emitted[row.LogicalID] {
				continue
			}
			emitted[row.LogicalID] = true
			wire := map[string]string{
				"low":    g.rows["low"].ID,
				"medium": g.rows["medium"].ID,
				"high":   g.rows["high"].ID,
			}
			context := minPositive(g.rows["low"].ContextWindow, g.rows["medium"].ContextWindow, g.rows["high"].ContextWindow)
			maxInput := minPositive(g.rows["low"].MaxInput, g.rows["medium"].MaxInput, g.rows["high"].MaxInput)
			vision := intersectCapability([]CapabilityState{normalizedCapability(g.rows["low"].Vision), normalizedCapability(g.rows["medium"].Vision), normalizedCapability(g.rows["high"].Vision)})
			out = append(out, Model{
				ID:                row.LogicalID,
				Context:           discoveredContext(context),
				MaxInput:          maxInput,
				Reasoning:         CapabilityTrue,
				ReasoningEfforts:  []string{"low", "medium", "high"},
				ReasoningWire:     wire,
				Vision:            vision,
				Discovered:        true,
				ConfirmedCallable: true,
				Availability:      Availability{Selectable: true},
			})
			continue
		}
		efforts := uniqueStrings(row.ReasoningEfforts)
		if row.ReasoningTier != "" && isReasoningTier(row.ReasoningTier) && !containsString(efforts, row.ReasoningTier) {
			efforts = append(efforts, row.ReasoningTier)
		}
		wire := map[string]string(nil)
		if row.ReasoningTier != "" && isReasoningTier(row.ReasoningTier) {
			wire = map[string]string{row.ReasoningTier: row.ID}
		}
		out = append(out, Model{
			ID:                row.ID,
			Context:           discoveredContext(row.ContextWindow),
			MaxInput:          row.MaxInput,
			Reasoning:         reasoningCapability(CapabilityUnknown, efforts),
			ReasoningEfforts:  efforts,
			ReasoningWire:     wire,
			Vision:            normalizedCapability(row.Vision),
			Discovered:        true,
			ConfirmedCallable: true,
			Availability:      Availability{Selectable: true},
		})
	}
	return out
}

func Project(in ProjectionInput) ([]Model, error) {
	normalized := NormalizeDiscovered(in.Discovered)
	configured := make(map[string]ConfiguredModel, len(in.Configured))
	for _, row := range in.Configured {
		if strings.TrimSpace(row.ID) == "" {
			continue
		}
		configured[row.ID] = row
	}

	review, _ := ResolveAutoReviewModel(in.RootConfig)
	byID := make(map[string]Model, len(normalized)+len(configured))
	order := make([]string, 0, len(normalized)+len(configured))

	for _, base := range normalized {
		cfg := configured[base.ID]
		contextPolicy := contextPolicyFor(in.Policy, in.ProviderID, base.ID)
		capTokens := in.Policy.ProviderContextCaps[in.ProviderID]
		discoveredTokens := 0
		if base.Context.Source == ContextDiscovered {
			discoveredTokens = base.Context.Tokens
		}
		staticTokens := cfg.ContextWindow
		ctx := EffectiveContextWindow(ContextInput{
			Discovered:   discoveredTokens,
			Operator:     contextPolicy.Tokens,
			OperatorMode: contextPolicy.Mode,
			Static:       staticTokens,
			Cap:          capTokens,
			CapDisabled:  in.Policy.ProviderContextCapDisabled[in.ProviderID],
		})
		standardInput := cfg.MaxInput
		if standardInput <= 0 {
			standardInput = base.MaxInput
		}
		base.AdvertisedTokens, base.StandardTokens = advertisedCatalogWindows(discoveredTokens, staticTokens, standardInput)
		base.Context = ctx
		if cfg.MaxInput > 0 {
			base.MaxInput = cfg.MaxInput
		}
		if base.MaxInput > 0 && base.MaxInput > ctx.Tokens {
			base.MaxInput = ctx.Tokens
		}

		efforts := base.ReasoningEfforts
		configuredReasoning := CapabilityUnknown
		if cfg.ReasoningEfforts != nil {
			efforts = cfg.ReasoningEfforts
			if len(uniqueStrings(cfg.ReasoningEfforts)) == 0 {
				configuredReasoning = CapabilityFalse
			} else {
				configuredReasoning = CapabilityTrue
			}
		}
		base.ReasoningEfforts = EffectiveReasoningEfforts(
			efforts,
			in.Policy.PreserveExactReasoningRungs[in.ProviderID],
			modelExactPolicy(in.Policy, in.ProviderID, base.ID),
		)
		base.Reasoning = mergeCapabilityEvidence(base.Reasoning, configuredReasoning)
		if base.Reasoning != CapabilityTrue {
			base.ReasoningEfforts = nil
		}

		base.Vision = mergeCapabilityEvidence(base.Vision, cfg.Vision)
		if static, ok := in.Policy.StaticVision[CapabilityKey{ProviderID: in.ProviderID, Destination: in.Destination, ModelID: base.ID}]; ok {
			base.Vision = mergeCapabilityEvidence(base.Vision, static)
		}
		base.AutoReviewModel = review
		base.AutoCompactTokenLimit = EffectiveAutoCompactLimit(ctx.Tokens, base.MaxInput, autoCompactPolicyFor(in.Policy, in.ProviderID, base.ID))
		base.Availability = AvailabilityFromCandidates(in.Candidates, in.Now)
		byID[base.ID] = base
		order = append(order, base.ID)
	}

	retained := in.Policy.RetainModels[in.ProviderID]
	for id, enabled := range retained {
		if !enabled {
			continue
		}
		if _, exists := byID[id]; exists {
			continue
		}
		cfg, exists := configured[id]
		if !exists {
			cfg = ConfiguredModel{ID: id}
		}
		contextPolicy := contextPolicyFor(in.Policy, in.ProviderID, id)
		ctx := EffectiveContextWindow(ContextInput{
			Operator:     contextPolicy.Tokens,
			OperatorMode: contextPolicy.Mode,
			Static:       cfg.ContextWindow,
			Cap:          in.Policy.ProviderContextCaps[in.ProviderID],
			CapDisabled:  in.Policy.ProviderContextCapDisabled[in.ProviderID],
		})
		advertised, standard := advertisedCatalogWindows(0, cfg.ContextWindow, cfg.MaxInput)
		maxInput := cfg.MaxInput
		if maxInput > ctx.Tokens && ctx.Tokens > 0 {
			maxInput = ctx.Tokens
		}
		vision := mergeCapabilityEvidence(CapabilityUnknown, cfg.Vision)
		if static, ok := in.Policy.StaticVision[CapabilityKey{ProviderID: in.ProviderID, Destination: in.Destination, ModelID: id}]; ok {
			vision = mergeCapabilityEvidence(vision, static)
		}
		reasoningEfforts := EffectiveReasoningEfforts(
			cfg.ReasoningEfforts,
			in.Policy.PreserveExactReasoningRungs[in.ProviderID],
			modelExactPolicy(in.Policy, in.ProviderID, id),
		)
		reasoning := CapabilityUnknown
		if cfg.ReasoningEfforts != nil {
			if len(reasoningEfforts) == 0 {
				reasoning = CapabilityFalse
			} else {
				reasoning = CapabilityTrue
			}
		}
		model := Model{
			ID:                    id,
			Context:               ctx,
			AdvertisedTokens:      advertised,
			StandardTokens:        standard,
			MaxInput:              maxInput,
			AutoCompactTokenLimit: EffectiveAutoCompactLimit(ctx.Tokens, maxInput, autoCompactPolicyFor(in.Policy, in.ProviderID, id)),
			AutoReviewModel:       review,
			Reasoning:             reasoning,
			ReasoningEfforts:      reasoningEfforts,
			Vision:                vision,
			Retained:              true,
			Discovered:            false,
			ConfirmedCallable:     false,
			Availability:          AvailabilityFromCandidates(in.Candidates, in.Now),
		}
		byID[id] = model
		order = append(order, id)
	}

	out := make([]Model, 0, len(order))
	seen := make(map[string]bool, len(order))
	for _, id := range order {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, byID[id])
	}
	return out, nil
}

func AvailabilityFromCandidates(candidates []credentialpool.Candidate, now time.Time) Availability {
	if len(candidates) == 0 {
		return Availability{Selectable: true}
	}
	allExhausted := true
	for _, candidate := range candidates {
		if credentialpool.EvaluateEvidence(candidate.Evidence, now) != credentialpool.AvailabilityExhausted {
			allExhausted = false
			break
		}
	}
	if allExhausted {
		return Availability{Selectable: false, Reason: "no_credit"}
	}
	return Availability{Selectable: true}
}

func SynthesizeCombo(id string, members []Model) (Model, error) {
	if len(members) == 0 {
		return Model{}, ErrEmptyCombo
	}
	result := cloneModel(members[0])
	result.ID = id
	result.Context = members[0].Context
	result.MaxInput = positiveOrZero(members[0].MaxInput)
	result.AutoCompactTokenLimit = positiveOrZero(members[0].AutoCompactTokenLimit)
	result.APITypes = append([]string(nil), members[0].APITypes...)
	result.ToolUse = normalizedCapability(members[0].ToolUse)
	result.Streaming = normalizedCapability(members[0].Streaming)
	result.Reasoning = normalizedCapability(members[0].Reasoning)
	result.ReasoningEfforts = append([]string(nil), members[0].ReasoningEfforts...)
	result.ReasoningWire = nil
	visions := make([]CapabilityState, 0, len(members))
	toolUse := make([]CapabilityState, 0, len(members))
	streaming := make([]CapabilityState, 0, len(members))
	reasoning := make([]CapabilityState, 0, len(members))
	visions = append(visions, normalizedCapability(members[0].Vision))
	toolUse = append(toolUse, result.ToolUse)
	streaming = append(streaming, result.Streaming)
	reasoning = append(reasoning, result.Reasoning)
	result.Discovered = true
	result.ConfirmedCallable = true
	result.Retained = false
	result.Availability = members[0].Availability

	for _, member := range members[1:] {
		if member.Context.Tokens > 0 && (result.Context.Tokens <= 0 || member.Context.Tokens < result.Context.Tokens) {
			result.Context = member.Context
		}
		result.MaxInput = minPositivePair(result.MaxInput, member.MaxInput)
		result.AutoCompactTokenLimit = minPositivePair(result.AutoCompactTokenLimit, member.AutoCompactTokenLimit)
		result.APITypes = intersectStrings(result.APITypes, member.APITypes)
		result.ReasoningEfforts = intersectStrings(result.ReasoningEfforts, member.ReasoningEfforts)
		visions = append(visions, normalizedCapability(member.Vision))
		toolUse = append(toolUse, normalizedCapability(member.ToolUse))
		streaming = append(streaming, normalizedCapability(member.Streaming))
		reasoning = append(reasoning, normalizedCapability(member.Reasoning))
		result.ConfirmedCallable = result.ConfirmedCallable && member.ConfirmedCallable
		if !member.Availability.Selectable && result.Availability.Selectable {
		} else if member.Availability.Selectable {
			result.Availability = Availability{Selectable: true}
		}
	}
	result.Vision = intersectCapability(visions)
	result.ToolUse = intersectCapability(toolUse)
	result.Streaming = intersectCapability(streaming)
	result.Reasoning = intersectCapability(reasoning)
	if result.AutoCompactTokenLimit == 0 {
		result.AutoCompactTokenLimit = EffectiveAutoCompactLimit(result.Context.Tokens, result.MaxInput, 0)
	}
	return result, nil
}

func ProjectAlias(base Model, id string, contextCap int) Model {
	alias := cloneModel(base)
	alias.ID = id
	if contextCap > 0 && (alias.Context.Tokens == 0 || contextCap < alias.Context.Tokens) {
		alias.Context = ContextWindow{Tokens: contextCap, Source: ContextCap}
	}
	if alias.MaxInput > 0 && alias.Context.Tokens > 0 && alias.MaxInput > alias.Context.Tokens {
		alias.MaxInput = alias.Context.Tokens
	}
	alias.AutoCompactTokenLimit = EffectiveAutoCompactLimit(alias.Context.Tokens, alias.MaxInput, base.AutoCompactTokenLimit)
	return alias
}

func advertisedCatalogWindows(discovered, static, maxInput int) (advertised, standard int) {
	advertised = discovered
	if advertised <= 0 {
		advertised = static
	}
	if maxInput > 0 && advertised > 0 && maxInput < advertised {
		standard = maxInput
	}
	return advertised, standard
}

func discoveredContext(tokens int) ContextWindow {
	if tokens > 0 {
		return ContextWindow{Tokens: tokens, Source: ContextDiscovered}
	}
	return ContextWindow{}
}

func normalizedCapability(value CapabilityState) CapabilityState {
	switch value {
	case CapabilityTrue, CapabilityFalse:
		return value
	default:
		return CapabilityUnknown
	}
}

func mergeCapabilityEvidence(base, overlay CapabilityState) CapabilityState {
	base = normalizedCapability(base)
	overlay = normalizedCapability(overlay)
	if base == CapabilityFalse || overlay == CapabilityFalse {
		return CapabilityFalse
	}
	if base == CapabilityTrue || overlay == CapabilityTrue {
		return CapabilityTrue
	}
	return CapabilityUnknown
}

func reasoningCapability(value CapabilityState, efforts []string) CapabilityState {
	value = normalizedCapability(value)
	if value != CapabilityUnknown {
		return value
	}
	if len(efforts) > 0 {
		return CapabilityTrue
	}
	return CapabilityUnknown
}

func intersectCapability(values []CapabilityState) CapabilityState {
	if len(values) == 0 {
		return CapabilityUnknown
	}
	allTrue := true
	for _, value := range values {
		switch normalizedCapability(value) {
		case CapabilityFalse:
			return CapabilityFalse
		case CapabilityUnknown:
			allTrue = false
		case CapabilityTrue:
		}
	}
	if allTrue {
		return CapabilityTrue
	}
	return CapabilityUnknown
}

func contextPolicyFor(policy Policy, providerID, modelID string) ContextPolicyValue {
	if byModel := policy.ModelContextWindows[providerID]; byModel != nil {
		return byModel[modelID]
	}
	return ContextPolicyValue{}
}

func modelExactPolicy(policy Policy, providerID, modelID string) *bool {
	if byModel := policy.ModelPreserveExactReasoningRungs[providerID]; byModel != nil {
		return byModel[modelID]
	}
	return nil
}

func autoCompactPolicyFor(policy Policy, providerID, modelID string) int {
	if byModel := policy.AutoCompactTokenLimits[providerID]; byModel != nil {
		return byModel[modelID]
	}
	return 0
}

func isReasoningTier(value string) bool {
	switch value {
	case "low", "medium", "high":
		return true
	default:
		return false
	}
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func intersectStrings(a, b []string) []string {
	set := make(map[string]bool, len(b))
	for _, value := range b {
		set[value] = true
	}
	out := make([]string, 0, len(a))
	for _, value := range a {
		if set[value] {
			out = append(out, value)
		}
	}
	return out
}

func minPositive(values ...int) int {
	out := 0
	for _, value := range values {
		if value > 0 && (out == 0 || value < out) {
			out = value
		}
	}
	return out
}

func minPositivePair(a, b int) int {
	if a <= 0 {
		return positiveOrZero(b)
	}
	if b <= 0 {
		return a
	}
	if b < a {
		return b
	}
	return a
}

func positiveOrZero(value int) int {
	if value > 0 {
		return value
	}
	return 0
}

func cloneModel(model Model) Model {
	out := model
	out.APITypes = append([]string(nil), model.APITypes...)
	out.ReasoningEfforts = append([]string(nil), model.ReasoningEfforts...)
	if model.ReasoningWire != nil {
		out.ReasoningWire = make(map[string]string, len(model.ReasoningWire))
		for key, value := range model.ReasoningWire {
			out.ReasoningWire[key] = value
		}
	}
	return out
}

func StableSort(rows []Model) []Model {
	out := make([]Model, len(rows))
	for i, row := range rows {
		out[i] = cloneModel(row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func equalJSON(a, b json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
}
