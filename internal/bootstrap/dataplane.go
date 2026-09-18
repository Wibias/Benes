package bootstrap

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentialpool"
	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/modeldiscovery"
	"github.com/Wibias/Benes/internal/platform"
	"github.com/Wibias/Benes/internal/providerregistry"
	"github.com/Wibias/Benes/internal/providers/antigravity"
	"github.com/Wibias/Benes/internal/providers/kiro"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
	"github.com/Wibias/Benes/internal/providers/xaicapability"
	"github.com/Wibias/Benes/internal/quota"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
	"github.com/Wibias/Benes/internal/server"
	"github.com/Wibias/Benes/internal/sessions"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
	"github.com/Wibias/Benes/internal/timeline"
	"github.com/Wibias/Benes/internal/transport"
)

type DataPlaneOptions struct {
	Registry               providerregistry.Options
	CodexPool              CodexPoolOptions
	MaxRequestBytes        int64
	RuntimeDataPlaneTokens []string
	ConfigPath             string
}

type DataPlane struct {
	Handler      http.Handler
	Listener     config.ListenerConfig
	Skipped      []config.ProviderProjectionSkip
	Policy       *catalog.PolicyStore
	Credentials  credentials.Store
	Platform     *platform.Probe
	bindReady    bool
	sessionStore *sessions.Store
	startStorage func(context.Context)
}

