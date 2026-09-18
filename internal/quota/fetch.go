package quota

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	HTTPClient HTTPDoer = &http.Client{
		Timeout: RequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	Now = time.Now
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Store struct {
	mu    sync.Mutex
	key   string
	ts    time.Time
	value Response
}

func cacheKey(providers []Provider) string {
	raw, _ := json.Marshal(providers)
	return string(raw)
}

func FetchReports(store *Store, providers []Provider, force bool) Response {
	now := Now()
	key := cacheKey(providers)
	if store != nil && !force {
		store.mu.Lock()
		fresh := store.key == key && now.Sub(store.ts) < CacheTTL && reportsCurrent(store.value.Reports, now)
		cached := store.value
		store.mu.Unlock()
		if fresh {
			return cached
		}
	}
	previous := []Report{}
	if store != nil {
		store.mu.Lock()
		if store.key == key {
			previous = append([]Report{}, store.value.Reports...)
		}
		store.mu.Unlock()
	}
	fresh := make([]Report, 0, len(providers))
	terminal := map[string]struct{}{}
	for _, provider := range providers {
		if provider.Disabled {
			continue
		}
		result := probeProvider(provider)
		switch result.kind {
		case probeFresh:
			fresh = append(fresh, bindReport(result.report, provider, result.report.Entitlement))
		case probeTerminal:
			terminal[providerIdentity(provider)] = struct{}{}
		}
	}
	cutoff := now.Add(-LastGoodMaxAge).UnixMilli()
	byProvider := map[string]Report{}
	for _, item := range previous {
		if item.UpdatedAt < cutoff {
			continue
		}
		byProvider[item.Identity()] = item
	}
	for _, item := range fresh {
		byProvider[item.Identity()] = item
	}
	for name := range terminal {
		delete(byProvider, name)
	}
	names := make([]string, 0, len(byProvider))
	for name := range byProvider {
		names = append(names, name)
	}
	sort.Strings(names)
	reports := make([]Report, 0, len(names))
	for _, name := range names {
		reports = append(reports, byProvider[name])
	}
	out := Response{GeneratedAt: now.UnixMilli(), Reports: reports}
	if store != nil {
		store.mu.Lock()
		store.key = key
		store.ts = now
		store.value = out
		store.mu.Unlock()
	}
	return out
}

func reportsCurrent(reports []Report, now time.Time) bool {
	cutoff := now.Add(-LastGoodMaxAge).UnixMilli()
	for _, item := range reports {
		if item.UpdatedAt < cutoff {
			return false
		}
	}
	return true
}

func probeProvider(provider Provider) probeResult {
	if oauthMode(provider) {
		switch provider.Name {
		case "command-code":
			return probeCommandCode(provider)
		case "cursor":
			return probeCursor(provider)
		case "google-antigravity":
			return probeAntigravity(provider)
		case "kimi":
			return probeKimi(provider)
		case "xai":
			return probeXai(provider)
		case "anthropic":
			return probeAnthropic(provider)
		case "kiro":
			return probeKiro(provider)
		default:
			return probeResult{kind: probeNone}
		}
	}
	if !authKey(provider) {
		return probeResult{kind: probeNone}
	}
	if provider.Name == "commandcode" {
		return probeCommandCode(provider)
	}
	if canonicalKimi(provider.BaseURL) {
		return probeKimi(provider)
	}
	switch {
	case canonicalOpenRouter(provider.BaseURL):
		return probeOpenRouter(provider)
	case canonicalDeepSeek(provider.BaseURL):
		return probeDeepSeek(provider)
	case canonicalMoonshot(provider.BaseURL):
		return probeMoonshot(provider)
	case canonicalVenice(provider.BaseURL):
		return probeVenice(provider)
	case canonicalCline(provider.BaseURL):
		return probeCline(provider)
	case canonicalMinimax(provider.BaseURL):
		return probeMinimax(provider)
	case canonicalDeepInfra(provider.BaseURL):
		return probeDeepInfra(provider)
	case canonicalNeuralwatt(provider.BaseURL):
		return probeNeuralwatt(provider)
	case canonicalSynthetic(provider.BaseURL):
		return probeSynthetic(provider)
	case canonicalOpenCodeGo(provider.BaseURL):
		return probeOpenCodeGo(provider)
	case canonicalA6api(provider.BaseURL):
		return probeA6api(provider)
	case canonicalZai(provider.BaseURL):
		return probeZai(provider)
	default:
		return probeResult{kind: probeNone}
	}
}

func doJSON(req *http.Request) (int, map[string]any, error) {
	res, err := HTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, ResponseMaxBytes+1))
	if err != nil {
		return res.StatusCode, nil, err
	}
	if len(raw) > ResponseMaxBytes {
		return res.StatusCode, nil, io.ErrUnexpectedEOF
	}
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return res.StatusCode, nil, nil
	}
	return res.StatusCode, body, nil
}

