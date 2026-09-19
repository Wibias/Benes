package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentials"
	"github.com/Wibias/Benes/internal/modeldiscovery"
	"github.com/Wibias/Benes/internal/providers/antigravity"
	"github.com/Wibias/Benes/internal/providers/cursor"
)

const (
	codexModelsURL       = "https://chatgpt.com/backend-api/codex/models?client_version=1.0.0"
	commandCodeModelsURL = "https://api.commandcode.ai/provider/v1/models"
	modelDiscoveryTO     = 12 * time.Second
	modelDiscoveryBody   = 1 << 20
)

type DiscoverModelsRequest struct {
	Provider     string
	Adapter      string
	AuthMode     string
	BaseURL      string
	APIKey       string
	AccessToken  string
	AccountID    string
	AllowPrivate bool
}

var discoverProviderModels = defaultDiscoverProviderModels

func (h *handler) serveModelDiscoverySync(w http.ResponseWriter, r *http.Request) bool {
	var body struct {
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return true
	}
	provider := strings.TrimSpace(body.Provider)
	if provider == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "provider is required")
		return true
	}
	if strings.TrimSpace(h.configPath) == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return true
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "config could not be read")
		return true
	}
	raw, ok := disk.Providers[provider]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown_provider", "unknown provider")
		return true
	}
	var rec struct {
		Disabled            bool            `json:"disabled"`
		LiveModels          *bool           `json:"liveModels"`
		Adapter             string          `json:"adapter"`
		AuthMode            string          `json:"authMode"`
		BaseURL             string          `json:"baseUrl"`
		APIKey              string          `json:"apiKey"`
		AllowPrivateNetwork bool            `json:"allowPrivateNetwork"`
		CredentialRef       credentials.Ref `json:"credentialRef"`
	}
	_ = json.Unmarshal(raw, &rec)
	if rec.Disabled {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "Provider is disabled"})
		return true
	}
	if rec.LiveModels != nil && !*rec.LiveModels {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "applicable": false, "reason": "static_catalog"})
		return true
	}
	req := DiscoverModelsRequest{
		Provider:     provider,
		Adapter:      rec.Adapter,
		AuthMode:     rec.AuthMode,
		BaseURL:      rec.BaseURL,
		APIKey:       rec.APIKey,
		AllowPrivate: rec.AllowPrivateNetwork,
	}
	if h.credentials != nil && (strings.TrimSpace(rec.CredentialRef.ID) != "" || rec.CredentialRef.Source != "") {
		if secret, err := h.credentials.Get(rec.CredentialRef); err == nil {
			req.APIKey = string(secret)
		}
	}
	if strings.EqualFold(strings.TrimSpace(rec.AuthMode), "forward") {
		req.AccessToken, req.AccountID = h.codexSyncCredential()
	}
	if token, accountID := h.oauthSyncCredential(provider); token != "" {
		req.AccessToken, req.AccountID = token, accountID
		if !strings.EqualFold(strings.TrimSpace(req.AuthMode), "forward") {
			req.AuthMode = "oauth"
		}
	}
	entries, err := discoverProviderModels(req)
	if err != nil {
		h.recordProviderActivity(provider, "model_catalogue_stale", "upstream model discovery failed", "warn")
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "upstream model discovery failed"})
		return true
	}
	if len(entries) == 0 {
		h.recordProviderActivity(provider, "model_catalogue_stale", "upstream model discovery returned no models", "warn")
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "upstream model discovery returned no models"})
		return true
	}
	merged, err := h.persistDiscoveredProviderModels(provider, entries)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "config_unreadable", "discovered models could not be stored")
		return true
	}
	h.replaceProviderCatalog(provider, merged)
	h.recordProviderActivity(provider, "model_catalogue_synchronized", "", "info")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": provider, "models": modeldiscovery.IDsFromEntries(merged)})
	return true
}

func (h *handler) persistDiscoveredProviderModels(provider string, discovered []modeldiscovery.CatalogEntry) ([]modeldiscovery.CatalogEntry, error) {
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return nil, err
	}
	existing := providerModelEntries(provider, disk.Providers[provider])
	merged := mergeCatalogEntries(existing, discovered)
	payload, err := marshalProviderModels(merged)
	if err != nil {
		return nil, err
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		return nil, err
	}
	if err := tx.Set(config.JSONPath("providers", provider, "models"), payload); err != nil {
		return nil, err
	}
	if _, err := tx.Commit(); err != nil {
		return nil, err
	}
	return merged, nil
}

