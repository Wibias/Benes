package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/codexappserver"
	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/modelprobe"
	"github.com/Wibias/Benes/internal/nativemain"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/provideractivity"
	providercontract "github.com/Wibias/Benes/internal/providers"

	"github.com/Wibias/Benes/internal/quota"
	"github.com/Wibias/Benes/internal/requesthistory"
	"github.com/Wibias/Benes/internal/requestpolicy"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/bridge"
	"github.com/Wibias/Benes/internal/responses/parsed"
	requestwire "github.com/Wibias/Benes/internal/responses/request"
	"github.com/Wibias/Benes/internal/responses/sse"
	"github.com/Wibias/Benes/internal/router"
	"github.com/Wibias/Benes/internal/sessions"
	"github.com/Wibias/Benes/internal/sidecar/websearch"
	"github.com/Wibias/Benes/internal/storage"
	"github.com/Wibias/Benes/internal/timeline"
	"github.com/Wibias/Benes/internal/usage"
	"github.com/Wibias/Benes/internal/usageledger"
)

type EventStream = providercontract.EventStream

type Provider = providercontract.Responses

type Options struct {
	// DataPlaneToken is a temporary migration compatibility alias for earlier Go
	// slices. New listener-aware callers must use AdmissionPolicy. Final cutover
	// removes both legacy token fields.
	DataPlaneToken         string
	DataPlaneTokens        []string
	AdmissionPolicy        *DataPlaneAdmissionPolicy
	Providers              map[string]Provider
	CodexAccountNamespaces map[string]string
	MaxRequestBytes        int64
	ImageMaxRequestBytes   int64
	ImageTimeout           time.Duration
	ResourceBudget         *resourcebudget.Manager
	CatalogModels          []catalog.Model
	Timeline               *timeline.Store
	Combos                 []Combo
	Aliases                router.AliasTable
	CatalogMaxBytes        int
	CatalogErrors          []CatalogError
	WebSearch              map[string]websearch.Config
	Credentials            credentials.Store
	DashboardDir           string
	ConfigPath             string
	CodexHome              string
	Home                   string
	Host                   string
	UsageLogPath           string
	AuthStorePath          string
	CodexQuota             func() []quota.Report
	CodexHealth            *codexauth.HealthState
	CodexAccounts          *CodexAccountRuntime
	NativeMainKeys         nativemain.KeyProvider
	CodexAppServer         codexappserver.ServiceIO
	Sessions               *sessions.Store
	SessionsUnavailable    bool
}

