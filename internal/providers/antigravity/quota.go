package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

var (
	ErrQuotaUnreadable = errors.New("Cloud Code Assist quota response is unreadable")
	ErrQuotaOversized  = errors.New("Cloud Code Assist quota response is oversized")
)

const DefaultQuotaMaxBytes = 1 << 20

type QuotaWindow struct {
	Label   string
	Percent float64
	ResetAt *time.Time
}

type Quota struct {
	Windows   []QuotaWindow
	UpdatedAt time.Time
	Source    string
}

func ParseQuota(body []byte, maxBytes int64) (Quota, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultQuotaMaxBytes
	}
	if int64(len(body)) > maxBytes {
		return Quota{}, ErrQuotaOversized
	}
	if len(body) == 0 || !json.Valid(body) {
		return Quota{}, ErrQuotaUnreadable
	}
	var root struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if json.Unmarshal(body, &root) != nil || root.Models == nil {
		return Quota{}, ErrQuotaUnreadable
	}
	seen := map[string]struct{}{}
	out := Quota{Source: "live", UpdatedAt: time.Now().UTC()}
	for modelID, raw := range root.Models {
		var model struct {
			DisplayName string            `json:"displayName"`
			QuotaInfo   []json.RawMessage `json:"quotaInfo"`
		}
		if json.Unmarshal(raw, &model) != nil {
			continue
		}
		for _, quotaRaw := range model.QuotaInfo {
			var info struct {
				Tier                string   `json:"tier"`
				RemainingFraction   *float64 `json:"remainingFraction"`
				RemainingPercentage *float64 `json:"remainingPercentage"`
				ResetTime           string   `json:"resetTime"`
			}
			if json.Unmarshal(quotaRaw, &info) != nil {
				continue
			}
			label := classifyFamily(modelID, model.DisplayName, info.Tier)
			if label == "" {
				continue
			}
			if _, exists := seen[label]; exists {
				continue
			}
			percent, ok := usedPercent(info.RemainingFraction, info.RemainingPercentage)
			if !ok {
				continue
			}
			seen[label] = struct{}{}
			window := QuotaWindow{Label: label, Percent: percent}
			if reset, err := time.Parse(time.RFC3339, info.ResetTime); err == nil {
				window.ResetAt = &reset
			}
			out.Windows = append(out.Windows, window)
		}
	}
	if len(out.Windows) == 0 {
		return Quota{}, ErrQuotaUnreadable
	}
	return out, nil
}

func FetchQuota(ctx context.Context, client *http.Client, endpoint, token, project string) (Quota, error) {
	if client == nil {
		return Quota{}, ErrQuotaUnreadable
	}
	dest, err := ResolveDestination(endpoint)
	if err != nil {
		return Quota{}, err
	}
	body, err := json.Marshal(map[string]any{"project": project})
	if err != nil {
		return Quota{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dest+"/v1internal:fetchAvailableModels", bytes.NewReader(body))
	if err != nil {
		return Quota{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Quota{}, ErrQuotaUnreadable
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, DefaultQuotaMaxBytes+1))
	if err != nil {
		return Quota{}, ErrQuotaUnreadable
	}
	live, err := ParseQuota(raw, DefaultQuotaMaxBytes)
	if err != nil {
		return Quota{}, err
	}
	return live, nil
}

func MergeQuota(live, catalog Quota) Quota {
	if len(live.Windows) > 0 && live.Source == "live" {
		return live
	}
	return catalog
}

func classifyFamily(modelID, displayName, tier string) string {
	hay := strings.ToLower(modelID + " " + displayName + " " + tier)
	switch {
	case strings.Contains(hay, "gemini"):
		return "Gem"
	case strings.Contains(hay, "claude") || strings.Contains(hay, "opus") || strings.Contains(hay, "sonnet") || strings.Contains(hay, "gpt-oss"):
		return "Cla"
	default:
		return ""
	}
}

func usedPercent(fraction, percentage *float64) (float64, bool) {
	var remaining *float64
	if fraction != nil && !math.IsNaN(*fraction) && !math.IsInf(*fraction, 0) {
		v := *fraction * 100
		remaining = &v
	} else if percentage != nil && !math.IsNaN(*percentage) && !math.IsInf(*percentage, 0) {
		remaining = percentage
	}
	if remaining == nil {
		return 0, false
	}
	used := 100 - *remaining
	if used < 0 {
		used = 0
	}
	if used > 100 {
		used = 100
	}
	return used, true
}
