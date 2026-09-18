package modeldiscovery

import (
	"encoding/json"
	"strings"
)

// CatalogEntry is one row from an upstream model list.
type CatalogEntry struct {
	ID            string
	ContextWindow int
	MaxInput      int
}

func ParseModelIDs(raw []byte) ([]string, bool) {
	ids, ok := ParseCatalogIDs(raw)
	if !ok {
		return nil, false
	}
	return ids, true
}

func ParseCatalogIDs(raw []byte) ([]string, bool) {
	entries, ok := ParseCatalog(raw)
	if !ok {
		return nil, false
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	return ids, true
}

func ParseCatalog(raw []byte) ([]CatalogEntry, bool) {
	var envelope struct {
		Data   []json.RawMessage `json:"data"`
		Models []json.RawMessage `json:"models"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return nil, false
	}
	if envelope.Data == nil && envelope.Models == nil {
		return nil, false
	}
	out := make([]CatalogEntry, 0, len(envelope.Data)+len(envelope.Models))
	seen := map[string]struct{}{}
	add := func(entry CatalogEntry) {
		entry.ID = strings.TrimSpace(entry.ID)
		if entry.ID == "" {
			return
		}
		if _, ok := seen[entry.ID]; ok {
			return
		}
		seen[entry.ID] = struct{}{}
		out = append(out, entry)
	}
	for _, row := range envelope.Data {
		add(parseCatalogRow(row, false))
	}
	for _, row := range envelope.Models {
		entry := parseCatalogRow(row, true)
		if entry.ID == "" {
			continue
		}
		add(entry)
	}
	return out, true
}

func parseCatalogRow(raw json.RawMessage, skipHidden bool) CatalogEntry {
	var row struct {
		ID               string  `json:"id"`
		Slug             string  `json:"slug"`
		Name             string  `json:"name"`
		Visibility       string  `json:"visibility"`
		ContextLength    float64 `json:"context_length"`
		ContextWindow    float64 `json:"context_window"`
		ContextWindowAlt float64 `json:"contextWindow"`
		MaxContextWindow float64 `json:"max_context_window"`
		MaxModelLen      float64 `json:"max_model_len"`
		MaxInputTokens   float64 `json:"max_input_tokens"`
		InputTokenLimit  float64 `json:"inputTokenLimit"`
		MaxInput         float64 `json:"maxInput"`
		Context          struct {
			Tokens float64 `json:"tokens"`
			Window float64 `json:"window"`
		} `json:"context"`
		TopProvider struct {
			ContextLength float64 `json:"context_length"`
		} `json:"top_provider"`
		Capabilities struct {
			Limits struct {
				MaxContextWindowTokens float64 `json:"max_context_window_tokens"`
				MaxPromptTokens        float64 `json:"max_prompt_tokens"`
			} `json:"limits"`
		} `json:"capabilities"`
	}
	if json.Unmarshal(raw, &row) != nil {
		return CatalogEntry{}
	}
	if skipHidden && strings.EqualFold(strings.TrimSpace(row.Visibility), "hide") {
		return CatalogEntry{}
	}
	id := strings.TrimSpace(row.Slug)
	if id == "" {
		id = strings.TrimSpace(row.ID)
	}
	if id == "" {
		id = strings.TrimPrefix(strings.TrimSpace(row.Name), "models/")
	}
	nestedWindow := positiveTokens(row.Capabilities.Limits.MaxContextWindowTokens)
	nestedPrompt := positiveTokens(row.Capabilities.Limits.MaxPromptTokens)
	standard := firstPositiveTokens(row.ContextWindow, row.ContextWindowAlt)
	available := maxPositiveTokens(
		row.MaxContextWindow,
		row.ContextLength,
		row.MaxModelLen,
		row.InputTokenLimit,
		row.Context.Tokens,
		row.Context.Window,
		row.TopProvider.ContextLength,
		float64(standard),
	)
	maxInput := firstPositiveTokens(row.MaxInputTokens, row.MaxInput, float64(nestedPrompt))
	if nestedWindow > 0 {
		available = nestedWindow
	} else {
		available, standard = liftAdvertisedContext(id, available, standard)
	}
	if maxInput <= 0 && standard > 0 && standard < available {
		maxInput = standard
	}
	if maxInput > available && available > 0 {
		maxInput = available
	}
	return CatalogEntry{ID: id, ContextWindow: available, MaxInput: maxInput}
}

func firstPositiveTokens(values ...float64) int {
	for _, value := range values {
		if tokens := positiveTokens(value); tokens > 0 {
			return tokens
		}
	}
	return 0
}

func maxPositiveTokens(values ...float64) int {
	out := 0
	for _, value := range values {
		if tokens := positiveTokens(value); tokens > out {
			out = tokens
		}
	}
	return out
}

func liftAdvertisedContext(id string, advertised, standard int) (int, int) {
	if !gpt56Family(id) {
		return advertised, standard
	}
	const available = 1_050_000
	if advertised <= 0 {
		return available, standard
	}
	if advertised >= available {
		return advertised, standard
	}
	if standard <= 0 {
		standard = advertised
	}
	return available, standard
}

func gpt56Family(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	return id == "gpt-5.6" || strings.HasPrefix(id, "gpt-5.6-")
}

func operatorContextPreset(tokens int) bool {
	switch tokens {
	case 32_000, 64_000, 128_000, 256_000:
		return true
	default:
		return false
	}
}

func positiveTokens(value float64) int {
	if value <= 0 || value > 100_000_000 {
		return 0
	}
	return int(value)
}

func EntriesFromIDs(ids []string) []CatalogEntry {
	out := make([]CatalogEntry, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, CatalogEntry{ID: id})
	}
	return out
}

// ParseStoredModels reads providers.<id>.models as either string ids or {id, contextWindow} objects.
// ParseProviderCatalog reads a provider record's stored models plus seed window maps.
func ParseProviderCatalog(raw json.RawMessage) []CatalogEntry {
	var rec struct {
		Models              json.RawMessage    `json:"models"`
		ModelContextWindows map[string]float64 `json:"modelContextWindows"`
		ModelMaxInputTokens map[string]float64 `json:"modelMaxInputTokens"`
	}
	if json.Unmarshal(raw, &rec) != nil {
		return nil
	}
	entries := ParseStoredModels(rec.Models)
	if len(entries) == 0 {
		return nil
	}
	for i, entry := range entries {
		if entry.ContextWindow <= 0 {
			if tokens := positiveTokens(rec.ModelContextWindows[entry.ID]); !operatorContextPreset(tokens) {
				entries[i].ContextWindow = tokens
			}
		}
		if entry.MaxInput <= 0 {
			entries[i].MaxInput = positiveTokens(rec.ModelMaxInputTokens[entry.ID])
		}
		if entries[i].ContextWindow > 0 {
			advertised, standard := liftAdvertisedContext(entry.ID, entries[i].ContextWindow, entries[i].MaxInput)
			if entries[i].MaxInput <= 0 && standard > 0 && standard < advertised {
				entries[i].MaxInput = standard
			}
			entries[i].ContextWindow = advertised
		}
	}
	return entries
}

func ParseStoredModels(raw json.RawMessage) []CatalogEntry {
	if len(bytesTrimSpace(raw)) == 0 {
		return nil
	}
	var names []string
	if json.Unmarshal(raw, &names) == nil {
		return EntriesFromIDs(names)
	}
	var objects []struct {
		ID            string  `json:"id"`
		ContextWindow float64 `json:"contextWindow"`
		MaxInput      float64 `json:"maxInput"`
	}
	if json.Unmarshal(raw, &objects) != nil {
		return nil
	}
	out := make([]CatalogEntry, 0, len(objects))
	seen := map[string]struct{}{}
	for _, object := range objects {
		id := strings.TrimSpace(object.ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, CatalogEntry{
			ID:            id,
			ContextWindow: positiveTokens(object.ContextWindow),
			MaxInput:      positiveTokens(object.MaxInput),
		})
	}
	return out
}

func bytesTrimSpace(raw json.RawMessage) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}

func IDsFromEntries(entries []CatalogEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	return out
}
