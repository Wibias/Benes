package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/providerregistry"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
	"github.com/Wibias/Benes/internal/quota"
)

const bootstrapConfigGeneration uint64 = 1

type CodexPoolOptions struct {
	BenesHome           string
	CodexHome           string
	ResolveCodexHome    func() (string, error)
	HTTPClient          *http.Client
	ManagedQuotaFetcher codexauth.ManagedQuotaFetcher
	MainQuotaFetcher    codexauth.MainQuotaFetcher
	Now                 func() time.Time
}

type codexPoolSnapshotSource struct {
	accounts   codexauth.ManagedAccountConfig
	policy     codexauth.PoolRoutingPolicy
	store      *codexauth.ManagedCredentialStore
	main       *codexauth.MainCredentialSource
	quotas     *codexauth.QuotaState
	mainQuotas *codexauth.MainQuotaState
	now        func() time.Time
}

type codexPoolRuntime struct {
	authorities map[string]openairesponses.ForwardCredentialAuthority
	primer      *codexauth.ManagedQuotaPrimer
	mainPrimer  *codexauth.MainQuotaPrimer
	store       *codexauth.ManagedCredentialStore
	tokens      *codexauth.ManagedTokenSource
	quotas      *codexauth.QuotaState
	reauth      *codexauth.ReauthState
	main        *codexauth.MainCredentialSource
	mainQuotas  *codexauth.MainQuotaState
	health      *codexauth.HealthState
	now         func() time.Time
	scheduler   *codexQuotaPrimeScheduler
}

func (r codexPoolRuntime) prime(ctx context.Context) {
	runCodexQuotaPrimers(ctx, r.primer, r.mainPrimer)
}

func buildCodexPoolAuthorities(
	ctx context.Context,
	disk config.DiskConfig,
	specs []providerregistry.Spec,
	options CodexPoolOptions,
) (map[string]openairesponses.ForwardCredentialAuthority, error) {
	runtime, err := buildCodexPoolRuntime(ctx, disk, specs, options)
	if err != nil {
		return nil, err
	}
	return runtime.authorities, nil
}

