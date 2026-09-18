package server

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/config"
	credentialstore "github.com/Wibias/Benes/internal/credentials"
)

type providerWorkspacePoolQuotaState struct {
	Known     bool
	Exhausted bool
	UpdatedAt int64
}

type providerWorkspacePoolReauthState struct {
	Known    bool
	Required bool
}

type providerWorkspaceCredentialState struct {
	Known            bool
	Missing          bool
	PoolPaused       bool
	StoreUnavailable bool
}

// applyWorkspaceQuotaEvidence projects quota onto logical providers. Canonical
// OpenAI is special because the user-facing OAuth lane can represent a pool of
// physical accounts: one exhausted member is not provider-level exhaustion.
func (h *handler) applyWorkspaceQuotaEvidence(
	evidence map[string]providerWorkspaceEvidence,
	logical []config.LogicalProvider,
	disk config.DiskConfig,
) {
	openAI, hasOpenAI := workspaceLogicalProvider(logical, config.LogicalOpenAIID)
	ignoreOpenAIOAuthReport := false
	aggregateOpenAIPool := false
	if hasOpenAI {
		// A provider-level OpenAI quota report describes the OAuth/main-account
		// lane. It must not drive the logical provider when API access is the
		// selected default.
		if openAI.Access.DefaultMethodID == config.DefaultAccessAPI {
			ignoreOpenAIOAuthReport = true
		} else if selection := openAI.Access.Selection; selection != nil && selection.Mode == "pool" {
			ignoreOpenAIOAuthReport = true
			aggregateOpenAIPool = true
		}
	}

	if aggregateOpenAIPool {
		if state := h.openAIWorkspacePoolQuotaState(disk); state.Known {
			if item, ok := evidence[config.LogicalOpenAIID]; ok && (state.UpdatedAt >= item.QuotaAt || !state.Exhausted) {
				item.QuotaExhausted = state.Exhausted
				if state.UpdatedAt > item.QuotaAt {
					item.QuotaAt = state.UpdatedAt
				}
				evidence[config.LogicalOpenAIID] = item
			}
		}
	}

	for _, report := range h.workspaceQuotaReports() {
		if ignoreOpenAIOAuthReport && report.Provider == config.LogicalOpenAIID {
			continue
		}
		provider := logicalIDForConnection(logical, report.Provider)
		item, ok := evidence[provider]
		if !ok || !workspaceQuotaReportApplies(report, provider, disk) {
			continue
		}
		exhausted := providerQuotaExhausted(report.Quota)
		if report.UpdatedAt >= item.QuotaAt || !exhausted {
			item.QuotaExhausted = exhausted
			if report.UpdatedAt > item.QuotaAt {
				item.QuotaAt = report.UpdatedAt
			}
			evidence[provider] = item
		}
	}
}

// applyWorkspaceOpenAIPoolHealthEvidence mirrors the pool selector's current
// account-wide health gates without mutating selector state or consuming probe
// leases. A blocked member is account-level evidence; it becomes provider-level
// attention only when every otherwise-usable OAuth credential is currently
// blocked. Scoped cooldowns remain model-scope evidence and do not mark the
// whole logical provider unavailable.
func (h *handler) applyWorkspaceOpenAIPoolHealthEvidence(
	evidence map[string]providerWorkspaceEvidence,
	logical []config.LogicalProvider,
	disk config.DiskConfig,
) {
	openAI, ok := workspaceLogicalProvider(logical, config.LogicalOpenAIID)
	if !ok || openAI.Access.DefaultMethodID == config.DefaultAccessAPI || h == nil || h.codexAccounts == nil || h.codexHealth == nil {
		return
	}
	selection := openAI.Access.Selection
	if selection == nil || selection.Mode != "pool" {
		return
	}
	item, ok := evidence[config.LogicalOpenAIID]
	if !ok {
		return
	}
	accounts, err := codexauth.ProjectManagedAccountConfig(disk.Raw)
	if err != nil {
		return
	}
	now := h.workspaceCodexNow()
	main := h.workspaceMainCredential(now)
	credentials := h.managedCredentialSnapshot()
	ids := codexauth.StaticEligibleAccountIDs(
		accounts,
		credentials,
		main,
		codexauth.StaticEligibilityOptions{IncludeMain: true},
	)
	if len(ids) == 0 {
		return
	}

	considered := 0
	hardBlocked := 0
	softBlocked := 0
	latestHardAt := int64(0)
	for _, id := range ids {
		if h.codexAccounts.Reauth != nil && h.codexAccounts.Reauth.Needs(id) {
			continue
		}
		considered++
		if cooldown, blocked := h.codexHealth.HardCooldown(id, now); blocked {
			hardBlocked++
			if timestamp := cooldown.Since.UnixMilli(); timestamp > latestHardAt {
				latestHardAt = timestamp
			}
			continue
		}
		if _, blocked := h.codexHealth.SoftAvoidUntil(id, now); blocked {
			softBlocked++
			continue
		}
		item.HealthFailed = false
		if state := h.openAIWorkspacePoolQuotaState(disk); !state.Known || !state.Exhausted {
			item.QuotaExhausted = false
		}
		evidence[config.LogicalOpenAIID] = item
		return
	}
	if considered == 0 || hardBlocked+softBlocked != considered {
		return
	}
	if hardBlocked > 0 {
		item.QuotaExhausted = true
		if latestHardAt > item.QuotaAt {
			item.QuotaAt = latestHardAt
		}
	}
	if softBlocked > 0 {
		item.HealthFailed = true
	}
	evidence[config.LogicalOpenAIID] = item
}