func BuildDataPlane(ctx context.Context, disk config.DiskConfig, options DataPlaneOptions) (DataPlane, error) {
	listener, err := config.ProjectListener(disk)
	if err != nil {
		return DataPlane{}, fmt.Errorf("project listener config: %w", err)
	}
	tokens := append([]string(nil), disk.DataPlaneTokens...)
	tokens = append(tokens, options.RuntimeDataPlaneTokens...)
	corsAllowOrigins := append([]string(nil), disk.CORSAllowOrigins...)
	admission := server.DataPlaneAdmissionPolicy{
		BindHostname:      listener.Hostname,
		DataPlaneTokens:   tokens,
		CORSAllowOrigins:  corsAllowOrigins,
		CORSDefaultOrigin: fmt.Sprintf("http://localhost:%d", listener.Port),
	}
	if err := server.ValidateDataPlaneAdmissionPolicy(admission); err != nil {
		return DataPlane{Listener: listener}, fmt.Errorf("validate data-plane admission: %w", err)
	}

	projection := config.ProjectProviderSpecs(disk)
	plane := DataPlane{
		Listener: listener,
		Skipped:  append([]config.ProviderProjectionSkip(nil), projection.Skipped...),
		Platform: platform.NewProbe(),
	}
	plane.Platform.Refresh(ctx)
	if len(projection.Specs) == 0 {
		return plane, fmt.Errorf("no migrated providers are available")
	}
	catalogModels := projectCatalogModels(disk, projection.Specs)
	for i, spec := range projection.Specs {
		for _, model := range catalogModels {
			if spec.ID != "" && (model.ID == spec.ID || strings.HasPrefix(model.ID, spec.ID+"/")) {
				projection.Specs[i].CatalogModels = append(projection.Specs[i].CatalogModels, model.ID)
			}
		}
	}

	attachAntigravityAccounts(options.CodexPool.BenesHome, options.CodexPool.CodexHome, projection.Specs)
	attachXAIAccount(options.CodexPool.BenesHome, projection.Specs)
	attachKiroAccount(options.CodexPool.BenesHome, projection.Specs)
	projection.Specs, projection.Skipped = dropUnresolvedXAIOAuth(projection.Specs, projection.Skipped)
	projection.Specs, projection.Skipped = dropUnresolvedKiroOAuth(projection.Specs, projection.Skipped)
	plane.Skipped = append([]config.ProviderProjectionSkip(nil), projection.Skipped...)
	if len(projection.Specs) == 0 {
		return plane, fmt.Errorf("no migrated providers are available")
	}

	poolRuntime, err := buildCodexPoolRuntime(ctx, disk, projection.Specs, options.CodexPool)
	if err != nil {
		return plane, fmt.Errorf("build Codex Pool runtime: %w", err)
	}
	registryOptions := cloneRegistryOptions(options.Registry)
	if registryOptions.TransportOptions.Pacer == nil {
		registryOptions.TransportOptions.Pacer = transport.NewPacer(0, nil)
	}
	if registryOptions.TransportOptions.Proxy == nil {
		policy := transport.PolicyFromConfig(disk.Proxy, disk.NoProxy)
		registryOptions.TransportOptions.Proxy = policy.Func()
	}
	registryOptions.TransportOptions.AllowDirectFallback = disk.ProxyDirectFallback
	if credDir := credentialStoreDir(options.CodexPool.BenesHome); credDir != "" {
		store, err := credentials.NewFileStore(credDir, true)
		if err != nil {
			return plane, fmt.Errorf("build credential store: %w", err)
		}
		plane.Credentials = store
		if registryOptions.Credentials == nil {
			registryOptions.Credentials = store
		}
		refs, err := adoptPlaintextProviderKeys(store, disk.Providers)
		if err != nil {
			return plane, fmt.Errorf("adopt plaintext credentials: %w", err)
		}
		if err := publishAdoptedCredentialRefs(options.ConfigPath, refs); err != nil {
			return plane, fmt.Errorf("publish credential refs: %w", err)
		}
	}
	if registryOptions.Continuation == nil {
		authority, err := buildContinuationAuthority(options.CodexPool.BenesHome)
		if err != nil {
			return plane, fmt.Errorf("build continuation authority: %w", err)
		}
		registryOptions.Continuation = authority
	}
	if len(poolRuntime.authorities) > 0 {
		if registryOptions.ForwardAuthorities == nil {
			registryOptions.ForwardAuthorities = make(map[string]openairesponses.ForwardCredentialAuthority, len(poolRuntime.authorities))
		}
		for id, authority := range poolRuntime.authorities {
			registryOptions.ForwardAuthorities[id] = authority
		}
	}

	providers, err := providerregistry.Build(ctx, projection.Specs, registryOptions)
	if err != nil {
		return plane, fmt.Errorf("build provider registry: %w", err)
	}
	combos := projectServerCombos(config.ProjectCombos(disk), projection.Specs)

	if strings.TrimSpace(options.ConfigPath) != "" {
		plane.Policy = &catalog.PolicyStore{Transactions: config.NewTransactionStore(options.ConfigPath, options.MaxRequestBytes)}
	}
	sessionStore, sessionStoreErr := newSessionStore(options.CodexPool.BenesHome)
	plane.sessionStore = sessionStore
	handler, err := server.NewHandler(server.Options{
		AdmissionPolicy:        &admission,
		Providers:              providers,
		CodexAccountNamespaces: disk.CodexAccountNamespaces,
		MaxRequestBytes:        options.MaxRequestBytes,
		ResourceBudget:         resourcebudget.NewManager(resourcebudget.Limits{}),
		CatalogModels:          catalogModels,
		Combos:                 combos,
		Aliases:                config.ProjectRouteAliases(disk),
		Timeline:               newRequestTimelineStore(options.CodexPool.BenesHome),
		CatalogErrors:          catalogErrorsFromSkipped(plane.Skipped),
		WebSearch:              webSearchConfigs(projection.Specs, disk.WebSearchMaxSearches, disk.ProxyDirectFallback),
		Credentials:            plane.Credentials,
		DashboardDir:           resolveDashboardDir(),
		ConfigPath:             options.ConfigPath,
		CodexHome:              options.CodexPool.CodexHome,
		UsageLogPath:           usageLogPath(options.CodexPool.BenesHome),
		AuthStorePath:          filepath.Join(options.CodexPool.BenesHome, "auth.json"),
		CodexQuota:             func() []quota.Report { return poolRuntime.quotaReports(codexQuotaProviderIDs(projection.Specs)) },
		CodexHealth:            poolRuntime.health,
		CodexAccounts: &server.CodexAccountRuntime{
			Store: poolRuntime.store, Quotas: poolRuntime.quotas, Main: poolRuntime.main,
			MainQuotas: poolRuntime.mainQuotas, Reauth: poolRuntime.reauth, Tokens: poolRuntime.tokens,
			Prime: func(ctx context.Context) { poolRuntime.prime(ctx) }, Now: poolRuntime.now,
		},
		Sessions:            sessionStore,
		SessionsUnavailable: sessionStoreErr != nil,
	})
	if err != nil {
		return plane, fmt.Errorf("build data-plane handler: %w", err)
	}
	if starter, ok := handler.(interface{ StartStorageCleanup(context.Context) }); ok {
		plane.startStorage = starter.StartStorageCleanup
	}
	plane.Handler = server.WithProviderPacingRuntime(handler, registryOptions.PacingRuntime, options.ConfigPath)
	plane.bindReady = true
	if poolRuntime.scheduler != nil {
		poolRuntime.scheduler.Trigger()
	}
	return plane, nil
}

