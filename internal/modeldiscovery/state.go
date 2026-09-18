package modeldiscovery

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
)

type Known struct {
	IDs       []string `json:"ids"`
	UpdatedAt string   `json:"updatedAt,omitempty"`
}

type Snapshot struct {
	NewModelPolicy string              `json:"newModelPolicy,omitempty"`
	KnownModels    map[string]Known    `json:"knownModels,omitempty"`
	ShownArrivals  map[string][]string `json:"shownArrivals,omitempty"`
}

func Decode(raw json.RawMessage) Snapshot {
	var snap Snapshot
	if len(raw) == 0 || json.Unmarshal(raw, &snap) != nil {
		return Snapshot{}
	}
	return snap
}

func DecodeRoot(root map[string]json.RawMessage) Snapshot {
	if root == nil {
		return Snapshot{}
	}
	return Decode(root["modelDiscovery"])
}

func (s Snapshot) KnownIDs(provider string) []string {
	if s.KnownModels == nil {
		return nil
	}
	return uniqueSorted(s.KnownModels[provider].IDs)
}

func (s Snapshot) Shown(provider string) []string {
	if s.ShownArrivals == nil {
		return nil
	}
	return uniqueSorted(s.ShownArrivals[provider])
}

func DecodeConfig(root json.RawMessage) Snapshot {
	var envelope struct {
		ModelDiscovery json.RawMessage `json:"modelDiscovery"`
	}
	if json.Unmarshal(root, &envelope) != nil {
		return Snapshot{}
	}
	return Decode(envelope.ModelDiscovery)
}

func ShownForProvider(root json.RawMessage, provider string) []string {
	return DecodeConfig(root).Shown(provider)
}

func CatalogID(provider, id string) string {
	provider = strings.TrimSpace(provider)
	id = strings.TrimSpace(id)
	if provider == "" || id == "" {
		return id
	}
	prefix := provider + "/"
	if strings.HasPrefix(id, prefix) {
		return id
	}
	return prefix + id
}

type ProviderCatalog struct {
	ID             string
	Skip           bool
	PolicyOverride string
	Selected       []string
	Custom         []string
	Discovered     []string
	Incomplete     bool
}

func DisabledFromConfig(root json.RawMessage) []string {
	var envelope struct {
		DisabledModels []string `json:"disabledModels"`
	}
	if json.Unmarshal(root, &envelope) != nil {
		return nil
	}
	return uniqueSorted(envelope.DisabledModels)
}

func CatalogsFromConfig(providers map[string]json.RawMessage, root json.RawMessage, snap Snapshot) []ProviderCatalog {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	customByProvider := customIDsByProvider(root)
	out := make([]ProviderCatalog, 0, len(names))
	for _, name := range names {
		raw := providers[name]
		auth := strings.TrimSpace(providerFieldString(raw, "authMode"))
		rec := ProviderCatalog{
			ID:             name,
			Skip:           strings.EqualFold(auth, "forward"),
			PolicyOverride: providerFieldString(raw, "newModelPolicy"),
			Selected:       providerFieldStrings(raw, "selectedModels"),
			Custom:         customByProvider[name],
		}
		ids := providerFieldStrings(raw, "models")
		if def := providerFieldString(raw, "defaultModel"); def != "" {
			ids = append(ids, def)
		}
		ids = append(ids, snap.Shown(name)...)
		rec.Discovered = ids
		out = append(out, rec)
	}
	return out
}

func customIDsByProvider(raw json.RawMessage) map[string][]string {
	var root struct {
		CustomModels []struct {
			Provider string `json:"provider"`
			ModelID  string `json:"modelId"`
		} `json:"customModels"`
	}
	_ = json.Unmarshal(raw, &root)
	out := map[string][]string{}
	for _, row := range root.CustomModels {
		if row.Provider == "" || row.ModelID == "" {
			continue
		}
		out[row.Provider] = append(out[row.Provider], row.ModelID)
	}
	return out
}

func providerFieldString(raw json.RawMessage, key string) string {
	var rec map[string]any
	if json.Unmarshal(raw, &rec) != nil {
		return ""
	}
	value, _ := rec[key].(string)
	return strings.TrimSpace(value)
}

func providerFieldStrings(raw json.RawMessage, key string) []string {
	var rec map[string]any
	if json.Unmarshal(raw, &rec) != nil {
		return nil
	}
	list, _ := rec[key].([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, _ := item.(string)
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func Reconcile(snap Snapshot, providers []ProviderCatalog, disabled []string, now time.Time) (Snapshot, []string, bool) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	stamp := now.Format(time.RFC3339)
	next := Snapshot{
		NewModelPolicy: strings.TrimSpace(snap.NewModelPolicy),
		KnownModels:    map[string]Known{},
		ShownArrivals:  map[string][]string{},
	}
	for name, known := range snap.KnownModels {
		next.KnownModels[name] = Known{IDs: uniqueSorted(known.IDs), UpdatedAt: known.UpdatedAt}
	}
	for name, shown := range snap.ShownArrivals {
		next.ShownArrivals[name] = uniqueSorted(shown)
	}
	disabledOut := uniqueSorted(disabled)
	changed := false
	for _, provider := range providers {
		if provider.Skip {
			continue
		}
		result := Apply(Input{
			Policy:         Resolve(next.NewModelPolicy, provider.PolicyOverride),
			Incomplete:     provider.Incomplete,
			Discovered:     provider.Discovered,
			Baseline:       next.KnownIDs(provider.ID),
			Disabled:       disabledOut,
			Show:           next.Shown(provider.ID),
			SelectedModels: provider.Selected,
			CustomIDs:      provider.Custom,
		})
		if !result.Changed {
			continue
		}
		changed = true
		disabledOut = result.Disabled
		next.KnownModels[provider.ID] = Known{IDs: result.Baseline, UpdatedAt: stamp}
		if len(result.Show) == 0 {
			delete(next.ShownArrivals, provider.ID)
		} else {
			next.ShownArrivals[provider.ID] = result.Show
		}
	}
	if len(next.KnownModels) == 0 {
		next.KnownModels = nil
	}
	if len(next.ShownArrivals) == 0 {
		next.ShownArrivals = nil
	}
	return next, disabledOut, changed
}
