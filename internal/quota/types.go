package quota

import (
	"os"
	"strings"
	"time"
)

const (
	LastGoodMaxAge     = 30 * time.Minute
	CacheTTL           = 5 * time.Minute
	RequestTimeout     = 8 * time.Second
	ResponseMaxBytes   = 512 * 1024
	OpenRouterBase     = "https://openrouter.ai/api/v1"
	DeepSeekBase       = "https://api.deepseek.com"
	CommandCodeBase    = "https://api.commandcode.ai"
	CursorAPIBase      = "https://api2.cursor.sh"
	AntigravityAPIBase = "https://daily-cloudcode-pa.googleapis.com"
	KimiCodeBase       = "https://api.kimi.com/coding/v1"
	XAIBillingBase     = "https://cli-chat-proxy.grok.com/v1/billing"
	AnthropicAPIBase   = "https://api.anthropic.com"
)

type Window struct {
	Label   string  `json:"label"`
	Percent float64 `json:"percent"`
	ResetAt int64   `json:"resetAt,omitempty"`
}

type CreditsUsd struct {
	Used      float64 `json:"used"`
	Limit     float64 `json:"limit"`
	Remaining float64 `json:"remaining"`
	Percent   float64 `json:"percent"`
	ExpiresAt int64   `json:"expiresAt,omitempty"`
	Unlimited bool    `json:"unlimited,omitempty"`
}

type Quota struct {
	FiveHourPercent *float64    `json:"fiveHourPercent,omitempty"`
	FiveHourResetAt int64       `json:"fiveHourResetAt,omitempty"`
	WeeklyPercent   *float64    `json:"weeklyPercent,omitempty"`
	WeeklyResetAt   int64       `json:"weeklyResetAt,omitempty"`
	MonthlyPercent  *float64    `json:"monthlyPercent,omitempty"`
	MonthlyResetAt  int64       `json:"monthlyResetAt,omitempty"`
	CustomWindows   []Window    `json:"customWindows,omitempty"`
	CreditsUsd      *CreditsUsd `json:"creditsUsd,omitempty"`
	OverageEnabled  bool        `json:"overageEnabled,omitempty"`
	UpdatedAt       int64       `json:"updatedAt"`
}

const (
	A6APIBase      = "https://api.a6api.com"
	OpenCodeGoBase = "https://opencode.ai/zen/go/v1"
	ClineBase      = "https://api.cline.bot"
	ZaiBase        = "https://api.z.ai"
	MinimaxIO      = "https://api.minimax.io/v1"
	MinimaxCN      = "https://api.minimaxi.com/v1"
	MoonshotAI     = "https://api.moonshot.ai/v1"
	MoonshotCN     = "https://api.moonshot.cn/v1"
	VeniceBase     = "https://api.venice.ai/api/v1"
	SyntheticBase  = "https://api.synthetic.new/v2"
	DeepInfraBase  = "https://api.deepinfra.com"
	NeuralwattBase = "https://api.neuralwatt.com/v1"
)

// Entitlement holds provider-native plan/subscription timing. These fields are
// distinct from OAuth/API-key expiry and from per-window quota resets. Missing
// evidence is omitted; callers must not invent a placeholder date.
type Entitlement struct {
	CredentialExpiresAt   int64  `json:"credentialExpiresAt,omitempty"`
	QuotaResetAt          int64  `json:"quotaResetAt,omitempty"`
	BillingPeriodEndsAt   int64  `json:"billingPeriodEndsAt,omitempty"`
	PlanRenewsAt          int64  `json:"planRenewsAt,omitempty"`
	SubscriptionExpiresAt int64  `json:"subscriptionExpiresAt,omitempty"`
	EntitlementExpiresAt  int64  `json:"entitlementExpiresAt,omitempty"`
	Source                string `json:"source,omitempty"`
	ObservedAt            int64  `json:"observedAt,omitempty"`
	Confidence            string `json:"confidence,omitempty"`
	AccountID             string `json:"accountId,omitempty"`
}

func (e Entitlement) unused() bool {
	return e.CredentialExpiresAt == 0 && e.QuotaResetAt == 0 && e.BillingPeriodEndsAt == 0 &&
		e.PlanRenewsAt == 0 && e.SubscriptionExpiresAt == 0 && e.EntitlementExpiresAt == 0
}

type Report struct {
	Provider          string       `json:"provider"`
	Label             string       `json:"label"`
	Source            string       `json:"source"`
	AccountID         string       `json:"accountId,omitempty"`
	Quota             Quota        `json:"quota"`
	Entitlement       *Entitlement `json:"entitlement,omitempty"`
	UpdatedAt         int64        `json:"updatedAt"`
	ReverseEngineered bool         `json:"reverseEngineered,omitempty"`
}

func (r Report) Identity() string {
	if id := strings.TrimSpace(r.AccountID); id != "" {
		return r.Provider + "\x00" + id
	}
	return r.Provider
}

type Response struct {
	GeneratedAt int64    `json:"generatedAt"`
	Reports     []Report `json:"reports"`
}

type Provider struct {
	Name        string
	APIKey      string
	BaseURL     string
	AuthMode    string
	Disabled    bool
	AccessToken string
	ProjectID   string
	AccountID   string
}

type probeKind int

const (
	probeNone probeKind = iota
	probeFresh
	probeTransient
	probeTerminal
)

type probeResult struct {
	kind   probeKind
	report Report
}

func normalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func resolveEnvValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		return strings.TrimSpace(os.Getenv(value[2 : len(value)-1]))
	}
	if strings.HasPrefix(value, "$") && !strings.ContainsAny(value, "/\\") {
		return strings.TrimSpace(os.Getenv(value[1:]))
	}
	return value
}

func canonicalOpenRouter(base string) bool {
	return normalizeBaseURL(base) == OpenRouterBase
}

func canonicalDeepSeek(base string) bool {
	n := normalizeBaseURL(base)
	return n == DeepSeekBase || n == DeepSeekBase+"/v1"
}

func authKey(provider Provider) bool {
	mode := strings.TrimSpace(provider.AuthMode)
	return mode == "" || mode == "key"
}

func oauthMode(provider Provider) bool {
	return strings.TrimSpace(provider.AuthMode) == "oauth"
}

func asFloat(v any) (float64, bool) {
	n, ok := v.(float64)
	if !ok {
		return 0, false
	}
	if n != n {
		return 0, false
	}
	return n, true
}

func asRecord(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