func webSearchConfigs(specs []providerregistry.Spec, maxSearches int, allowDirectFallback bool) map[string]websearch.Config {
	out := make(map[string]websearch.Config)
	if maxSearches <= 0 {
		maxSearches = websearch.DefaultMaxSearches
	}
	for _, spec := range specs {
		if !spec.Capability.HostedWebSearch {
			continue
		}
		cfg := websearch.Config{
			ProviderID:    spec.ID,
			Endpoint:      spec.Endpoint,
			APIKey:        spec.APIKey,
			AuthClass:     string(spec.AuthMode),
			Wire:          string(spec.Protocol),
			AllowedModels: append([]string(nil), spec.Capability.WebSearchModels...),
			Enabled:       true,
			CredentialRef: spec.CredentialRef,
			MaxSearches:   maxSearches,
		}
		target, err := transport.ResolveTarget(context.Background(), spec.Endpoint, spec.DestinationPolicy)
		if err != nil {
			continue
		}
		options := transport.ClientOptions{AllowDirectFallback: allowDirectFallback}
		if spec.TransportOptions != nil {
			options = *spec.TransportOptions
			options.AllowDirectFallback = allowDirectFallback
		}
		cfg.HTTPClient = transport.NewClient(target, options)
		out[spec.ID] = cfg
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneRegistryOptions(options providerregistry.Options) providerregistry.Options {
	cloned := options
	cloned.PacingRuntime = make(map[string]*transport.Pacer)
	if options.ForwardAuthorities != nil {
		cloned.ForwardAuthorities = make(map[string]openairesponses.ForwardCredentialAuthority, len(options.ForwardAuthorities))
		for id, authority := range options.ForwardAuthorities {
			cloned.ForwardAuthorities[id] = authority
		}
	}
	return cloned
}

func resolveDashboardDir() string {
	if dir := strings.TrimSpace(os.Getenv("BENES_DASHBOARD_DIR")); dir != "" {
		return dir
	}
	candidates := []string{filepath.Join("gui", "dist")}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "gui", "dist"))
	}
	for _, dir := range candidates {
		if info, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !info.IsDir() {
			return dir
		}
	}
	return ""
}

func projectServerCombos(projection config.ComboProjection, specs []providerregistry.Spec) []server.Combo {
	if len(projection.Combos) == 0 {
		return nil
	}
	protocols := make(map[string]string, len(specs))
	for _, spec := range specs {
		protocols[spec.ID] = string(spec.Protocol)
	}
	out := make([]server.Combo, 0, len(projection.Combos))
	for _, spec := range projection.Combos {
		targets := make([]server.ComboTarget, 0, len(spec.Targets))
		for _, target := range spec.Targets {
			targets = append(targets, server.ComboTarget{
				ProviderID: target.ProviderID,
				Model:      target.Model,
				Protocol:   protocols[target.ProviderID],
			})
		}
		out = append(out, server.Combo{ID: spec.ID, Targets: targets})
	}
	return out
}

func ListCatalogModels(disk config.DiskConfig) []catalog.Model {
	return projectCatalogModels(disk, config.ProjectProviderSpecs(disk).Specs)
}