// applyWorkspaceReauthEvidence treats OpenAI OAuth pool reauthentication as an
// account-level problem while another eligible account remains usable. Direct
// mode uses caller Authorization for normal requests and therefore does not let
// stored OAuth reauthentication drive the logical provider lifecycle. API-
// default OpenAI likewise ignores stored OAuth reauthentication.
func (h *handler) applyWorkspaceReauthEvidence(
	evidence map[string]providerWorkspaceEvidence,
	logical []config.LogicalProvider,
	disk config.DiskConfig,
) {
	openAI, ok := workspaceLogicalProvider(logical, config.LogicalOpenAIID)
	if !ok {
		return
	}
	item, ok := evidence[config.LogicalOpenAIID]
	if !ok {
		return
	}
	if openAI.Access.DefaultMethodID == config.DefaultAccessAPI {
		item.ReauthRequired = false
		item.ReauthAt = 0
		evidence[config.LogicalOpenAIID] = item
		return
	}

	if selection := openAI.Access.Selection; selection != nil {
		if selection.Mode == "direct" {
			item.ReauthRequired = false
			item.ReauthAt = 0
			evidence[config.LogicalOpenAIID] = item
			return
		}
		if selection.Mode == "pool" {
			state := h.openAIWorkspacePoolReauthState(disk)
			if !state.Known {
				return
			}
			item.ReauthRequired = state.Required
			if !state.Required {
				item.ReauthAt = 0
			}
			evidence[config.LogicalOpenAIID] = item
			return
		}
	}

	if h == nil || h.codexAccounts == nil || h.codexAccounts.Reauth == nil {
		return
	}
	item.ReauthRequired = h.codexAccounts.Reauth.Needs(codexauth.MainAccountID)
	if !item.ReauthRequired {
		item.ReauthAt = 0
	}
	evidence[config.LogicalOpenAIID] = item
}

// applyWorkspaceCredentialEvidence verifies the currently selected OpenAI
// access lane against physical credential state. Alternate lanes are not
// treated as silent rescue paths because runtime does not cross-failover after
// an authentication failure on the selected lane. Direct mode is caller-auth
// and intentionally has no stored-credential prerequisite.
func (h *handler) applyWorkspaceCredentialEvidence(
	evidence map[string]providerWorkspaceEvidence,
	logical []config.LogicalProvider,
	disk config.DiskConfig,
) {
	openAI, ok := workspaceLogicalProvider(logical, config.LogicalOpenAIID)
	if !ok {
		return
	}
	item, ok := evidence[config.LogicalOpenAIID]
	if !ok {
		return
	}
	state := providerWorkspaceCredentialState{}
	if openAI.Access.DefaultMethodID == config.DefaultAccessAPI {
		state = h.openAIWorkspaceAPICredentialState(disk)
	} else if selection := openAI.Access.Selection; selection != nil && selection.Mode == "direct" {
		item.CredentialMissing = false
		item.CredentialStoreUnavailable = false
		item.CredentialPoolPaused = false
		evidence[config.LogicalOpenAIID] = item
		return
	} else {
		state = h.openAIWorkspaceOAuthCredentialState(disk)
	}
	if !state.Known {
		return
	}
	item.CredentialMissing = state.Missing
	item.CredentialStoreUnavailable = state.StoreUnavailable
	item.CredentialPoolPaused = state.PoolPaused
	evidence[config.LogicalOpenAIID] = item
}

func workspaceLogicalProvider(logical []config.LogicalProvider, id string) (config.LogicalProvider, bool) {
	for _, provider := range logical {
		if provider.ID == id {
			return provider, true
		}
	}
	return config.LogicalProvider{}, false
}

