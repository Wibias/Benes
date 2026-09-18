package config

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/providerregistry"
)

const (
	LogicalOpenAIID     = "openai"
	OpenAIAPIConnection = "openai-apikey"

	AccessMethodOAuth  = "oauth"
	AccessMethodAPI    = "api"
	AccessMethodAPIKey = "api-key"

	DefaultAccessOAuth = AccessMethodOAuth
	DefaultAccessAPI   = AccessMethodAPI
)

// AccessMethod is one credential/connection lane on a user-facing logical provider.
type AccessMethod struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	ConnectionID      string `json:"connectionId"`
	SupportsMultiple  bool   `json:"supportsMultiple"`
	QuotaAvailable    bool   `json:"quotaAvailable"`
	SelectionOrder    bool   `json:"selectionOrder"`
	PoolSupported     bool   `json:"poolSupported"`
	ConnectionPresent bool   `json:"connectionPresent"`
}

// SelectionCapabilities describes editable credential-pool policy for one method.
type SelectionCapabilities struct {
	Supported         bool     `json:"supported"`
	Mode              string   `json:"mode,omitempty"`
	Strategy          string   `json:"strategy,omitempty"`
	Strategies        []string `json:"strategies,omitempty"`
	AutoSwitchPercent *float64 `json:"autoSwitchThreshold,omitempty"`
	StickyLimit       *int     `json:"stickyLimit,omitempty"`
	ShowAutoSwitch    bool     `json:"showAutoSwitch,omitempty"`
	ShowStickyLimit   bool     `json:"showStickyLimit,omitempty"`
	ResetOrder        *string  `json:"resetOrder,omitempty"`
	MethodID          string   `json:"methodId,omitempty"`
	ThreadAffinity    bool     `json:"-"`
}

// AccessDescriptor is the generic Access-tab contract. The UI renders from this,
// not from provider-id branches.
type AccessDescriptor struct {
	Methods           []AccessMethod         `json:"methods"`
	DefaultMethodID   string                 `json:"defaultMethodId,omitempty"`
	DefaultAccess     bool                   `json:"defaultAccess"`
	Selection         *SelectionCapabilities `json:"selection,omitempty"`
	ActivitySupported bool                   `json:"activitySupported"`
}

// LogicalProvider is the user-facing provider row. Runtime specs stay on ConnectionIDs.
type LogicalProvider struct {
	ID              string
	ConnectionIDs   []string
	HiddenIDs       []string
	Access          AccessDescriptor
	Disabled        bool
	NeedsCredential bool
}

// FoldLogicalProviders maps disk provider records onto user-facing rows.
// Canonical openai (ChatGPT forward) absorbs openai-apikey as a second connection.
// Other ids stay 1:1. Unknown custom providers are never merged.
func FoldLogicalProviders(disk DiskConfig) []LogicalProvider {
	ids := make([]string, 0, len(disk.Providers))
	for id := range disk.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	records := map[string]map[string]json.RawMessage{}
	for id, raw := range disk.Providers {
		rec := map[string]json.RawMessage{}
		if json.Unmarshal(raw, &rec) != nil || rec == nil {
			continue
		}
		records[id] = rec
	}

	hidden := map[string]bool{}
	out := make([]LogicalProvider, 0, len(ids))
	_, hasAPIKey := records[OpenAIAPIConnection]
	if CanonicalOpenAIForward(LogicalOpenAIID, records[LogicalOpenAIID]) {
		hidden[OpenAIAPIConnection] = true
		out = append(out, logicalOpenAI(disk, records, hasAPIKey))
	}

	for _, id := range ids {
		if hidden[id] {
			continue
		}
		if id == LogicalOpenAIID && CanonicalOpenAIForward(id, records[id]) {
			continue
		}
		rec := records[id]
		if rec == nil {
			continue
		}
		out = append(out, logicalFromRecord(id, rec))
	}
	return out
}

func CanonicalOpenAIForward(id string, rec map[string]json.RawMessage) bool {
	if id != LogicalOpenAIID || rec == nil {
		return false
	}
	adapter, _, _ := optionalString(rec, "adapter")
	auth, _, _ := optionalString(rec, "authMode")
	base, _, _ := optionalString(rec, "baseUrl")
	return adapter == string(providerregistry.ProtocolOpenAIResponses) &&
		auth == string(providerregistry.AuthModeForward) &&
		canonicalForwardProviderBaseURL(base)
}