func projectCatalogModels(disk config.DiskConfig, specs []providerregistry.Spec) []catalog.Model {
	policy := catalog.PolicyFromRoot(disk.Raw)
	out := make([]catalog.Model, 0)
	for _, spec := range specs {
		configured := configuredModelsForProvider(disk, spec.ID)
		for _, id := range modeldiscovery.ShownForProvider(disk.Raw, spec.ID) {
			configured = append(configured, catalog.ConfiguredModel{ID: modeldiscovery.CatalogID(spec.ID, id)})
		}
		if len(configured) == 0 && len(policy.RetainModels[spec.ID]) == 0 {
			if defaultModel := defaultModelForProvider(disk.Providers[spec.ID]); defaultModel != "" {
				configured = []catalog.ConfiguredModel{{ID: defaultModel}}
			}
		}
		if len(configured) > 0 {
			if policy.RetainModels == nil {
				policy.RetainModels = map[string]map[string]bool{}
			}
			if policy.RetainModels[spec.ID] == nil {
				policy.RetainModels[spec.ID] = map[string]bool{}
			}
			for _, model := range configured {
				policy.RetainModels[spec.ID][model.ID] = true
			}
		}
		projected, err := catalog.Project(catalog.ProjectionInput{
			ProviderID:  spec.ID,
			Destination: spec.Endpoint,
			Policy:      policy,
			Configured:  configured,
			Candidates:  catalogCandidates(spec),
			RootConfig:  disk.Raw,
		})
		if err != nil {
			continue
		}
		if contract, ok := providerregistry.ModelRuntimeContract(spec.Protocol); ok {
			for i := range projected {
				projected[i].APITypes = append([]string(nil), contract.APITypes...)
				projected[i].ToolUse = runtimeCapabilityState(contract.SupportsToolUse)
				projected[i].Streaming = runtimeCapabilityState(contract.SupportsStreaming)
			}
		}
		for i := range projected {
			if !strings.Contains(projected[i].ID, "/") {
				projected[i].ID = spec.ID + "/" + projected[i].ID
			}
			out = append(out, projected[i])
		}
	}
	return appendSynthesizedCombos(out, disk)
}

func runtimeCapabilityState(value *bool) catalog.CapabilityState {
	if value == nil {
		return catalog.CapabilityUnknown
	}
	if *value {
		return catalog.CapabilityTrue
	}
	return catalog.CapabilityFalse
}

func appendSynthesizedCombos(models []catalog.Model, disk config.DiskConfig) []catalog.Model {
	projection := config.ProjectCombos(disk)
	if len(projection.Combos) == 0 {
		return models
	}
	byID := make(map[string]catalog.Model, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}
	for _, spec := range projection.Combos {
		members := make([]catalog.Model, 0, len(spec.Targets))
		for _, target := range spec.Targets {
			member, ok := byID[target.ProviderID+"/"+target.Model]
			if !ok {
				members = nil
				break
			}
			members = append(members, member)
		}
		if len(members) == 0 {
			continue
		}
		combo, err := catalog.SynthesizeCombo("combo/"+spec.ID, members)
		if err != nil {
			continue
		}
		models = append(models, combo)
	}
	return models
}

type providerModelMetadata struct {
	InputModalities       []string            `json:"inputModalities"`
	ReasoningEfforts      []string            `json:"reasoningEfforts"`
	ModelInputModalities  map[string][]string `json:"modelInputModalities"`
	ModelReasoningEfforts map[string][]string `json:"modelReasoningEfforts"`
}

type configuredCustomModel struct {
	Provider         string   `json:"provider"`
	ModelID          string   `json:"modelId"`
	ContextWindow    int      `json:"contextWindow"`
	InputModalities  []string `json:"inputModalities"`
	ReasoningEfforts []string `json:"reasoningEfforts"`
}