func NewHandler(options Options) (http.Handler, error) {
	if options.DataPlaneToken != "" && len(options.DataPlaneTokens) > 0 {
		return nil, fmt.Errorf("DataPlaneToken and DataPlaneTokens cannot be combined")
	}

	admission, err := buildDataPlaneAdmission(options)
	if err != nil {
		return nil, err
	}
	if len(options.Providers) == 0 {
		return nil, fmt.Errorf("at least one provider is required")
	}
	maxRequestBytes := options.MaxRequestBytes
	if maxRequestBytes <= 0 {
		maxRequestBytes = 16 << 20
	}
	catalogMaxBytes := options.CatalogMaxBytes
	if catalogMaxBytes <= 0 {
		catalogMaxBytes = 1 << 20
	}
	authStorePath := strings.TrimSpace(options.AuthStorePath)
	if authStorePath == "" && strings.TrimSpace(options.ConfigPath) != "" {
		authStorePath = filepath.Join(filepath.Dir(options.ConfigPath), "auth.json")
	}
	home := strings.TrimSpace(options.Home)
	if home == "" {
		if resolved, resolveErr := os.UserHomeDir(); resolveErr == nil {
			home = resolved
		}
	}
	host := strings.TrimSpace(options.Host)
	if host == "" {
		host = runtime.GOOS
	}
	h := &handler{
		providers:              options.Providers,
		codexAccountNamespaces: cloneCodexAccountNamespaces(options.CodexAccountNamespaces),
		maxRequestBytes:        maxRequestBytes,
		imageMaxRequestBytes:   options.ImageMaxRequestBytes,
		imageTimeout:           options.ImageTimeout,
		resourceBudget:         options.ResourceBudget,
		catalogModels:          append([]catalog.Model(nil), options.CatalogModels...),
		admission:              admission,
		// Temporary mirror for earlier migration tests. Admission decisions use
		// admission exclusively; this field disappears with the legacy bearer path.
		dataPlaneTokenHashes: admission.tokenHashes,
		timeline:             options.Timeline,
		combos:               cloneCombos(options.Combos),
		aliases:              options.Aliases.Clone(),
		catalogMaxBytes:      catalogMaxBytes,
		catalogErrors:        cloneCatalogErrors(options.CatalogErrors),
		webSearch:            cloneWebSearch(options.WebSearch),
		credentials:          options.Credentials,
		dashboard:            dashboardFileServer(options.DashboardDir),
		configPath:           options.ConfigPath,
		codexHome:            options.CodexHome,
		home:                 home,
		host:                 host,
		usageLogPath:         options.UsageLogPath,
		usageHome:            usageHomeFromOptions(options),
		authStorePath:        authStorePath,
		codexQuota:           options.CodexQuota,
		codexHealth:          options.CodexHealth,
		codexAccounts:        options.CodexAccounts,
		nativeMainKeys:       options.NativeMainKeys,
		codexAppServer:       options.CodexAppServer,
		debug:                newDebugState(),
		diagnostics:          newRequestTelemetryState(),
		sessions:             options.Sessions,
		sessionsUnavailable:  options.SessionsUnavailable,
		quotaStore:           &quota.Store{},
		probes:               modelprobe.NewCache(modelprobe.DefaultTTL),
		activity:             provideractivity.New(),
		storageEngine:        storage.NewEngine(),
		policyRuntime:        newPolicyRuntime(),
	}
	if home := strings.TrimSpace(h.usageHome); home != "" {
		if ledger, err := usageledger.Open(home); err == nil {
			h.usageLedger = ledger
		}
		if idx, err := requesthistory.OpenIndexer(home); err == nil {
			h.requestHistory = idx
			_ = idx.CatchUpBestEffort()
		} else {
			h.debug.appendProvider("request-history index unavailable")
		}
	}
	if options.SessionsUnavailable {
		h.debug.appendProvider("sessions store unavailable")
	}
	h.fabricRuntime = newFabricRuntime(h)
	return h, nil
}

type handler struct {
	providers              map[string]Provider
	codexAccountNamespaces map[string]string
	maxRequestBytes        int64
	imageMaxRequestBytes   int64
	imageTimeout           time.Duration
	resourceBudget         *resourcebudget.Manager
	catalogModels          []catalog.Model
	admission              dataPlaneAdmission
	dataPlaneTokenHashes   [][sha256.Size]byte
	timeline               *timeline.Store
	combosMu               sync.RWMutex
	combos                 map[string]Combo
	aliases                router.AliasTable
	catalogMaxBytes        int
	catalogErrors          []CatalogError
	webSearch              map[string]websearch.Config
	credentials            credentials.Store
	dashboard              http.Handler
	configPath             string
	codexHome              string
	home                   string
	host                   string
	usageLogPath           string
	usageHome              string
	usageLedger            *usageledger.Ledger
	requestHistory         *requesthistory.Indexer
	authStorePath          string
	codexQuota             func() []quota.Report
	codexHealth            *codexauth.HealthState
	codexAccounts          *CodexAccountRuntime
	nativeMainKeys         nativemain.KeyProvider
	codexAppServer         codexappserver.ServiceIO
	stop                   func()
	debug                  *debugState
	diagnostics            *requestTelemetryState
	sessions               *sessions.Store
	sessionsUnavailable    bool
	priceOverlayLoad       func() []usage.PriceRecord
	quotaStore             *quota.Store
	probes                 *modelprobe.Cache
	activity               *provideractivity.Log
	policyRuntime          *policyRuntime
	onManagedAccountLoad   func()
	storageEngine          *storage.Engine
	storageCancel          context.CancelFunc
	storageOnce            sync.Once
	storageJob             storageJobState
	storagePersistErr      error
	usageRetentionJob      *usageRetentionJob
	fabricRuntime          *fabricRuntime
	projections            projectionLifecycle
}