func classifyHTTP(status int) probeKind {
	if status >= 400 && status < 500 && status != 408 && status != 429 {
		return probeTerminal
	}
	return probeTransient
}

func bearerGet(url, apiKey string) (int, map[string]any, error) {
	return bearerJSON(http.MethodGet, url, apiKey, nil, nil)
}

func bearerJSON(method, url, token string, body []byte, extra http.Header) (int, map[string]any, error) {
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	for key, values := range extra {
		for _, value := range values {
			req.Header.Set(key, value)
		}
	}
	return doJSON(req)
}

func makeReport(provider, source string, quota Quota) Report {
	return Report{
		Provider:  provider,
		Label:     provider,
		Source:    source,
		Quota:     quota,
		UpdatedAt: quota.UpdatedAt,
	}
}

func boundReport(provider Provider, source string, quota Quota, ent *Entitlement) Report {
	return bindReport(makeReport(provider.Name, source, quota), provider, ent)
}

func probeOpenRouter(provider Provider) probeResult {
	apiKey := resolveEnvValue(provider.APIKey)
	if apiKey == "" {
		return probeResult{kind: probeNone}
	}
	status, body, err := bearerGet(OpenRouterBase+"/key", apiKey)
	if err != nil || status == 0 {
		return probeResult{kind: probeTransient}
	}
	if status < 200 || status >= 300 {
		return probeResult{kind: classifyHTTP(status)}
	}
	data := asRecord(body["data"])
	if data == nil {
		data = body
	}
	if data == nil {
		return probeResult{kind: probeTransient}
	}
	limit, ok := asFloat(data["limit"])
	if !ok || limit <= 0 {
		return probeResult{kind: probeTerminal}
	}
	var used float64
	if remaining, ok := asFloat(data["limit_remaining"]); ok {
		used = limit - remaining
		if used < 0 {
			used = 0
		}
	} else if usage, ok := asFloat(data["usage"]); ok && usage >= 0 {
		used = usage
	} else {
		return probeResult{kind: probeTransient}
	}
	percent := (used / limit) * 100
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	now := Now().UnixMilli()
	label := "API credits ($" + formatMoney(remaining) + " of $" + formatMoney(limit) + " remaining)"
	return probeResult{kind: probeFresh, report: makeReport(provider.Name, "openrouter:key-info", Quota{
		CustomWindows: []Window{{Label: label, Percent: percent}},
		UpdatedAt:     now,
	})}
}

func probeDeepSeek(provider Provider) probeResult {
	apiKey := resolveEnvValue(provider.APIKey)
	if apiKey == "" {
		return probeResult{kind: probeNone}
	}
	status, body, err := bearerGet(DeepSeekBase+"/user/balance", apiKey)
	if err != nil || status == 0 {
		return probeResult{kind: probeTransient}
	}
	if status < 200 || status >= 300 {
		return probeResult{kind: classifyHTTP(status)}
	}
	rows, _ := body["balance_infos"].([]any)
	var preferred map[string]any
	for _, currency := range []string{"USD", "CNY"} {
		for _, raw := range rows {
			row := asRecord(raw)
			if row == nil {
				continue
			}
			if strings.EqualFold(stringValue(row["currency"]), currency) {
				preferred = row
				break
			}
		}
		if preferred != nil {
			break
		}
	}
	if preferred == nil && len(rows) > 0 {
		preferred = asRecord(rows[0])
	}
	if preferred == nil {
		return probeResult{kind: probeTransient}
	}
	balance, ok := asFloat(preferred["total_balance"])
	if !ok {
		balance, ok = asFloat(preferred["granted_balance"])
	}
	if !ok {
		balance, ok = asFloat(preferred["topped_up_balance"])
	}
	if !ok || balance < 0 {
		return probeResult{kind: probeTransient}
	}
	granted, hasGranted := asFloat(preferred["granted_balance"])
	label := "API balance ($" + formatMoney(balance) + ")"
	if hasGranted && granted > 0 {
		label = "API balance ($" + formatMoney(balance) + " total, $" + formatMoney(granted) + " granted)"
	}
	now := Now().UnixMilli()
	return probeResult{kind: probeFresh, report: makeReport(provider.Name, "deepseek:balance", Quota{
		CustomWindows: []Window{{Label: label, Percent: 0}},
		UpdatedAt:     now,
	})}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func formatMoney(n float64) string {
	return strconv.FormatFloat(n, 'f', 2, 64)
}