func configuredModelsForProvider(disk config.DiskConfig, providerID string) []catalog.ConfiguredModel {
	raw := disk.Providers[providerID]
	entries := modeldiscovery.ParseProviderCatalog(raw)
	var metadata providerModelMetadata
	_ = json.Unmarshal(raw, &metadata)

	byID := make(map[string]catalog.ConfiguredModel, len(entries))
	order := make([]string, 0, len(entries))
	for _, entry := range entries {
		model := catalog.ConfiguredModel{
			ID:            entry.ID,
			ContextWindow: entry.ContextWindow,
			MaxInput:      entry.MaxInput,
		}
		if metadata.ReasoningEfforts != nil {
			model.ReasoningEfforts = append([]string{}, metadata.ReasoningEfforts...)
		}
		if efforts, ok := metadata.ModelReasoningEfforts[entry.ID]; ok {
			model.ReasoningEfforts = append([]string{}, efforts...)
		}
		if metadata.InputModalities != nil {
			model.Vision = visionCapabilityFromModalities(metadata.InputModalities)
		}
		if modalities, ok := metadata.ModelInputModalities[entry.ID]; ok {
			model.Vision = visionCapabilityFromModalities(modalities)
		}
		byID[entry.ID] = model
		order = append(order, entry.ID)
	}

	var root struct {
		CustomModels []configuredCustomModel `json:"customModels"`
	}
	if json.Unmarshal(disk.Raw, &root) == nil {
		for _, custom := range root.CustomModels {
			if strings.TrimSpace(custom.Provider) != providerID {
				continue
			}
			id := strings.TrimSpace(custom.ModelID)
			if id == "" {
				continue
			}
			model, exists := byID[id]
			if !exists {
				model.ID = id
				order = append(order, id)
			}
			if custom.ContextWindow > 0 {
				model.ContextWindow = custom.ContextWindow
			}
			if custom.InputModalities != nil {
				model.Vision = visionCapabilityFromModalities(custom.InputModalities)
			}
			if custom.ReasoningEfforts != nil {
				model.ReasoningEfforts = append([]string{}, custom.ReasoningEfforts...)
			}
			byID[id] = model
		}
	}

	out := make([]catalog.ConfiguredModel, 0, len(order))
	seen := make(map[string]struct{}, len(order))
	for _, id := range order {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, byID[id])
	}
	return out
}

func visionCapabilityFromModalities(modalities []string) catalog.CapabilityState {
	known := false
	for _, modality := range modalities {
		modality = strings.TrimSpace(modality)
		if modality == "" {
			continue
		}
		known = true
		if strings.EqualFold(modality, "image") {
			return catalog.CapabilityTrue
		}
	}
	if known {
		return catalog.CapabilityFalse
	}
	return catalog.CapabilityUnknown
}

func attachAntigravityAccounts(benesHome, codexHome string, specs []providerregistry.Spec) {
	accounts := loadAntigravityAccounts(benesHome, codexHome)
	if len(accounts) == 0 {
		return
	}
	for i := range specs {
		if specs[i].Protocol == providerregistry.ProtocolGoogleAntigravity && len(specs[i].Accounts) == 0 {
			specs[i].Accounts = append([]antigravity.Account(nil), accounts...)
		}
	}
}

func loadAntigravityAccounts(homes ...string) []antigravity.Account {
	for _, home := range homes {
		if !filepath.IsAbs(home) {
			continue
		}
		accounts, err := antigravity.LoadAccountsFile(filepath.Join(home, "auth.json"))
		if err == nil && len(accounts) > 0 {
			return accounts
		}
	}
	return nil
}

func catalogErrorsFromSkipped(skipped []config.ProviderProjectionSkip) []server.CatalogError {
	if len(skipped) == 0 {
		return nil
	}
	out := make([]server.CatalogError, 0, len(skipped))
	for _, item := range skipped {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Code) == "" {
			continue
		}
		out = append(out, server.CatalogError{ID: item.ID, Code: item.Code})
	}
	return out
}

func newRequestTimelineStore(home string) *timeline.Store {
	if strings.TrimSpace(home) == "" {
		return nil
	}
	return timeline.NewStore(filepath.Join(home, "timeline"), 32)
}

func newSessionStore(home string) (*sessions.Store, error) {
	if strings.TrimSpace(home) == "" {
		return nil, nil
	}
	store, err := sessions.Open(sessions.FilePath(home))
	if err != nil {
		return nil, err
	}
	return store, nil
}

func (p *DataPlane) Close() error {
	if p == nil {
		return nil
	}
	var err error
	if closer, ok := p.Handler.(interface{ Close() error }); ok {
		err = closer.Close()
	}
	if p.sessionStore != nil {
		if serr := p.sessionStore.Close(); serr != nil && err == nil {
			err = serr
		}
		p.sessionStore = nil
	}
	return err
}

func usageLogPath(home string) string {
	home = strings.TrimSpace(home)
	if home == "" {
		return ""
	}
	return filepath.Join(home, "usage.jsonl")
}

