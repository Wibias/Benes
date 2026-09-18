package quota

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/providers/kiro"
)

const kiroUsageTarget = "AmazonCodeWhispererService.GetUsageLimits"

func probeKiro(provider Provider) probeResult {
	token := strings.TrimSpace(provider.AccessToken)
	if token == "" {
		token = strings.TrimSpace(provider.APIKey)
	}
	if token == "" {
		return probeResult{kind: probeNone}
	}
	region := kiro.RegionFromRuntimeURL(provider.BaseURL)
	if region == "" {
		return probeResult{kind: probeNone}
	}
	dest, err := kiro.ManagementURL(region)
	if err != nil {
		return probeResult{kind: probeNone}
	}
	body, _ := json.Marshal(map[string]string{"origin": "AI_EDITOR", "profileArn": strings.TrimSpace(provider.AccountID)})
	extra := make(http.Header)
	extra.Set("Content-Type", "application/json")
	extra.Set("x-amz-target", kiroUsageTarget)
	status, payload, err := bearerJSON(http.MethodPost, dest, token, body, extra)
	if res := httpProbe(status, err); res.kind != probeFresh {
		return res
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return probeResult{kind: probeNone}
	}
	limits, err := kiro.ParseUsageLimits(raw)
	if err != nil {
		return probeResult{kind: probeNone}
	}
	out := Quota{
		MonthlyPercent: ptr(limits.MonthlyPercent),
		MonthlyResetAt: limits.MonthlyResetAt,
		OverageEnabled: limits.OverageEnabled,
		UpdatedAt:      Now().UnixMilli(),
	}
	if limits.HasTrial {
		out.CustomWindows = []Window{{Label: "Trial", Percent: limits.TrialPercent, ResetAt: limits.TrialResetAt}}
	}
	report := makeReport(provider.Name, "kiro:get-usage-limits", out)
	report.AccountID = strings.TrimSpace(provider.AccountID)
	return probeResult{kind: probeFresh, report: report}
}