// openAIWorkspacePoolQuotaState reports provider-level exhaustion only when the
// whole currently eligible OAuth pool is known exhausted. Unknown quota for any
// remaining eligible credential keeps the aggregate unknown; a single known
// credential with headroom is sufficient to prove the pool is not exhausted.
func (h *handler) openAIWorkspacePoolQuotaState(disk config.DiskConfig) providerWorkspacePoolQuotaState {
	if h == nil || h.codexAccounts == nil {
		return providerWorkspacePoolQuotaState{}
	}
	accounts, err := codexauth.ProjectManagedAccountConfig(disk.Raw)
	if err != nil {
		return providerWorkspacePoolQuotaState{}
	}

	now := h.workspaceCodexNow()
	main := h.workspaceMainCredential(now)
	credentials := h.managedCredentialSnapshot()
	ids := codexauth.StaticEligibleAccountIDs(
		accounts,
		credentials,
		main,
		codexauth.StaticEligibilityOptions{IncludeMain: true},
	)
	if len(ids) == 0 {
		return providerWorkspacePoolQuotaState{}
	}

	plans := make(map[string]string, len(accounts.Accounts))
	for _, account := range accounts.Accounts {
		if codexauth.IsSelectableManagedAccount(account) {
			plans[account.ID] = account.Plan
		}
	}

	candidates := 0
	unknown := false
	latest := int64(0)
	for _, id := range ids {
		if h.codexAccounts.Reauth != nil && h.codexAccounts.Reauth.Needs(id) {
			continue
		}
		candidates++

		var snapshot *codexauth.QuotaSnapshot
		var updatedAt time.Time
		plan := plans[id]
		if id == codexauth.MainAccountID {
			if h.codexAccounts.MainQuotas == nil || main.Status != codexauth.MainCredentialOK {
				unknown = true
				continue
			}
			mainQuota := h.codexAccounts.MainQuotas.Snapshot(main.Identity)
			if mainQuota.Quota == nil {
				unknown = true
				continue
			}
			plan = mainQuota.Plan
			snapshot = workspaceQuotaSnapshot(mainQuota.Quota)
			updatedAt = mainQuota.UpdatedAt
		} else {
			record, ok := credentials.Records[id]
			if !ok || record.Credential == nil || record.DeletedAtMS != nil || h.codexAccounts.Quotas == nil {
				unknown = true
				continue
			}
			stored := h.codexAccounts.Quotas.GetForCredential(id, record.Generation)
			if stored == nil {
				unknown = true
				continue
			}
			snapshot = workspaceQuotaSnapshot(&stored.QuotaReading)
			updatedAt = stored.UpdatedAt
		}

		usage := codexauth.ComputeQuotaUsageScore(snapshot, plan)
		if usage == codexauth.UnknownUsageScore {
			unknown = true
			continue
		}
		if millis := updatedAt.UnixMilli(); millis > latest {
			latest = millis
		}
		if usage < codexauth.ExhaustedUsagePercent {
			return providerWorkspacePoolQuotaState{Known: true, Exhausted: false, UpdatedAt: latest}
		}
	}

	if candidates == 0 || unknown {
		return providerWorkspacePoolQuotaState{}
	}
	return providerWorkspacePoolQuotaState{Known: true, Exhausted: true, UpdatedAt: latest}
}

func (h *handler) openAIWorkspacePoolReauthState(disk config.DiskConfig) providerWorkspacePoolReauthState {
	if h == nil || h.codexAccounts == nil || h.codexAccounts.Reauth == nil {
		return providerWorkspacePoolReauthState{}
	}
	accounts, err := codexauth.ProjectManagedAccountConfig(disk.Raw)
	if err != nil {
		return providerWorkspacePoolReauthState{}
	}
	main := h.workspaceMainCredential(h.workspaceCodexNow())
	credentials := h.managedCredentialSnapshot()
	ids := codexauth.StaticEligibleAccountIDs(
		accounts,
		credentials,
		main,
		codexauth.StaticEligibilityOptions{IncludeMain: true},
	)
	if len(ids) == 0 {
		if credentials.Status == codexauth.ManagedCredentialStoreInvalid || credentials.Status == codexauth.ManagedCredentialStoreUnreadable {
			return providerWorkspacePoolReauthState{}
		}
		switch main.Status {
		case codexauth.MainCredentialExpired, codexauth.MainCredentialInvalid:
			return providerWorkspacePoolReauthState{Known: true, Required: true}
		default:
			return providerWorkspacePoolReauthState{}
		}
	}
	for _, id := range ids {
		if !h.codexAccounts.Reauth.Needs(id) {
			return providerWorkspacePoolReauthState{Known: true, Required: false}
		}
	}
	return providerWorkspacePoolReauthState{Known: true, Required: true}
}