func credentialStoreDir(home string) string {
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, "credentials")
}

func adoptPlaintextProviderKeys(store credentials.Store, providers map[string]json.RawMessage) (map[string]credentials.Ref, error) {
	if store == nil {
		return nil, nil
	}
	refs := make(map[string]credentials.Ref)
	for id, raw := range providers {
		id = strings.TrimSpace(id)
		if id == "" || len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil {
			continue
		}
		if _, hasRef := obj["credentialRef"]; hasRef {
			continue
		}
		keyRaw, ok := obj["apiKey"]
		if !ok {
			continue
		}
		var key string
		if json.Unmarshal(keyRaw, &key) != nil {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		ref, err := store.Put(id, []byte(key))
		if err != nil {
			return nil, err
		}
		refs[id] = ref
	}
	return refs, nil
}

func publishAdoptedCredentialRefs(configPath string, refs map[string]credentials.Ref) error {
	if strings.TrimSpace(configPath) == "" || len(refs) == 0 {
		return nil
	}
	store := config.NewTransactionStore(configPath, 0)
	tx, err := store.Begin()
	if err != nil {
		return err
	}
	for id, ref := range refs {
		body, err := json.Marshal(ref)
		if err != nil {
			return err
		}
		if err := tx.Set(config.JSONPath("providers", id, "credentialRef"), body); err != nil {
			return err
		}
	}
	_, err = tx.Commit()
	return err
}

func catalogCandidates(spec providerregistry.Spec) []credentialpool.Candidate {
	authClass := xaicapability.AuthClass(xaicapability.IsOAuthProxyEndpoint(spec.Endpoint))
	if len(spec.APIKeyPool) == 0 {
		if strings.TrimSpace(spec.APIKey) == "" {
			return nil
		}
		return []credentialpool.Candidate{{
			Ref: "primary", Destination: spec.Endpoint, AuthClass: authClass,
			Evidence: credentialpool.Evidence{Auth: credentialpool.AuthUsable, Quota: credentialpool.QuotaUnknown, Limit: credentialpool.LimitAvailable},
		}}
	}
	out := make([]credentialpool.Candidate, 0, len(spec.APIKeyPool))
	for _, slot := range spec.APIKeyPool {
		out = append(out, credentialpool.Candidate{
			Ref: slot.ID, Destination: spec.Endpoint, AuthClass: authClass,
			Evidence: credentialpool.Evidence{Auth: credentialpool.AuthUsable, Quota: credentialpool.QuotaUnknown, Limit: credentialpool.LimitAvailable},
		})
	}
	return out
}

func attachKiroAccount(benesHome string, specs []providerregistry.Spec) {
	if !filepath.IsAbs(benesHome) {
		return
	}
	path := filepath.Join(benesHome, "auth.json")
	account, ok := antigravity.ActiveStoredAccount(path, kiro.ProviderID)
	if !ok || strings.TrimSpace(account.Token) == "" {
		return
	}
	snap, err := kiro.SnapshotFromStored(account)
	if err != nil {
		return
	}
	snaps := loadKiroSnapshots(path, snap)
	for i := range specs {
		if specs[i].Protocol != providerregistry.ProtocolKiro && specs[i].ID != kiro.ProviderID {
			continue
		}
		specs[i].APIKey = snap.AccessToken
		specs[i].ProfileARN = snap.ProfileARN
		specs[i].APIRegion = snap.EffectiveRegion()
		specs[i].AuthType = snap.AuthType
		specs[i].SSORegion = snap.SSORegion
		specs[i].Endpoint = kiro.RuntimeURL(snap.EffectiveRegion())
		specs[i].KiroAccounts = snaps
		specs[i].APIKeyPool = nil
		specs[i].CredentialRef = credentials.Ref{}
	}
}

func loadKiroSnapshots(path string, active kiro.AccountSnapshot) []kiro.AccountSnapshot {
	raw, err := os.ReadFile(path)
	if err != nil {
		return []kiro.AccountSnapshot{active}
	}
	stored := antigravity.ParseAuthStoreFor(raw, kiro.ProviderID)
	out := make([]kiro.AccountSnapshot, 0, len(stored))
	seen := map[string]struct{}{}
	for _, item := range stored {
		if item.NeedsReauth || strings.TrimSpace(item.Token) == "" {
			continue
		}
		snap, err := kiro.SnapshotFromStored(item)
		if err != nil {
			continue
		}
		ref := snap.Ref()
		if ref == "" {
			out = append(out, snap)
			continue
		}
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, snap)
	}
	if len(out) == 0 {
		return []kiro.AccountSnapshot{active}
	}
	if ref := active.Ref(); ref != "" {
		if _, ok := seen[ref]; !ok {
			return append([]kiro.AccountSnapshot{active}, out...)
		}
	}
	return out
}