func (h *handler) replaceProviderCatalog(provider string, entries []modeldiscovery.CatalogEntry) {
	prefix := provider + "/"
	next := make([]catalog.Model, 0, len(h.catalogModels)+len(entries))
	for _, model := range h.catalogModels {
		if model.ID == provider || strings.HasPrefix(model.ID, prefix) {
			continue
		}
		next = append(next, model)
	}
	for _, entry := range entries {
		model := catalog.Model{
			ID:           modeldiscovery.CatalogID(provider, entry.ID),
			Discovered:   true,
			Availability: catalog.Availability{Selectable: true},
		}
		if entry.ContextWindow > 0 {
			model.Context = catalog.ContextWindow{Tokens: entry.ContextWindow, Source: catalog.ContextDiscovered}
		} else if curated, _, ok := catalog.CuratedContextFor(provider, entry.ID); ok {
			model.Context = curated
		} else if strings.EqualFold(strings.TrimSpace(provider), "opencode-go") {
			model.Context = catalog.EffectiveContextWindow(catalog.ContextInput{})
		}
		if entry.MaxInput > 0 {
			model.MaxInput = entry.MaxInput
		}
		next = append(next, model)
	}
	h.catalogModels = next
	// The listener's catalogue changed: every applied projection that carries it has to catch up.
	h.notifyCatalogueChanged()
}

func (h *handler) oauthSyncCredential(provider string) (accessToken, accountID string) {
	path := ""
	if h != nil {
		path = h.oauthAuthStorePath()
	}
	if path == "" {
		return "", ""
	}
	account, ok := antigravity.ActiveStoredAccount(path, provider)
	if !ok {
		return "", ""
	}
	return strings.TrimSpace(account.Token), strings.TrimSpace(account.AccountID)
}

func (h *handler) codexSyncCredential() (accessToken, accountID string) {
	if h == nil || h.codexAccounts == nil || h.codexAccounts.Main == nil {
		return "", ""
	}
	now := time.Now()
	if h.codexAccounts.Now != nil {
		now = h.codexAccounts.Now()
	}
	result := h.codexAccounts.Main.Read(now)
	if result.Status != codexauth.MainCredentialOK {
		return "", ""
	}
	return strings.TrimSpace(result.Credential.AccessToken), strings.TrimSpace(result.Credential.ChatGPTAccountID)
}

func providerModelEntries(provider string, raw json.RawMessage) []modeldiscovery.CatalogEntry {
	if strings.EqualFold(strings.TrimSpace(provider), "opencode-go") {
		var rec struct {
			Models json.RawMessage `json:"models"`
		}
		if json.Unmarshal(raw, &rec) != nil {
			return nil
		}
		return modeldiscovery.ParseStoredModels(rec.Models)
	}
	return modeldiscovery.ParseProviderCatalog(raw)
}

func mergeCatalogEntries(existing, discovered []modeldiscovery.CatalogEntry) []modeldiscovery.CatalogEntry {
	byID := map[string]modeldiscovery.CatalogEntry{}
	order := make([]string, 0, len(existing)+len(discovered))
	add := func(entry modeldiscovery.CatalogEntry) {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			return
		}
		prev, ok := byID[id]
		if !ok {
			order = append(order, id)
			byID[id] = modeldiscovery.CatalogEntry{ID: id, ContextWindow: entry.ContextWindow, MaxInput: entry.MaxInput}
			return
		}
		if entry.ContextWindow > 0 {
			prev.ContextWindow = entry.ContextWindow
		}
		if entry.MaxInput > 0 {
			prev.MaxInput = entry.MaxInput
		}
		byID[id] = prev
	}
	for _, entry := range existing {
		add(entry)
	}
	for _, entry := range discovered {
		add(entry)
	}
	sort.Strings(order)
	out := make([]modeldiscovery.CatalogEntry, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out
}

func marshalProviderModels(entries []modeldiscovery.CatalogEntry) ([]byte, error) {
	hasMeta := false
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
		if entry.ContextWindow > 0 || entry.MaxInput > 0 {
			hasMeta = true
		}
	}
	if !hasMeta {
		return json.Marshal(ids)
	}
	rows := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		row := map[string]any{"id": entry.ID}
		if entry.ContextWindow > 0 {
			row["contextWindow"] = entry.ContextWindow
		}
		if entry.MaxInput > 0 {
			row["maxInput"] = entry.MaxInput
		}
		rows = append(rows, row)
	}
	return json.Marshal(rows)
}

func defaultDiscoverProviderModels(req DiscoverModelsRequest) ([]modeldiscovery.CatalogEntry, error) {
	if cursorModelList(req) {
		ids, err := discoverCursorModels(req)
		if err != nil {
			return nil, err
		}
		return modeldiscovery.EntriesFromIDs(ids), nil
	}
	url := modelsListURL(req)
	if url == "" {
		return nil, fmt.Errorf("provider has no model list URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), modelDiscoveryTO)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	applyModelListAuth(request, req)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "benes")
	if account := strings.TrimSpace(req.AccountID); account != "" {
		request.Header.Set("ChatGPT-Account-Id", account)
	}
	client := &http.Client{
		Timeout: modelDiscoveryTO,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, modelDiscoveryBody))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("model list HTTP %d", response.StatusCode)
	}
	entries, ok := modeldiscovery.ParseCatalog(raw)
	if !ok {
		return nil, fmt.Errorf("model list was incomplete")
	}
	return entries, nil
}