func buildCodexPoolRuntime(
	ctx context.Context,
	disk config.DiskConfig,
	specs []providerregistry.Spec,
	options CodexPoolOptions,
) (codexPoolRuntime, error) {
	authoritySpecs := codexStoredAuthoritySpecs(specs, len(disk.CodexAccountNamespaces) > 0)
	if len(authoritySpecs) == 0 {
		return codexPoolRuntime{}, nil
	}
	if err := ctx.Err(); err != nil {
		return codexPoolRuntime{}, err
	}

	accounts, err := codexauth.ProjectManagedAccountConfig(disk.Raw)
	if err != nil {
		return codexPoolRuntime{}, fmt.Errorf("project Codex Pool accounts: %w", err)
	}
	policy, err := codexauth.ProjectPoolRoutingPolicy(disk.Raw)
	if err != nil {
		return codexPoolRuntime{}, fmt.Errorf("project Codex Pool routing policy: %w", err)
	}
	store, err := codexauth.NewManagedCredentialStore(options.BenesHome)
	if err != nil {
		return codexPoolRuntime{}, fmt.Errorf("configure Codex Pool credential store: %w", err)
	}
	initialCredentials := store.Read()
	if err := validateManagedCredentialSnapshot(initialCredentials); err != nil {
		return codexPoolRuntime{}, err
	}

	codexHome := strings.TrimSpace(options.CodexHome)
	if codexHome == "" {
		resolver := options.ResolveCodexHome
		if resolver == nil {
			resolver = func() (string, error) {
				return config.ResolveCodexHome(config.CodexHomeOptions{})
			}
		}
		codexHome, err = resolver()
		if err != nil {
			return codexPoolRuntime{}, fmt.Errorf("resolve Codex home for Pool: %w", err)
		}
	}
	if !filepath.IsAbs(codexHome) {
		return codexPoolRuntime{}, fmt.Errorf("configure Codex Pool main credential: absolute Codex home is required")
	}
	main, err := codexauth.NewMainCredentialSource(codexHome)
	if err != nil {
		return codexPoolRuntime{}, fmt.Errorf("configure Codex Pool main credential: %w", err)
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}
	tokens, err := codexauth.NewManagedTokenSource(codexauth.ManagedTokenSourceConfig{
		Store:      store,
		HTTPClient: options.HTTPClient,
		Now:        now,
	})
	if err != nil {
		return codexPoolRuntime{}, fmt.Errorf("configure Codex Pool token source: %w", err)
	}

	reauth := codexauth.NewReauthState()
	health := codexauth.NewHealthState()
	failures := codexauth.NewFailureState()
	affinity := codexauth.NewThreadAffinityState()
	rotation := codexauth.NewRotationState()
	mainFence := codexauth.NewMainPhysicalFence(health, failures, reauth, affinity)
	selector := codexauth.NewPoolSelector(codexauth.PoolSelectorDependencies{
		Reauth: reauth, Health: health, Failures: failures, Affinity: affinity, Rotation: rotation, MainFence: mainFence,
	})
	outcomes := codexauth.NewOutcomeRecorder(codexauth.OutcomeRecorderDependencies{
		Reauth: reauth, Health: health, Failures: failures, Affinity: affinity, Rotation: rotation, MainFence: mainFence,
	})
	liveAccountIDs := liveCodexPoolAccountIDs(accounts)
	outcomes.Reconcile(bootstrapConfigGeneration, liveAccountIDs)
	quotas := codexauth.NewQuotaState()
	quotas.Reconcile(bootstrapConfigGeneration, liveAccountIDs)
	mainQuotas := codexauth.NewMainQuotaState()

	managedQuotaFetcher := options.ManagedQuotaFetcher
	mainQuotaFetcher := options.MainQuotaFetcher
	if managedQuotaFetcher == nil && mainQuotaFetcher == nil {
		shared := newDeferredManagedQuotaFetcher()
		managedQuotaFetcher = shared
		mainQuotaFetcher = shared
	} else {
		if managedQuotaFetcher == nil {
			managedQuotaFetcher = newDeferredManagedQuotaFetcher()
		}
		if mainQuotaFetcher == nil {
			if shared, ok := managedQuotaFetcher.(codexauth.MainQuotaFetcher); ok {
				mainQuotaFetcher = shared
			} else {
				mainQuotaFetcher = newDeferredManagedQuotaFetcher()
			}
		}
	}
	primer, err := codexauth.NewManagedQuotaPrimer(codexauth.ManagedQuotaPrimerConfig{
		Accounts:         accounts,
		Credentials:      store,
		Tokens:           tokens,
		Fetcher:          managedQuotaFetcher,
		Quotas:           quotas,
		Reauth:           reauth,
		WriterGeneration: bootstrapConfigGeneration,
		Now:              now,
	})
	if err != nil {
		return codexPoolRuntime{}, fmt.Errorf("configure Codex Pool quota primer: %w", err)
	}
	mainPrimer, err := codexauth.NewMainQuotaPrimer(codexauth.MainQuotaPrimerConfig{
		Credentials:      main,
		Fetcher:          mainQuotaFetcher,
		State:            mainQuotas,
		Reauth:           reauth,
		WriterGeneration: bootstrapConfigGeneration,
		ConfiguredPlan:   configuredMainPlan(accounts),
		Now:              now,
	})
	if err != nil {
		return codexPoolRuntime{}, fmt.Errorf("configure Codex Pool main quota primer: %w", err)
	}
	quotaRecorder, err := codexauth.NewPoolQuotaRecorder(codexauth.PoolQuotaRecorderConfig{
		Quotas: quotas, MainQuota: mainPrimer, Now: now,
	})
	if err != nil {
		return codexPoolRuntime{}, fmt.Errorf("configure Codex Pool live quota recorder: %w", err)
	}
	scheduler := newCodexQuotaPrimeScheduler(ctx, primer, mainPrimer)

	resolver, err := codexauth.NewPoolCredentialResolver(codexauth.PoolCredentialResolverConfig{
		Selector: selector, ManagedTokens: tokens, ManagedCredentials: store, MainCredentials: main,
		OnSelectedQuotaMissing: func(string) {
			scheduler.Trigger()
		},
	})
	if err != nil {
		return codexPoolRuntime{}, fmt.Errorf("configure Codex Pool credential resolver: %w", err)
	}
	snapshot := &codexPoolSnapshotSource{
		accounts:   accounts,
		policy:     policy,
		store:      store,
		main:       main,
		quotas:     quotas,
		mainQuotas: mainQuotas,
		now:        now,
	}
	poolAuthority, err := openairesponses.NewCodexPoolAuthorityWithOutcomesAndQuota(snapshot, resolver, outcomes, quotaRecorder)
	if err != nil {
		return codexPoolRuntime{}, fmt.Errorf("configure Codex Pool forward authority: %w", err)
	}

	authorities := make(map[string]openairesponses.ForwardCredentialAuthority, len(authoritySpecs))
	for _, spec := range authoritySpecs {
		switch spec.CodexAccountMode {
		case providerregistry.CodexAccountModeDirect:
			authorities[spec.ID] = openairesponses.NewCodexAccountAuthority(openairesponses.DirectForwardAuthority{}, poolAuthority)
		default:
			authorities[spec.ID] = poolAuthority
		}
	}
	return codexPoolRuntime{
		authorities: authorities,
		primer:      primer,
		mainPrimer:  mainPrimer,
		store:       store,
		tokens:      tokens,
		quotas:      quotas,
		reauth:      reauth,
		main:        main,
		mainQuotas:  mainQuotas,
		health:      health,
		now:         now,
		scheduler:   scheduler,
	}, nil
}