func (h *handler) openAIWorkspaceOAuthCredentialState(disk config.DiskConfig) providerWorkspaceCredentialState {
	if h == nil || h.codexAccounts == nil {
		return providerWorkspaceCredentialState{}
	}
	accounts, err := codexauth.ProjectManagedAccountConfig(disk.Raw)
	if err != nil {
		return providerWorkspaceCredentialState{}
	}
	main := h.workspaceMainCredential(h.workspaceCodexNow())
	credentials := h.managedCredentialSnapshot()
	if credentials.Status == codexauth.ManagedCredentialStoreInvalid || credentials.Status == codexauth.ManagedCredentialStoreUnreadable {
		return providerWorkspaceCredentialState{Known: true, StoreUnavailable: true}
	}
	ids := codexauth.StaticEligibleAccountIDs(
		accounts,
		credentials,
		main,
		codexauth.StaticEligibilityOptions{IncludeMain: true},
	)
	if len(ids) > 0 {
		return providerWorkspaceCredentialState{Known: true}
	}

	physicalCredential := false
	unpausedPhysicalCredential := false
	if main.Status != codexauth.MainCredentialMissing {
		physicalCredential = true
		if !accounts.PausedAccountIDs[codexauth.MainAccountID] {
			unpausedPhysicalCredential = true
		}
	}
	for _, account := range accounts.Accounts {
		if !codexauth.IsSelectableManagedAccount(account) {
			continue
		}
		record, ok := credentials.Records[account.ID]
		if !ok || record.Credential == nil || record.DeletedAtMS != nil {
			continue
		}
		physicalCredential = true
		if !accounts.PausedAccountIDs[account.ID] {
			unpausedPhysicalCredential = true
		}
	}
	return providerWorkspaceCredentialState{
		Known:      true,
		Missing:    !physicalCredential,
		PoolPaused: physicalCredential && !unpausedPhysicalCredential,
	}
}

func (h *handler) openAIWorkspaceAPICredentialState(disk config.DiskConfig) providerWorkspaceCredentialState {
	raw, ok := disk.Providers[config.OpenAIAPIConnection]
	if !ok {
		return providerWorkspaceCredentialState{Known: true, Missing: true}
	}
	var record struct {
		APIKey        string              `json:"apiKey"`
		CredentialRef credentialstore.Ref `json:"credentialRef"`
	}
	if json.Unmarshal(raw, &record) != nil {
		return providerWorkspaceCredentialState{}
	}
	if strings.TrimSpace(record.APIKey) != "" {
		return providerWorkspaceCredentialState{Known: true}
	}
	ref := record.CredentialRef
	if strings.TrimSpace(ref.ID) == "" && strings.TrimSpace(string(ref.Source)) == "" && strings.TrimSpace(ref.Hint) == "" {
		return providerWorkspaceCredentialState{Known: true, Missing: true}
	}
	if h == nil || h.credentials == nil {
		return providerWorkspaceCredentialState{}
	}
	if ref.Source == credentialstore.SourceSecureStore || ref.Source == "" {
		if lister, ok := h.credentials.(credentialLister); ok && lister != nil {
			refs, err := lister.List()
			if err != nil {
				return providerWorkspaceCredentialState{}
			}
			found := false
			for _, candidate := range refs {
				if candidate.ID == ref.ID {
					found = true
					break
				}
			}
			if !found {
				return providerWorkspaceCredentialState{Known: true, Missing: true}
			}
		}
	}
	secret, err := h.credentials.Get(ref)
	if err != nil || strings.TrimSpace(string(secret)) == "" {
		return providerWorkspaceCredentialState{Known: true, Missing: true}
	}
	return providerWorkspaceCredentialState{Known: true}
}

func (h *handler) workspaceCodexNow() time.Time {
	if h != nil && h.codexAccounts != nil && h.codexAccounts.Now != nil {
		return h.codexAccounts.Now()
	}
	return time.Now()
}

func (h *handler) workspaceMainCredential(now time.Time) codexauth.MainCredentialResult {
	if h == nil || h.codexAccounts == nil || h.codexAccounts.Main == nil {
		return codexauth.MainCredentialResult{Status: codexauth.MainCredentialMissing}
	}
	return h.codexAccounts.Main.Read(now)
}

func workspaceQuotaSnapshot(reading *codexauth.QuotaReading) *codexauth.QuotaSnapshot {
	if reading == nil {
		return nil
	}
	return &codexauth.QuotaSnapshot{
		WeeklyPercent:  reading.WeeklyPercent,
		MonthlyPercent: reading.MonthlyPercent,
		ShortPercent:   reading.ShortPercent,
	}
}
