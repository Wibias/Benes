package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/harnessboard"
	"github.com/Wibias/Benes/internal/provideractivity"
	"github.com/Wibias/Benes/internal/quota"
)

const providerWorkspaceQuotaExhaustedPercent = 99.5

type providerWorkspaceIssue struct {
	Provider  string `json:"provider"`
	Code      string `json:"code"`
	Severity  string `json:"severity"`
	Detail    string `json:"detail,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type providerWorkspaceEvidence struct {
	CredentialMissing          bool
	CredentialStoreUnavailable bool
	CredentialPoolPaused       bool
	ReauthRequired             bool
	ReauthAt                   int64
	QuotaExhausted             bool
	QuotaAt                    int64
	HealthFailed               bool
	HealthAt                   int64
	CatalogueStale             bool
	CatalogueAt                int64
	LastValidated              int64
	LastModelSync              int64
}

type providerWorkspaceClassification struct {
	Lifecycle string
	Issues    []providerWorkspaceIssue
}

type providerWorkspaceSummary struct {
	TotalProviders int `json:"totalProviders"`
	Healthy        int `json:"healthy"`
	Attention      int `json:"attention"`
	Disabled       int `json:"disabled"`
	ExposedModels  int `json:"exposedModels"`
}

type providerWorkspaceProviderDownstream struct {
	Harnesses *int `json:"harnesses"`
	Routes    int  `json:"routes"`
	Subagents int  `json:"subagents"`
}

type providerWorkspaceProviderRow struct {
	ID            string                              `json:"id"`
	Connections   []string                            `json:"connections"`
	Hidden        []string                            `json:"hidden"`
	Lifecycle     string                              `json:"lifecycle"`
	ModelCount    int                                 `json:"modelCount"`
	Access        config.AccessDescriptor             `json:"access"`
	Disabled      bool                                `json:"disabled"`
	LastValidated *int64                              `json:"lastValidated"`
	Downstream    providerWorkspaceProviderDownstream `json:"downstream"`
}

type providerWorkspaceAvailability struct {
	ModelsAvailable         int    `json:"modelsAvailable"`
	ModelsUnavailable       int    `json:"modelsUnavailable"`
	StaleProviderCatalogues *int   `json:"staleProviderCatalogues"`
	LastModelSync           *int64 `json:"lastModelSync"`
}

type providerWorkspaceDownstream struct {
	HarnessCount       *int `json:"harnessCount"`
	RouteCount         int  `json:"routeCount"`
	SubAgentModelCount int  `json:"subAgentModelCount"`
	AffectedRouteCount int  `json:"affectedRouteCount"`
}

type providersWorkspaceResponse struct {
	Summary      providerWorkspaceSummary       `json:"summary"`
	Providers    []providerWorkspaceProviderRow `json:"providers"`
	Attention    []providerWorkspaceIssue       `json:"attention"`
	Availability providerWorkspaceAvailability  `json:"availability"`
	Downstream   providerWorkspaceDownstream    `json:"downstream"`
	RecentEvents []provideractivity.Event       `json:"recentEvents"`
}

func (h *handler) serveProvidersWorkspace(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/providers/workspace" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	writeJSON(w, http.StatusOK, h.providersWorkspaceView())
	return true
}

func (h *handler) providersWorkspaceView() providersWorkspaceResponse {
	disk := config.DiskConfig{}
	if strings.TrimSpace(h.configPath) != "" {
		if loaded, err := config.LoadDiskConfig(h.configPath, 0); err == nil {
			disk = loaded
		}
	}
	logical := config.FoldLogicalProviders(disk)
	hidden := config.HiddenConnectionSet(logical)
	modelCounts, available, unavailable := logicalModelCounts(logical, hidden, h.catalogModels)
	evidence, staleCount, lastModelSync := h.workspaceEvidence(logical, hidden, disk)

	classifications := make(map[string]providerWorkspaceClassification, len(logical))
	attention := make([]providerWorkspaceIssue, 0)
	summary := providerWorkspaceSummary{TotalProviders: len(logical)}
	for _, lp := range logical {
		classification := classifyProviderWorkspace(lp, evidence[lp.ID])
		classifications[lp.ID] = classification
		attention = append(attention, classification.Issues...)
		switch classification.Lifecycle {
		case "disabled":
			summary.Disabled++
		case "attention":
			summary.Attention++
		default:
			summary.Healthy++
		}
	}

	combos := map[string]Combo{}
	for _, spec := range h.listRuntimeCombos() {
		combos[spec.ID] = spec
	}
	routeCount, affectedRoutes, routesByProvider := comboImpact(combos, logical, classifications)
	subagentCount, subagentsByProvider := diskSubagentImpact(disk, logical)
	rows := make([]providerWorkspaceProviderRow, 0, len(logical))
	for _, lp := range logical {
		classification := classifications[lp.ID]
		var validated *int64
		if timestamp := evidence[lp.ID].LastValidated; timestamp > 0 {
			value := timestamp
			validated = &value
		}
		rows = append(rows, providerWorkspaceProviderRow{
			ID:            lp.ID,
			Connections:   append([]string{}, lp.ConnectionIDs...),
			Hidden:        append([]string{}, lp.HiddenIDs...),
			Lifecycle:     classification.Lifecycle,
			ModelCount:    modelCounts[lp.ID],
			Access:        lp.Access,
			Disabled:      lp.Disabled,
			LastValidated: validated,
			Downstream: providerWorkspaceProviderDownstream{
				Harnesses: nil,
				Routes:    routesByProvider[lp.ID],
				Subagents: subagentsByProvider[lp.ID],
			},
		})
		summary.ExposedModels += modelCounts[lp.ID]
	}

	return providersWorkspaceResponse{
		Summary:   summary,
		Providers: rows,
		Attention: attention,
		Availability: providerWorkspaceAvailability{
			ModelsAvailable:         available,
			ModelsUnavailable:       unavailable,
			StaleProviderCatalogues: staleCount,
			LastModelSync:           lastModelSync,
		},
		Downstream: providerWorkspaceDownstream{
			HarnessCount:       h.workspaceHarnessCount(),
			RouteCount:         routeCount,
			SubAgentModelCount: subagentCount,
			AffectedRouteCount: affectedRoutes,
		},
		RecentEvents: h.workspaceEvents(logical, hidden),
	}
}

func classifyProviderWorkspace(lp config.LogicalProvider, evidence providerWorkspaceEvidence) providerWorkspaceClassification {
	if lp.Disabled {
		return providerWorkspaceClassification{Lifecycle: "disabled"}
	}
	issues := make([]providerWorkspaceIssue, 0, 7)
	add := func(code, severity string, timestamp int64) {
		issues = append(issues, providerWorkspaceIssue{
			Provider: lp.ID, Code: code, Severity: severity, Timestamp: timestamp,
		})
	}
	if lp.NeedsCredential || evidence.CredentialMissing {
		add("credential_missing", "warn", 0)
	}
	if evidence.CredentialStoreUnavailable {
		add("credential_store_unavailable", "warn", 0)
	}
	if evidence.CredentialPoolPaused {
		add("credential_pool_paused", "warn", 0)
	}
	if evidence.ReauthRequired {
		add("credential_reauth_required", "warn", evidence.ReauthAt)
	}
	if evidence.QuotaExhausted {
		add("quota_exhausted", "warn", evidence.QuotaAt)
	}
	if evidence.HealthFailed {
		add("provider_health_failure", "error", evidence.HealthAt)
	}
	if evidence.CatalogueStale {
		add("catalogue_stale", "warn", evidence.CatalogueAt)
	}
	if len(issues) > 0 {
		return providerWorkspaceClassification{Lifecycle: "attention", Issues: issues}
	}
	return providerWorkspaceClassification{Lifecycle: "healthy"}
}

func (h *handler) workspaceEvidence(logical []config.LogicalProvider, hidden map[string]bool, disk config.DiskConfig) (map[string]providerWorkspaceEvidence, *int, *int64) {
	evidence := make(map[string]providerWorkspaceEvidence, len(logical))
	for _, lp := range logical {
		evidence[lp.ID] = providerWorkspaceEvidence{}
	}

	if h != nil && h.activity != nil {
		for _, event := range h.activity.Recent(64) {
			sourceProvider := event.Provider
			provider := sourceProvider
			if hidden[provider] {
				provider = logicalIDForConnection(logical, provider)
			}
			if _, ok := evidence[provider]; !ok {
				continue
			}
			if !providerWorkspaceEvidenceEventApplies(logical, sourceProvider, provider, event) {
				continue
			}
			item := evidence[provider]
			applyProviderWorkspaceEvent(&item, event)
			evidence[provider] = item
		}
	}

	h.applyWorkspaceCredentialEvidence(evidence, logical, disk)
	h.applyWorkspaceQuotaEvidence(evidence, logical, disk)
	h.applyWorkspaceReauthEvidence(evidence, logical, disk)
	for id, item := range evidence {
		applyWorkspaceObservedRecovery(&item)
		evidence[id] = item
	}
	h.applyWorkspaceOpenAIPoolHealthEvidence(evidence, logical, disk)

	stale := 0
	latestSync := int64(0)
	for _, item := range evidence {
		if item.CatalogueStale {
			stale++
		}
		if item.LastModelSync > latestSync {
			latestSync = item.LastModelSync
		}
	}
	var stalePtr *int
	if stale > 0 {
		value := stale
		stalePtr = &value
	}
	var syncPtr *int64
	if latestSync > 0 {
		value := latestSync
		syncPtr = &value
	}
	return evidence, stalePtr, syncPtr
}

func providerWorkspaceEvidenceEventApplies(
	logical []config.LogicalProvider,
	sourceProvider string,
	logicalProvider string,
	event provideractivity.Event,
) bool {
	if sourceProvider != config.OpenAIAPIConnection || logicalProvider != config.LogicalOpenAIID {
		return true
	}
	openAI, ok := workspaceLogicalProvider(logical, config.LogicalOpenAIID)
	if !ok || openAI.Access.DefaultMethodID == config.DefaultAccessAPI {
		return true
	}
	switch event.Type {
	case "credentials_validated", "connection_validated",
		"quota_exhausted", "quota_recovered", "rate_limited",
		"provider_health_failure", "provider_health_recovered":
		return false
	default:
		return true
	}
}

func applyProviderWorkspaceEvent(evidence *providerWorkspaceEvidence, event provideractivity.Event) {
	if evidence == nil {
		return
	}
	switch event.Type {
	case "credential_reauth_required":
		if event.Timestamp >= evidence.ReauthAt {
			evidence.ReauthRequired = true
			evidence.ReauthAt = event.Timestamp
		}
	case "credentials_validated", "connection_validated", "oauth_reauthenticated":
		if event.Timestamp > evidence.LastValidated {
			evidence.LastValidated = event.Timestamp
		}
		if event.Timestamp >= evidence.ReauthAt {
			evidence.ReauthRequired = false
			evidence.ReauthAt = event.Timestamp
		}
		if event.Timestamp >= evidence.HealthAt {
			evidence.HealthFailed = false
			evidence.HealthAt = event.Timestamp
		}
	case "quota_exhausted", "rate_limited":
		if event.Timestamp >= evidence.QuotaAt {
			evidence.QuotaExhausted = true
			evidence.QuotaAt = event.Timestamp
		}
	case "quota_recovered":
		if event.Timestamp >= evidence.QuotaAt {
			evidence.QuotaExhausted = false
			evidence.QuotaAt = event.Timestamp
		}
	case "provider_health_failure":
		if event.Timestamp >= evidence.HealthAt {
			evidence.HealthFailed = true
			evidence.HealthAt = event.Timestamp
		}
	case "provider_health_recovered":
		if event.Timestamp >= evidence.HealthAt {
			evidence.HealthFailed = false
			evidence.HealthAt = event.Timestamp
		}
	case "model_catalogue_stale":
		if event.Timestamp >= evidence.CatalogueAt {
			evidence.CatalogueStale = true
			evidence.CatalogueAt = event.Timestamp
		}
	case "model_catalogue_synchronized":
		if event.Timestamp > evidence.LastModelSync {
			evidence.LastModelSync = event.Timestamp
		}
		if event.Timestamp >= evidence.CatalogueAt {
			evidence.CatalogueStale = false
			evidence.CatalogueAt = event.Timestamp
		}
		if event.Timestamp >= evidence.HealthAt {
			evidence.HealthFailed = false
			evidence.HealthAt = event.Timestamp
		}
		if event.Timestamp >= evidence.QuotaAt {
			evidence.QuotaExhausted = false
			evidence.QuotaAt = event.Timestamp
		}
	}
}

func applyWorkspaceObservedRecovery(evidence *providerWorkspaceEvidence) {
	if evidence == nil {
		return
	}
	if evidence.LastValidated > 0 && evidence.LastValidated >= evidence.HealthAt {
		evidence.HealthFailed = false
	}
	if evidence.LastModelSync > 0 && evidence.LastModelSync >= evidence.HealthAt {
		evidence.HealthFailed = false
	}
	if evidence.LastModelSync > 0 && evidence.LastModelSync >= evidence.CatalogueAt {
		evidence.CatalogueStale = false
	}
	latestOK := evidence.LastValidated
	if evidence.LastModelSync > latestOK {
		latestOK = evidence.LastModelSync
	}
	if latestOK > 0 && latestOK >= evidence.QuotaAt {
		evidence.QuotaExhausted = false
	}
}

func (h *handler) workspaceQuotaReports() []quota.Report {
	if h == nil {
		return nil
	}
	cached := h.quotaStore.Snapshot().Reports
	out := append([]quota.Report(nil), cached...)
	if h.codexQuota != nil {
		out = append(out, h.codexQuota()...)
	}
	return out
}

func workspaceQuotaReportApplies(report quota.Report, logicalProvider string, disk config.DiskConfig) bool {
	if strings.TrimSpace(report.AccountID) == "" {
		return true
	}
	if logicalProvider != config.LogicalOpenAIID {
		return true
	}
	active := activeCodexAccountID(disk)
	return active != "" && active == strings.TrimSpace(report.AccountID)
}

func activeCodexAccountID(disk config.DiskConfig) string {
	var root struct {
		Active string `json:"activeCodexAccountId"`
		Pinned string `json:"activeCodexAccountPinned"`
	}
	_ = json.Unmarshal(disk.Raw, &root)
	if active := strings.TrimSpace(root.Active); active != "" {
		return active
	}
	return strings.TrimSpace(root.Pinned)
}

func providerQuotaExhausted(value quota.Quota) bool {
	for _, percent := range []*float64{value.FiveHourPercent, value.WeeklyPercent, value.MonthlyPercent} {
		if percent != nil && *percent >= providerWorkspaceQuotaExhaustedPercent {
			return true
		}
	}
	for _, window := range value.CustomWindows {
		if window.Percent >= providerWorkspaceQuotaExhaustedPercent {
			return true
		}
	}
	return value.CreditsUsd != nil && !value.CreditsUsd.Unlimited && value.CreditsUsd.Percent >= providerWorkspaceQuotaExhaustedPercent
}

func logicalModelCounts(logical []config.LogicalProvider, hidden map[string]bool, models []catalog.Model) (map[string]int, int, int) {
	idOf := logicalConnectionMap(logical)
	counts := map[string]int{}
	seen := map[string]bool{}
	available, unavailable := 0, 0
	for _, model := range models {
		provider, rest := splitProviderModel(model.ID)
		logicalID := idOf[provider]
		if logicalID == "" {
			if hidden[provider] {
				continue
			}
			continue
		}
		key := logicalID + "/" + rest
		if seen[key] {
			continue
		}
		seen[key] = true
		counts[logicalID]++
		if modelCallability(model) {
			available++
			continue
		}
		unavailable++
	}
	return counts, available, unavailable
}

// modelCallability is catalog.Availability.Selectable — the same callability the
// projector uses. Hidden/disabled visibility is a separate Models-catalog concept
// and must not be counted as unavailable. Zero-value Availability means the model
// has not been marked unselectable (discovery-synced rows often omit the field).
func modelCallability(model catalog.Model) bool {
	if model.Availability.Reason != "" {
		return model.Availability.Selectable
	}
	return true
}

func splitProviderModel(id string) (string, string) {
	provider, model, ok := strings.Cut(id, "/")
	if !ok {
		return id, id
	}
	return provider, model
}

func logicalConnectionMap(logical []config.LogicalProvider) map[string]string {
	idOf := map[string]string{}
	for _, lp := range logical {
		idOf[lp.ID] = lp.ID
		for _, conn := range lp.ConnectionIDs {
			idOf[conn] = lp.ID
		}
	}
	return idOf
}

func comboImpact(combos map[string]Combo, logical []config.LogicalProvider, classifications map[string]providerWorkspaceClassification) (int, int, map[string]int) {
	idOf := logicalConnectionMap(logical)
	byProvider := map[string]int{}
	affected := 0
	for _, combo := range combos {
		seenProviders := map[string]bool{}
		issue := false
		for _, target := range combo.Targets {
			logicalID := idOf[target.ProviderID]
			if logicalID == "" {
				continue
			}
			seenProviders[logicalID] = true
			state := classifications[logicalID].Lifecycle
			if state == "attention" || state == "disabled" {
				issue = true
			}
		}
		for provider := range seenProviders {
			byProvider[provider]++
		}
		if issue {
			affected++
		}
	}
	return len(combos), affected, byProvider
}

func diskDisabledModels(disk config.DiskConfig) []string {
	var root struct {
		DisabledModels []string `json:"disabledModels"`
	}
	_ = json.Unmarshal(disk.Raw, &root)
	return root.DisabledModels
}

func diskSubagentImpact(disk config.DiskConfig, logical []config.LogicalProvider) (int, map[string]int) {
	var root struct {
		SubagentModels        map[string]string   `json:"subagentModels"`
		SubagentModelFallback map[string][]string `json:"subagentModelFallback"`
		InjectionModel        string              `json:"injectionModel"`
	}
	_ = json.Unmarshal(disk.Raw, &root)
	ids := map[string]bool{}
	for _, id := range root.SubagentModels {
		if id = strings.TrimSpace(id); id != "" {
			ids[id] = true
		}
	}
	for _, chain := range root.SubagentModelFallback {
		for _, id := range chain {
			if id = strings.TrimSpace(id); id != "" {
				ids[id] = true
			}
		}
	}
	if id := strings.TrimSpace(root.InjectionModel); id != "" {
		ids[id] = true
	}

	idOf := logicalConnectionMap(logical)
	byProvider := map[string]int{}
	for id := range ids {
		provider, _ := splitProviderModel(id)
		if logicalID := idOf[provider]; logicalID != "" {
			byProvider[logicalID]++
		}
	}
	return len(ids), byProvider
}

func (h *handler) workspaceHarnessCount() *int {
	if h == nil {
		return nil
	}
	home := strings.TrimSpace(h.benesHome())
	if home == "" {
		return nil
	}
	clients, err := harnessboard.Probe(harnessboard.ProbeInput{
		BenesHome: home,
		CodexHome: strings.TrimSpace(h.codexHome),
	})
	if err != nil {
		return nil
	}
	n := 0
	for _, client := range clients {
		if client.Running != nil && *client.Running {
			n++
			continue
		}
		if client.DetectPath == nil {
			continue
		}
		path := strings.TrimSpace(*client.DetectPath)
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			n++
		}
	}
	if n == 0 {
		return nil
	}
	return &n
}

func (h *handler) workspaceEvents(logical []config.LogicalProvider, hidden map[string]bool) []provideractivity.Event {
	if h == nil || h.activity == nil {
		return []provideractivity.Event{}
	}
	events := h.activity.Recent(12)
	if len(events) == 0 {
		return []provideractivity.Event{}
	}
	out := make([]provideractivity.Event, len(events))
	copy(out, events)
	for i := range out {
		if hidden[out[i].Provider] {
			out[i].Provider = logicalIDForConnection(logical, out[i].Provider)
		}
	}
	return out
}

func (h *handler) recordProviderActivity(provider, eventType, detail, severity string) {
	if h == nil {
		return
	}
	if eventType == "default_access_changed" {
		invalidateProviderDefaultAccess(h.configPath)
	}
	if h.activity == nil {
		return
	}
	h.activity.Record(provideractivity.Event{
		Provider:  provider,
		Type:      eventType,
		Detail:    detail,
		Severity:  severity,
		Timestamp: time.Now().UnixMilli(),
	})
}

func logicalIDForConnection(logical []config.LogicalProvider, connection string) string {
	for _, lp := range logical {
		if lp.ID == connection {
			return lp.ID
		}
		for _, id := range lp.ConnectionIDs {
			if id == connection {
				return lp.ID
			}
		}
	}
	return connection
}