func discoverCursorModels(req DiscoverModelsRequest) ([]string, error) {
	token := strings.TrimSpace(req.AccessToken)
	if token == "" {
		token = strings.TrimSpace(req.APIKey)
	}
	if token == "" {
		return nil, fmt.Errorf("Cursor access token is required")
	}
	base := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if base == "" {
		base = cursor.DefaultAPI
	}
	ctx, cancel := context.WithTimeout(context.Background(), modelDiscoveryTO)
	defer cancel()
	client := &http.Client{Timeout: modelDiscoveryTO}
	ids, err := cursor.FetchUsableModels(ctx, client, base, token)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("model list was incomplete")
	}
	return ids, nil
}

func applyModelListAuth(request *http.Request, req DiscoverModelsRequest) {
	token := strings.TrimSpace(req.AccessToken)
	if token == "" {
		token = strings.TrimSpace(req.APIKey)
	}
	if token == "" {
		return
	}
	if googleAIStudioModelList(req) {
		request.Header.Set("x-goog-api-key", token)
		return
	}
	if anthropicModelList(req) {
		request.Header.Set("anthropic-version", "2023-06-01")
		if strings.EqualFold(strings.TrimSpace(req.AuthMode), "oauth") {
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("anthropic-beta", "oauth-2025-04-20")
			return
		}
		request.Header.Set("x-api-key", token)
		return
	}
	request.Header.Set("Authorization", "Bearer "+token)
}

func anthropicModelList(req DiscoverModelsRequest) bool {
	if strings.EqualFold(strings.TrimSpace(req.Provider), "anthropic") || strings.EqualFold(strings.TrimSpace(req.Provider), "anthropic-apikey") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(req.Adapter), "anthropic") {
		return true
	}
	host := strings.ToLower(strings.TrimSpace(req.BaseURL))
	return strings.Contains(host, "api.anthropic.com")
}

func cursorModelList(req DiscoverModelsRequest) bool {
	if strings.EqualFold(strings.TrimSpace(req.Provider), "cursor") || strings.EqualFold(strings.TrimSpace(req.Adapter), "cursor") {
		return true
	}
	return strings.Contains(strings.ToLower(strings.TrimSpace(req.BaseURL)), "api2.cursor.sh")
}

func googleAIStudioModelList(req DiscoverModelsRequest) bool {
	base := strings.ToLower(strings.TrimSpace(req.BaseURL))
	if strings.Contains(base, "daily-cloudcode-pa.googleapis.com") || strings.Contains(base, "aiplatform.googleapis.com") {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(req.Provider), "google") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(req.Adapter), "google") && strings.Contains(base, "generativelanguage.googleapis.com") {
		return true
	}
	return strings.Contains(base, "generativelanguage.googleapis.com")
}

func commandCodeModelList(req DiscoverModelsRequest) bool {
	name := strings.ToLower(strings.TrimSpace(req.Provider))
	if name == "command-code" || name == "commandcode" {
		return true
	}
	host := strings.ToLower(strings.TrimSpace(req.BaseURL))
	return strings.Contains(host, "api.commandcode.ai")
}

func anthropicModelsURL(base string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(base), "/")
	if trimmed == "" {
		trimmed = "https://api.anthropic.com"
	}
	if strings.HasSuffix(trimmed, "/models") {
		return trimmed
	}
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed + "/models"
	}
	return trimmed + "/v1/models"
}

func googleAIStudioModelsURL(base string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(base), "/")
	if trimmed == "" {
		trimmed = "https://generativelanguage.googleapis.com"
	}
	if strings.HasSuffix(trimmed, "/models") {
		return trimmed
	}
	if strings.Contains(trimmed, "/v1beta") {
		return trimmed + "/models"
	}
	return trimmed + "/v1beta/models"
}

func modelsListURL(req DiscoverModelsRequest) string {
	if strings.EqualFold(strings.TrimSpace(req.AuthMode), "forward") {
		base := strings.ToLower(strings.TrimSpace(req.BaseURL))
		if strings.Contains(base, "chatgpt.com") && strings.Contains(base, "/backend-api/codex") {
			return codexModelsURL
		}
	}
	if commandCodeModelList(req) {
		return commandCodeModelsURL
	}
	if anthropicModelList(req) {
		return anthropicModelsURL(req.BaseURL)
	}
	if googleAIStudioModelList(req) {
		return googleAIStudioModelsURL(req.BaseURL)
	}
	if cursorModelList(req) {
		return ""
	}
	trimmed := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimSuffix(trimmed, "/chat/completions")
	trimmed = strings.TrimSuffix(trimmed, "/responses")
	trimmed = strings.TrimRight(trimmed, "/")
	if strings.HasSuffix(trimmed, "/models") {
		return trimmed
	}
	return trimmed + "/models"
}