func (r codexPoolRuntime) quotaReports(providerIDs []string) []quota.Report {
	if r.main == nil || r.mainQuotas == nil {
		return nil
	}
	now := time.Now()
	if r.now != nil {
		now = r.now()
	}
	cred := r.main.Read(now)
	if cred.Status != codexauth.MainCredentialOK {
		return nil
	}
	snap := r.mainQuotas.Snapshot(cred.Identity)
	if snap.Quota == nil {
		return nil
	}
	reading := quotaFromCodexReading(snap.Quota, snap.UpdatedAt)
	if reading.UpdatedAt == 0 {
		return nil
	}
	out := make([]quota.Report, 0, len(providerIDs))
	for _, name := range providerIDs {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out = append(out, quota.Report{
			Provider:  name,
			Label:     name,
			Source:    "chatgpt:wham",
			Quota:     reading,
			UpdatedAt: reading.UpdatedAt,
		})
	}
	return out
}

func quotaFromCodexReading(src *codexauth.QuotaReading, updated time.Time) quota.Quota {
	if src == nil {
		return quota.Quota{}
	}
	out := quota.Quota{UpdatedAt: updated.UnixMilli()}
	if src.ShortPercent != nil {
		out.FiveHourPercent = src.ShortPercent
	}
	if src.ShortResetAt != nil {
		out.FiveHourResetAt = int64(*src.ShortResetAt)
	}
	if src.WeeklyPercent != nil {
		out.WeeklyPercent = src.WeeklyPercent
	}
	if src.WeeklyResetAt != nil {
		out.WeeklyResetAt = int64(*src.WeeklyResetAt)
	}
	if src.MonthlyPercent != nil {
		out.MonthlyPercent = src.MonthlyPercent
	}
	if src.MonthlyResetAt != nil {
		out.MonthlyResetAt = int64(*src.MonthlyResetAt)
	}
	if out.FiveHourPercent == nil && out.WeeklyPercent == nil && out.MonthlyPercent == nil {
		return quota.Quota{}
	}
	return out
}

func (s *codexPoolSnapshotSource) Snapshot(
	ctx context.Context,
	identity openairesponses.CodexPoolDispatchIdentity,
) (codexauth.PoolCredentialRequest, error) {
	if err := ctx.Err(); err != nil {
		return codexauth.PoolCredentialRequest{}, err
	}
	now := s.now()
	credentials := s.store.Read()
	if err := validateManagedCredentialSnapshot(credentials); err != nil {
		return codexauth.PoolCredentialRequest{}, err
	}
	main := s.main.Read(now)
	quotas := map[string]*codexauth.QuotaSnapshot(nil)
	if s.quotas != nil {
		quotas = s.quotas.SelectionSnapshotsForCredentials(credentials)
	}
	mainPlan := configuredMainPlan(s.accounts)
	if main.Status == codexauth.MainCredentialOK && s.mainQuotas != nil {
		mainSnapshot := s.mainQuotas.Snapshot(main.Identity)
		if mainSnapshot.Identity != "" {
			if strings.TrimSpace(mainSnapshot.Plan) != "" {
				mainPlan = mainSnapshot.Plan
			}
			if mainQuota := mainSelectionQuota(mainSnapshot.Quota); mainQuota != nil {
				mainQuota.UpdatedAt = mainSnapshot.UpdatedAt
				if quotas == nil {
					quotas = make(map[string]*codexauth.QuotaSnapshot)
				}
				quotas[codexauth.MainAccountID] = mainQuota
			}
		}
	}
	return codexauth.PoolCredentialRequest{
		Selection: codexauth.PoolSelectionInput{
			Accounts:            s.accounts,
			Credentials:         credentials,
			Main:                main,
			IncludeMain:         true,
			MainPlan:            mainPlan,
			Strategy:            s.policy.Strategy,
			ResetOrder:          s.policy.ResetOrder,
			StickyLimit:         s.policy.StickyLimit,
			AutoSwitchThreshold: s.policy.AutoSwitchThreshold,
			FailoverThreshold:   s.policy.FailoverThreshold,
			Scope:               bootstrapQuotaScope(identity.ModelID),
			Now:                 now,
			Quotas:              quotas,
		},
		WriterGeneration: bootstrapConfigGeneration,
	}, nil
}