func (h *handler) acquireTurn(ctx context.Context, requestBytes int) (*resourcebudget.Turn, error) {
	if h.resourceBudget == nil {
		return nil, nil
	}
	turn, err := h.resourceBudget.AcquireTurn(ctx, "")
	if err != nil {
		return nil, err
	}
	if requestBytes > 0 {
		if _, err := turn.Reserve(resourcebudget.ClassRequestBody, int64(requestBytes)); err != nil {
			_ = turn.Close()
			return nil, err
		}
	}
	turn.SetPhase(resourcebudget.PhaseAdmission)
	if diag := diagnosticsRecorderFrom(ctx); diag != nil {
		diag.BindPhysicalSendTurn(turn)
	}
	return turn, nil
}

func cloneCodexAccountNamespaces(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for namespace, accountID := range values {
		cloned[namespace] = accountID
	}
	return cloned
}

func isDataPlaneRoute(path string) bool {
	return path == "/v1/responses" || path == compactPath || path == alphaSearchPath || path == chatCompletionsPath || path == anthropicMessagesPath || path == imageGenerationsPath || path == imageEditsPath || path == modelsPath || path == requestTimelinePath || path == catalogPath
}

func isDataPlaneGET(path string) bool {
	return path == modelsPath || path == requestTimelinePath || path == catalogPath
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			if cap, ok := w.(*statusCapture); ok && cap.status != 0 {
				return
			}
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		}
	}()
	if h.handleCORS(w, r) {
		return
	}

	if h.serveHealthz(w, r) {
		return
	}
	if h.serveResourceMetrics(w, r) {
		return
	}
	if h.serveStop(w, r) {
		return
	}

	if h.serveCredentialsAPI(w, r) {
		return
	}
	if h.serveProvidersAPI(w, r) {
		return
	}
	if h.serveProviderQuotasAPI(w, r) {
		return
	}
	if h.serveConfigMutationsAPI(w, r) {
		return
	}
	if h.serveConfigAPI(w, r) {
		return
	}
	if h.serveRoutingProfilesAPI(w, r) {
		return
	}
	if h.serveCombosAPI(w, r) {
		return
	}
	if h.serveLabAPI(w, r) {
		return
	}
	if h.serveFabricAPI(w, r) {
		return
	}
	if h.serveAuthAPI(w, r) {
		return
	}
	if h.serveClientIntegrationsAPI(w, r) {
		return
	}
	if h.serveHarnessesAPI(w, r) {
		return
	}
	if h.serveDebugAPI(w, r) {
		return
	}
	if h.serveLogsAPI(w, r) {
		return
	}
	if h.serveDiagnosticsAPI(w, r) {
		return
	}
	if h.serveSessionsAPI(w, r) {
		return
	}
	if h.serveSystemMemoryAPI(w, r) {
		return
	}
	if h.serveCodexAppServerAPI(w, r) {
		return
	}
	if h.serveCodexRestartAPI(w, r) {
		return
	}
	if h.serveStorageCodexLogsAPI(w, r) {
		return
	}

	if h.serveStorageAPI(w, r) {
		return
	}
	if h.serveGrokAPI(w, r) {
		return
	}
	if h.serveUsageRetentionAPI(w, r) {
		return
	}
	if h.serveUsageAPI(w, r) {
		return
	}
	if h.serveOAuthAccountsAPI(w, r) {
		return
	}
	if h.serveNativeMainProfilesAPI(w, r) {
		return
	}
	if h.serveCodexAuthAPI(w, r) {
		return
	}
	if h.serveClaudeCodeAPI(w, r) {
		return
	}
	if h.serveModelsAPI(w, r) {
		return
	}
	if h.serveKeysAPI(w, r) {
		return
	}
	if h.serveClientConfigAPI(w, r) {
		return
	}
	if h.serveAgentSettingsAPI(w, r) {
		return
	}
	if h.serveCustomModelsAPI(w, r) {
		return
	}
	if h.serveSelectedModelsAPI(w, r) {
		return
	}
	if h.serveModelPresetsAPI(w, r) {
		return
	}
	if h.serveModelDiscoveryAPI(w, r) {
		return
	}
	if h.serveSidecarSettingsAPI(w, r) {
		return
	}
	if h.serveContextProjectionAPI(w, r) {
		return
	}
	if h.serveFabricSettingsAPI(w, r) {
		return
	}
	if h.serveShadowCallAPI(w, r) {
		return
	}
	if h.serveSettingsAPI(w, r) {
		return
	}
	if h.serveUpdateAPI(w, r) {
		return
	}
	if h.serveSyncAPI(w, r) {
		return
	}
	if h.serveNativeIntegrationsAPI(w, r) {
		return
	}
	if h.serveStartupHealthAPI(w, r) {
		return
	}
	if h.serveGitHubStarAPI(w, r) {
		return
	}
	if h.serveWindowsTrayAPI(w, r) {
		return
	}
	if h.serveProjectConfigAPI(w, r) {
		return
	}
	if h.serveOAuthLoginAPI(w, r) {
		return
	}
	if h.serveOAuthLoginCodeAPI(w, r) {
		return
	}
	if h.serveOAuthLoginCancelAPI(w, r) {
		return
	}
	if h.serveClaudeDesktopAPI(w, r) {
		return
	}
	if h.serveRequestPacingAPI(w, r) {
		return
	}
	if h.serveContextCapsAPI(w, r) {
		return
	}
	if h.serveCatalogAPI(w, r) {
		return
	}
	if h.serveRequestHistoryAPI(w, r) {
		return
	}
	if h.serveV2API(w, r) {
		return
	}

	if h.serveClaudeInboundDebugAPI(w, r) {
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/") && !isDataPlaneRoute(r.URL.Path) {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	if h.serveDashboard(w, r) {
		return
	}
	if !isDataPlaneRoute(r.URL.Path) || (r.Method != http.MethodPost && !isDataPlaneGET(r.URL.Path)) || (isDataPlaneGET(r.URL.Path) && r.Method != http.MethodGet && r.Method != http.MethodHead) {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	h.recordDataPlaneDebug(r)
	correlationID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	requestID := correlationID
	if requestID == "" {
		requestID = newRequestID()
	}
	started := time.Now()
	capture := &statusCapture{ResponseWriter: w}
	w = capture
	var rec *sessionRecorder
	diag := newDiagnosticsRecorder(requestID, correlationID, started)
	diag.surface = usage.CanonicalSurface(r.Header.Get(usage.HeaderSurface))
	diag.prices = h.newPriceOverlaySnapshot()
	diag.bindSidecarPolicyEvidence(h, r.Context())
	trace := timeline.New(requestID, 16)
	// Admit the configured request policy once for this logical request. Diagnostics and
	// every provider dispatch of this request read the admitted value, never live settings.
	r = r.WithContext(withAdmittedServiceTier(r.Context(), requestpolicy.AdmittedServiceTier()))
	r = r.WithContext(withDiagnosticsRecorder(r.Context(), diag))
	r = r.WithContext(providercontract.WithCommittedUsageAccountObserver(r.Context(), diag))
	r = r.WithContext(timeline.WithTrace(r.Context(), trace))
	defer func() {
		h.recordRequestTelemetry(r, capture, started, rec, trace)
	}()
	w.Header().Set("X-Benes-Request-Id", requestID)
	if h.timeline != nil {
		defer func() { _ = h.timeline.Save(trace) }()
	}
	status, allowed := h.admission.admit(requestView{
		host:          r.Host,
		origin:        r.Header.Get("Origin"),
		authorization: r.Header.Get("Authorization"),
		dedicatedKey:  r.Header.Get("X-Benes-API-Key"),
		tls:           r.TLS != nil,
	})
	if !allowed {
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "admission_denied")
		if h.admission.kind == admissionLegacyBearer && status == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
		}
		if r.URL.Path == chatCompletionsPath {
			if status == http.StatusForbidden {
				writeChatError(w, status, "request host or origin is not allowed", "invalid_request_error", "forbidden")
				return
			}
			writeChatError(w, http.StatusUnauthorized, "invalid data-plane credential", "invalid_request_error", "invalid_api_key")
			return
		}
		if r.URL.Path == anthropicMessagesPath {
			if status == http.StatusForbidden {
				writeAnthropicError(w, status, "request host or origin is not allowed", "permission_error", "forbidden")
				return
			}
			writeAnthropicError(w, http.StatusUnauthorized, "invalid data-plane credential", "authentication_error", "invalid_api_key")
			return
		}
		if status == http.StatusForbidden {
			writeError(w, status, "forbidden", "request host or origin is not allowed")
			return
		}
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid data-plane credential")
		return
	}
	diag.markAdmitted()
	if rec = h.beginSession(r, started, trace); rec != nil {
		r = r.WithContext(withSessionRecorder(r.Context(), rec))
		defer h.finishSession(rec, capture, started)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			capture.panicked = true
			trace.Mark(timeline.StageTerminalDelivery, timeline.SideLocal, "", false, "internal_panic")
			panic(recovered)
		}
	}()
	trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", true, "")
	if r.URL.Path == modelsPath {
		h.handleModels(w, r)
		return
	}
	if r.URL.Path == catalogPath {
		h.handleCatalog(w, r)
		return
	}
	if r.URL.Path == requestTimelinePath {
		h.handleRequestTimeline(w, r)
		return
	}
	if r.URL.Path == chatCompletionsPath {
		h.handleChatCompletions(w, r, trace)
		return
	}
	if r.URL.Path == anthropicMessagesPath {
		h.handleAnthropicMessages(w, r, trace)
		return
	}
	if r.URL.Path == compactPath {
		h.handleCompact(w, r, trace)
		return
	}
	if r.URL.Path == alphaSearchPath {
		h.handleAlphaSearch(w, r, trace)
		return
	}
	if r.URL.Path == imageGenerationsPath || r.URL.Path == imageEditsPath {
		h.handleImages(w, r, trace)
		return
	}
	wireRequest, err := requestwire.Decode(r.Body, h.maxRequestBytes)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, requestwire.ErrTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(w, status, "invalid_request", "invalid Responses request")
		return
	}
	route, err := h.parseRoute(wireRequest.Model)
	if err != nil {
		if message, ok := ambiguousAliasMessage(err); ok {
			writeError(w, http.StatusBadRequest, "invalid_request", message)
			return
		}
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "explicit_route_required")
		writeError(w, http.StatusNotImplemented, "migration_not_ready", "explicit provider/model routing is required by the Go migration path")
		return
	}
	if rec := sessionRecorderFrom(r.Context()); rec != nil {
		rec.SetRequestedModel(wireRequest.Model)
		rec.SetRoute(wireRequest.Model, route)
	}
	if diag := diagnosticsRecorderFrom(r.Context()); diag != nil {
		diag.SetRoute(wireRequest.Model, route)
	}
	trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", true, "")
	request, err := parsed.Build(wireRequest, time.Now().UnixMilli())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid Responses request")
		return
	}
	if rec := sessionRecorderFrom(r.Context()); rec != nil {
		rec.SetParsed(request)
	}
	request.UpstreamModelID = route.Model

	var searchClient *websearch.Client
	turn, err := h.acquireTurn(r.Context(), len(wireRequest.Raw))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "resource_exhausted", "request resource budget is exhausted")
		return
	}
	defer turn.Close()
	reader := turn.OpenReader()
	defer reader.Close()

	var emitFrames func(frames []bridge.Frame) error
	var streamFlusher http.Flusher
	var firstDownstream bool

	// Resolve provider exactly once for Responses. Pass the same resolved authority
	// into runModelTurn (no second resolve). Hosted-search / compaction use this instance.
	resolvedProbe := h.resolveProviderWithEvidence(route, policyEvidenceFromRequest(request))
	if resolvedProbe.MissingCombo {
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "combo_not_found")
		writeError(w, http.StatusNotFound, "not_found", "combo is not configured")
		return
	}
	if resolvedProbe.MissingPolicy {
		trace.Mark(timeline.StagePreDispatch, timeline.SideLocal, "", false, "policy_not_found")
		writeError(w, http.StatusNotFound, "not_found", "routing profile is not configured")
		return
	}
	if resolvedProbe.MissingProvider || resolvedProbe.Provider == nil {
		writeError(w, http.StatusNotImplemented, "migration_not_ready", "provider is not migrated to the Go Responses path")
		return
	}
	routedCompaction := request.CompactionRequest && !isNativeCodexForward(resolvedProbe.Provider)
	if routedCompaction {
		applyRoutedCompaction(&request)
		request.HostedWebSearchTools = nil
	}
	nativeHostedSearch := providercontract.SupportsNativeHostedWebSearch(resolvedProbe.Provider, route.Model)
	if hostedWebSearchRequested(request) && !routedCompaction {
		_, client, err := h.webSearchClientForRequest(r.Context(), webSearchProviderID(route, resolvedProbe), route.Model, nativeHostedSearch)
		if err != nil {
			writeError(w, http.StatusNotImplemented, "web_search_unavailable", err.Error())
			return
		}
		if client != nil {
			searchClient = client
			request.Context.Tools = websearch.ReplaceHostedTools(request.Context.Tools)
			request.HostedWebSearchTools = nil
		}
	}

	if request.Stream {
		headersSent := false
		emitFrames = func(frames []bridge.Frame) error {
			if !headersSent {
				w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				w.Header().Set("X-Content-Type-Options", "nosniff")
				w.WriteHeader(http.StatusOK)
				streamFlusher, _ = w.(http.Flusher)
				headersSent = true
			}
			for _, frame := range frames {
				encoded, _ := json.Marshal(frame.Data)
				if turn != nil {
					if _, err := turn.Reserve(resourcebudget.ClassDownstreamQueue, int64(len(encoded))); err != nil {
						return err
					}
				}
				if err := sse.WriteJSON(w, frame.Data); err != nil {
					markTrace(trace, timeline.StageDownstreamWrite, timeline.SideDownstream, "", false, "downstream_write")
					return err
				}
				turn.MarkCommitted()
				if !firstDownstream {
					firstDownstream = true
					markTrace(trace, timeline.StageDownstreamWrite, timeline.SideDownstream, timeline.MilestoneFirstDownstream, true, "")
				}
				if streamFlusher != nil {
					streamFlusher.Flush()
				}
			}
			return nil
		}
	}

	out, openErr := h.runModelTurn(r.Context(), modelTurnInput{
		Model:            wireRequest.Model,
		Parsed:           &request,
		RequestID:        requestID,
		Surface:          diag.surface,
		Turn:             turn,
		Path:             r.URL.Path,
		Protocol:         "responses",
		Persist:          false,
		ForwardHeaders:   h.snapshotForwardHeaders(r.Header),
		SearchClient:     searchClient,
		RoutedCompaction: routedCompaction,
		EmitFrames:       emitFrames,
		WatchSession:     true,
		Diag:             diag,
		Trace:            trace,
		PreResolvedRoute: &route,
		PreResolved:      &resolvedProbe, // sole Responses resolution authority
	})
	if openErr != nil {
		if errors.Is(openErr, resourcebudget.ErrPhysicalSendBudgetExceeded) {
			writeError(w, http.StatusTooManyRequests, physicalSendBudgetErrorCode, physicalSendBudgetErrorMessage)
			return
		}
		if writeResponsesOpenError(w, request, route.Model, routedCompaction, turn, openErr) {
			return
		}
		writeError(w, http.StatusBadGateway, "upstream_error", publicProviderOpenMessage(openErr))
		return
	}
	if request.Stream {
		return
	}
	switch out.Status {
	case "failed":
		if out.Reason == "combo_not_found" {
			writeError(w, http.StatusNotFound, "not_found", "combo is not configured")
			return
		}
		if out.Reason == "policy_not_found" {
			writeError(w, http.StatusNotFound, "not_found", "routing profile is not configured")
			return
		}
		if out.Reason == "provider_unavailable" || out.Reason == "explicit_route_required" {
			writeError(w, http.StatusNotImplemented, "migration_not_ready", "provider is not migrated to the Go Responses path")
			return
		}
		if out.TerminalResponse != nil {
			h.writeCollectedTerminal(w, out.TerminalResponse, out.HTTPStatus, turn)
			return
		}
		writeError(w, http.StatusBadGateway, "upstream_error", "provider stream failed")
		return
	case "cancelled":
		return
	}
	if out.TerminalResponse == nil {
		writeError(w, http.StatusBadGateway, "upstream_error", "provider stream did not produce a terminal response")
		return
	}
	h.writeCollectedTerminal(w, out.TerminalResponse, out.HTTPStatus, turn)
}