func logicalOpenAI(disk DiskConfig, records map[string]map[string]json.RawMessage, apiPresent bool) LogicalProvider {
	oauth := AccessMethod{
		ID:                AccessMethodOAuth,
		Kind:              AccessMethodOAuth,
		ConnectionID:      LogicalOpenAIID,
		SupportsMultiple:  true,
		QuotaAvailable:    true,
		SelectionOrder:    true,
		PoolSupported:     true,
		ConnectionPresent: true,
	}
	api := AccessMethod{
		ID:                AccessMethodAPI,
		Kind:              AccessMethodAPIKey,
		ConnectionID:      OpenAIAPIConnection,
		SupportsMultiple:  true,
		QuotaAvailable:    false,
		SelectionOrder:    false,
		PoolSupported:     true,
		ConnectionPresent: apiPresent,
	}
	defaultMethod := DefaultAccessOAuth
	codexMode := "pool"
	if rec := records[LogicalOpenAIID]; rec != nil {
		if value, present, ok := optionalString(rec, "defaultAccess"); ok && present {
			switch strings.TrimSpace(value) {
			case DefaultAccessAPI:
				defaultMethod = DefaultAccessAPI
			case DefaultAccessOAuth:
				defaultMethod = DefaultAccessOAuth
			}
		}
		if value, present, ok := optionalString(rec, "codexAccountMode"); ok && present && strings.TrimSpace(value) == "direct" {
			codexMode = "direct"
		}
	}
	disabled, _, _ := optionalBool(records[LogicalOpenAIID], "disabled")
	connections := []string{LogicalOpenAIID}
	hidden := []string{}
	if apiPresent {
		connections = append(connections, OpenAIAPIConnection)
		hidden = append(hidden, OpenAIAPIConnection)
	}
	policy := poolSelectionFromDisk(disk)
	policy.MethodID = AccessMethodOAuth
	policy.Mode = codexMode
	policy.Supported = true
	return LogicalProvider{
		ID:            LogicalOpenAIID,
		ConnectionIDs: connections,
		HiddenIDs:     hidden,
		Disabled:      disabled,
		Access: AccessDescriptor{
			Methods:           []AccessMethod{oauth, api},
			DefaultMethodID:   defaultMethod,
			DefaultAccess:     true,
			Selection:         policy,
			ActivitySupported: true,
		},
	}
}

func logicalFromRecord(id string, rec map[string]json.RawMessage) LogicalProvider {
	disabled, _, _ := optionalBool(rec, "disabled")
	auth, _, _ := optionalString(rec, "authMode")
	adapter, _, _ := optionalString(rec, "adapter")
	base, _, _ := optionalString(rec, "baseUrl")
	lp := LogicalProvider{
		ID:            id,
		ConnectionIDs: []string{id},
		Disabled:      disabled,
	}
	switch {
	case auth == string(providerregistry.AuthModeForward) || isLocalAdapter(adapter, base):
		lp.Access = AccessDescriptor{ActivitySupported: false}
	case auth == "oauth":
		lp.Access = AccessDescriptor{
			Methods: []AccessMethod{{
				ID: AccessMethodOAuth, Kind: AccessMethodOAuth, ConnectionID: id,
				SupportsMultiple: true, ConnectionPresent: true,
			}},
			DefaultMethodID:   AccessMethodOAuth,
			ActivitySupported: true,
		}
	default:
		hasKey := hasCredentialMaterial(rec)
		lp.NeedsCredential = !hasKey
		lp.Access = AccessDescriptor{
			Methods: []AccessMethod{{
				ID: AccessMethodAPIKey, Kind: AccessMethodAPIKey, ConnectionID: id,
				SupportsMultiple: true, PoolSupported: true, ConnectionPresent: true,
			}},
			DefaultMethodID:   AccessMethodAPIKey,
			ActivitySupported: true,
		}
	}
	return lp
}

func poolSelectionFromDisk(disk DiskConfig) *SelectionCapabilities {
	raw := disk.Raw
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	policy, err := codexauth.ProjectPoolRoutingPolicy(raw)
	if err != nil {
		policy = codexauth.PoolRoutingPolicy{Strategy: codexauth.PoolStrategyQuota}
	}
	if policy.Strategy == "" {
		policy.Strategy = codexauth.PoolStrategyQuota
	}
	cap := &SelectionCapabilities{
		Mode:       "pool",
		Strategy:   string(policy.Strategy),
		Strategies: []string{string(codexauth.PoolStrategyQuota), string(codexauth.PoolStrategyRoundRobin), string(codexauth.PoolStrategyFillFirst), string(codexauth.PoolStrategyResetWindow)},
	}
	switch policy.Strategy {
	case codexauth.PoolStrategyRoundRobin:
		cap.ShowStickyLimit = true
		limit := policy.StickyLimit
		cap.StickyLimit = &limit
	case codexauth.PoolStrategyResetWindow:
		order := string(codexauth.NormalizeResetOrder(string(policy.ResetOrder)))
		cap.ResetOrder = &order
	case codexauth.PoolStrategyQuota:
		cap.ShowAutoSwitch = true
		if policy.AutoSwitchThreshold != nil {
			cap.AutoSwitchPercent = policy.AutoSwitchThreshold
		} else {
			v := codexauth.DefaultAutoSwitchThreshold
			cap.AutoSwitchPercent = &v
		}
	}
	return cap
}

func hasCredentialMaterial(rec map[string]json.RawMessage) bool {
	if _, ok := rec["apiKey"]; ok {
		text, _, _ := optionalString(rec, "apiKey")
		if strings.TrimSpace(text) != "" {
			return true
		}
	}
	if raw, ok := rec["credentialRef"]; ok && len(raw) > 0 && string(raw) != "null" {
		return true
	}
	if raw, ok := rec["apiKeyPool"]; ok && len(raw) > 4 && string(raw) != "null" && string(raw) != "[]" {
		return true
	}
	return false
}

func isLocalAdapter(adapter, baseURL string) bool {
	if adapter == "local" {
		return true
	}
	parsedHost := strings.ToLower(baseURL)
	return strings.Contains(parsedHost, "127.0.0.1") || strings.Contains(parsedHost, "localhost") || strings.Contains(parsedHost, "[::1]")
}

func HiddenConnectionSet(logical []LogicalProvider) map[string]bool {
	hidden := map[string]bool{}
	for _, lp := range logical {
		for _, id := range lp.HiddenIDs {
			hidden[id] = true
		}
	}
	return hidden
}
