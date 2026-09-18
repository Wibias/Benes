package config

import (
	"encoding/json"
	"math"
	"strings"
	"time"
)

type providerPacingRule struct {
	RequestsPerMinute *float64 `json:"requestsPerMinute"`
	MinIntervalMs     *float64 `json:"minIntervalMs"`
}

type providerPacingConfig struct {
	Enabled *bool                         `json:"enabled"`
	providerPacingRule
	Models map[string]providerPacingRule `json:"models"`
}

func projectProviderPacing(provider map[string]json.RawMessage) (time.Duration, map[string]time.Duration, bool) {
	if raw, exists := provider["requestPacing"]; exists {
		var cfg providerPacingConfig
		if json.Unmarshal(raw, &cfg) != nil {
			return 0, nil, false
		}
		if cfg.Enabled != nil && !*cfg.Enabled {
			return 0, nil, true
		}
		base, ok := effectivePacingInterval(cfg.providerPacingRule)
		if !ok {
			return 0, nil, false
		}
		models := map[string]time.Duration{}
		for rawModel, override := range cfg.Models {
			model := strings.TrimSpace(rawModel)
			if model == "" {
				return 0, nil, false
			}
			merged := cfg.providerPacingRule
			if override.RequestsPerMinute != nil {
				merged.RequestsPerMinute = override.RequestsPerMinute
			}
			if override.MinIntervalMs != nil {
				merged.MinIntervalMs = override.MinIntervalMs
			}
			interval, valid := effectivePacingInterval(merged)
			if !valid {
				return 0, nil, false
			}
			models[model] = interval
		}
		if len(models) == 0 {
			models = nil
		}
		return base, models, true
	}

	if raw, exists := provider["requestPacingMs"]; exists {
		var ms float64
		if json.Unmarshal(raw, &ms) != nil || ms < 0 {
			return 0, nil, false
		}
		if ms == 0 {
			return 0, nil, true
		}
		return durationFromMilliseconds(ms), nil, true
	}
	return 0, nil, true
}

func effectivePacingInterval(rule providerPacingRule) (time.Duration, bool) {
	interval := time.Duration(0)
	if rule.RequestsPerMinute != nil {
		rpm := *rule.RequestsPerMinute
		if !finitePositive(rpm) {
			return 0, false
		}
		fromRPM := time.Duration(math.Ceil(float64(time.Minute) / rpm))
		if fromRPM > interval {
			interval = fromRPM
		}
	}
	if rule.MinIntervalMs != nil {
		ms := *rule.MinIntervalMs
		if !finitePositive(ms) {
			return 0, false
		}
		fromDelay := durationFromMilliseconds(ms)
		if fromDelay > interval {
			interval = fromDelay
		}
	}
	return interval, true
}

func durationFromMilliseconds(ms float64) time.Duration {
	return time.Duration(math.Ceil(ms * float64(time.Millisecond)))
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