func mainSelectionQuota(reading *codexauth.QuotaReading) *codexauth.QuotaSnapshot {
	if reading == nil || (reading.WeeklyPercent == nil && reading.MonthlyPercent == nil && reading.ShortPercent == nil) {
		return nil
	}
	return &codexauth.QuotaSnapshot{
		WeeklyPercent:  copyOptionalFloat(reading.WeeklyPercent),
		MonthlyPercent: copyOptionalFloat(reading.MonthlyPercent),
		ShortPercent:   copyOptionalFloat(reading.ShortPercent),
		WeeklyResetAt:  copyOptionalFloat(reading.WeeklyResetAt),
		MonthlyResetAt: copyOptionalFloat(reading.MonthlyResetAt),
		ShortResetAt:   copyOptionalFloat(reading.ShortResetAt),
	}
}

func copyOptionalFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func codexStoredAuthoritySpecs(specs []providerregistry.Spec, exactNamespaces bool) []providerregistry.Spec {
	selected := make([]providerregistry.Spec, 0, 1)
	for _, spec := range specs {
		if spec.CodexAccountMode == providerregistry.CodexAccountModePool ||
			(exactNamespaces && spec.ID == "openai" && spec.CodexAccountMode == providerregistry.CodexAccountModeDirect) {
			selected = append(selected, spec)
		}
	}
	return selected
}

func poolProviderIDs(specs []providerregistry.Spec) []string {
	ids := make([]string, 0, 1)
	for _, spec := range specs {
		if spec.CodexAccountMode == providerregistry.CodexAccountModePool {
			ids = append(ids, spec.ID)
		}
	}
	return ids
}

func validateManagedCredentialSnapshot(snapshot codexauth.ManagedCredentialSnapshot) error {
	switch snapshot.Status {
	case codexauth.ManagedCredentialStoreOK, codexauth.ManagedCredentialStoreMissing:
		return nil
	case codexauth.ManagedCredentialStoreInvalid:
		return fmt.Errorf("read Codex Pool credential store: %w", codexauth.ErrManagedCredentialStoreInvalid)
	case codexauth.ManagedCredentialStoreUnreadable:
		return fmt.Errorf("read Codex Pool credential store: %w", codexauth.ErrManagedCredentialStoreUnreadable)
	default:
		return fmt.Errorf("read Codex Pool credential store: unknown store status %q", snapshot.Status)
	}
}

func liveCodexPoolAccountIDs(accounts codexauth.ManagedAccountConfig) map[string]struct{} {
	live := make(map[string]struct{}, len(accounts.Accounts)+1)
	live[codexauth.MainAccountID] = struct{}{}
	for _, account := range accounts.Accounts {
		if account.ID != "" {
			live[account.ID] = struct{}{}
		}
	}
	return live
}

func configuredMainPlan(accounts codexauth.ManagedAccountConfig) string {
	for _, account := range accounts.Accounts {
		if account.IsMain || account.ID == codexauth.MainAccountID {
			return account.Plan
		}
	}
	return ""
}

func bootstrapQuotaScope(modelID string) codexauth.QuotaScope {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ""
	}
	if strings.EqualFold(modelID, "gpt-5.3-codex-spark") {
		return codexauth.QuotaScopeSpark
	}
	return codexauth.QuotaScopeShared
}