func dropUnresolvedKiroOAuth(specs []providerregistry.Spec, skipped []config.ProviderProjectionSkip) ([]providerregistry.Spec, []config.ProviderProjectionSkip) {
	out := make([]providerregistry.Spec, 0, len(specs))
	for _, spec := range specs {
		if spec.Protocol == providerregistry.ProtocolKiro && spec.AuthMode == providerregistry.AuthModeOAuth && strings.TrimSpace(spec.APIKey) == "" {
			skipped = append(skipped, config.ProviderProjectionSkip{ID: spec.ID, Code: "missing_credential", Field: "apiKey"})
			continue
		}
		out = append(out, spec)
	}
	return out, skipped
}

func attachXAIAccount(benesHome string, specs []providerregistry.Spec) {
	if !filepath.IsAbs(benesHome) {
		return
	}
	account, ok := antigravity.ActiveStoredAccount(filepath.Join(benesHome, "auth.json"), "xai")
	if !ok || strings.TrimSpace(account.Token) == "" {
		return
	}
	for i := range specs {
		if specs[i].ID != "xai" || !xaicapability.IsOAuthProxyEndpoint(specs[i].Endpoint) {
			continue
		}
		specs[i].APIKey = account.Token
		specs[i].APIKeyPool = nil
		specs[i].CredentialRef = credentials.Ref{}
	}
}

func dropUnresolvedXAIOAuth(specs []providerregistry.Spec, skipped []config.ProviderProjectionSkip) ([]providerregistry.Spec, []config.ProviderProjectionSkip) {
	out := make([]providerregistry.Spec, 0, len(specs))
	for _, spec := range specs {
		if spec.ID == "xai" && xaicapability.IsOAuthProxyEndpoint(spec.Endpoint) && strings.TrimSpace(spec.APIKey) == "" {
			skipped = append(skipped, config.ProviderProjectionSkip{ID: spec.ID, Code: "missing_credential", Field: "apiKey"})
			continue
		}
		out = append(out, spec)
	}
	return out, skipped
}

func defaultModelForProvider(raw json.RawMessage) string {
	var fields struct {
		DefaultModel string `json:"defaultModel"`
	}
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	return strings.TrimSpace(fields.DefaultModel)
}

func buildContinuationAuthority(home string) (*continuation.Authority, error) {
	store := continuation.NewStore(continuation.StoreLimits{}, nil)
	var persister *continuation.Persister
	var salt []byte
	if strings.TrimSpace(home) == "" {
		salt = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return nil, fmt.Errorf("generate ephemeral continuation salt: %w", err)
		}
	} else {
		saltPath := filepath.Join(home, "continuation", "install.salt")
		snapshotPath := filepath.Join(home, "continuation", "state.json")
		var err error
		salt, err = continuation.LoadOrCreateInstallationSalt(saltPath)
		if err != nil {
			return nil, err
		}
		if err := continuation.LoadSnapshot(snapshotPath, store, 0); err != nil && !errors.Is(err, continuation.ErrSnapshotVersion) {
			return nil, err
		}
		persister = continuation.NewPersister(store, snapshotPath)
	}
	return continuation.NewAuthority(store, persister, salt)
}

func codexQuotaProviderIDs(specs []providerregistry.Spec) []string {
	out := make([]string, 0, 1)
	for _, spec := range specs {
		if spec.AuthMode == providerregistry.AuthModeForward && strings.TrimSpace(spec.ID) != "" {
			out = append(out, spec.ID)
		}
	}
	return out
}