func (h *handler) writeCollectedTerminal(w http.ResponseWriter, terminal map[string]any, status int, turn *resourcebudget.Turn) {
	if status == 0 {
		status = http.StatusOK
	}
	encoded, err := json.Marshal(terminal)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_error", "provider stream did not produce a terminal response")
		return
	}
	if turn != nil {
		if _, err := turn.Reserve(resourcebudget.ClassOutput, int64(len(encoded))); err != nil {
			writeError(w, http.StatusServiceUnavailable, "resource_exhausted", "request resource budget is exhausted")
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
	_, _ = w.Write([]byte("\n"))
	if turn != nil {
		turn.MarkCommitted()
	}
}

func (h *handler) streamResponse(w http.ResponseWriter, r *http.Request, b *bridge.Bridge, stream EventStream, turn *resourcebudget.Turn, tr *timeline.Trace) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	firstDownstream := false
	firstText := false
	writeFrames := func(frames []bridge.Frame) bool {
		for _, frame := range frames {
			encoded, _ := json.Marshal(frame.Data)
			if turn != nil {
				if _, err := turn.Reserve(resourcebudget.ClassDownstreamQueue, int64(len(encoded))); err != nil {
					return false
				}
			}
			if err := sse.WriteJSON(w, frame.Data); err != nil {
				markTrace(tr, timeline.StageDownstreamWrite, timeline.SideDownstream, "", false, "downstream_write")
				return false
			}
			turn.MarkCommitted()
			if !firstDownstream {
				firstDownstream = true
				markTrace(tr, timeline.StageDownstreamWrite, timeline.SideDownstream, timeline.MilestoneFirstDownstream, true, "")
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		return true
	}
	if !writeFrames(b.Start()) {
		return
	}
	for {
		if err := r.Context().Err(); err != nil {
			markTrace(tr, timeline.StageClientCancel, timeline.SideClient, "", false, "client_cancel")
			return
		}
		event, err := stream.Next()
		if errors.Is(err, io.EOF) {
			markTrace(tr, timeline.StageUpstreamRead, timeline.SideUpstream, timeline.MilestoneUpstreamEnd, true, "")
			if writeFrames(b.End()) {
				markTrace(tr, timeline.StageDownstreamWrite, timeline.SideDownstream, timeline.MilestoneDownstreamEnd, true, "")
			}
			return
		}
		if err != nil {
			frames, _ := b.Handle(protocol.Event{Type: protocol.EventError, Message: "provider stream failed"})
			if writeFrames(frames) {
				markTrace(tr, timeline.StageDownstreamWrite, timeline.SideDownstream, timeline.MilestoneDownstreamEnd, true, "")
			}
			return
		}
		frames, bridgeErr := b.Handle(event)
		if !writeFrames(frames) {
			return
		}
		if !firstText && event.Type == protocol.EventTextDelta && event.Text != "" {
			firstText = true
			markTrace(tr, timeline.StageUpstreamRead, timeline.SideUpstream, timeline.MilestoneTTFT, true, "")
		}
		if bridgeErr != nil {
			cause := "translator"
			if event.Validate() != nil {
				cause = "malformed_frame"
			}
			markTrace(tr, timeline.StageRelayTransform, timeline.SideRelay, "", false, cause)
			return
		}
		select {
		case <-r.Context().Done():
			markTrace(tr, timeline.StageClientCancel, timeline.SideClient, "", false, "client_cancel")
			return
		default:
		}
	}
}

func markTrace(tr *timeline.Trace, stage timeline.Stage, side timeline.Side, milestone timeline.Milestone, ok bool, cause string) {
	if tr == nil {
		return
	}
	tr.Mark(stage, side, milestone, ok, cause)
}

func (h *handler) collectResponse(w http.ResponseWriter, b *bridge.Bridge, stream EventStream, turn *resourcebudget.Turn) {
	_ = b.Start()
	var terminal map[string]any
	status := http.StatusOK
	for {
		event, err := stream.Next()
		if errors.Is(err, io.EOF) {
			for _, frame := range b.End() {
				if response, ok := frame.Data["response"].(map[string]any); ok {
					terminal = response
				}
			}
			break
		}
		if err != nil {
			frames, _ := b.Handle(protocol.Event{Type: protocol.EventError, Message: "provider stream failed"})
			for _, frame := range frames {
				if response, ok := frame.Data["response"].(map[string]any); ok {
					terminal = response
				}
			}
			status = http.StatusBadGateway
			break
		}
		frames, bridgeErr := b.Handle(event)
		for _, frame := range frames {
			if frame.Name == "response.completed" || frame.Name == "response.incomplete" || frame.Name == "response.failed" {
				if response, ok := frame.Data["response"].(map[string]any); ok {
					terminal = response
				}
				if frame.Name == "response.failed" {
					status = http.StatusBadGateway
				}
			}
		}
		if bridgeErr != nil {
			status = http.StatusBadGateway
			break
		}
		if terminal != nil {
			break
		}
	}
	if terminal == nil {
		writeError(w, http.StatusBadGateway, "upstream_error", "provider stream did not produce a terminal response")
		return
	}
	encoded, err := json.Marshal(terminal)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_error", "provider stream did not produce a terminal response")
		return
	}
	if turn != nil {
		if _, err := turn.Reserve(resourcebudget.ClassOutput, int64(len(encoded))); err != nil {
			writeError(w, http.StatusServiceUnavailable, "resource_exhausted", "request resource budget is exhausted")
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
	_, _ = w.Write([]byte("\n"))
	turn.MarkCommitted()
}

func hashDataPlaneTokens(tokens []string) ([][sha256.Size]byte, error) {
	return hashAdmissionTokens(tokens)
}

func bearerMatchesAny(header string, tokenHashes [][sha256.Size]byte) bool {
	return tokenHeaderMatches(header, "Bearer ", tokenHashes)
}

func newRequestID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": message}})
}
