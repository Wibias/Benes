package codexauth

import (
	"encoding/json"
	"fmt"
	"math"
)

type PoolRoutingPolicy struct {
	Strategy            PoolStrategy
	ResetOrder          ResetOrder
	StickyLimit         int
	AutoSwitchThreshold *float64
	FailoverThreshold   *int
}

func ProjectPoolRoutingPolicy(raw []byte) (PoolRoutingPolicy, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil || root == nil {
		if err == nil {
			err = fmt.Errorf("root must be an object")
		}
		return PoolRoutingPolicy{}, fmt.Errorf("decode Codex Pool routing policy: %w", err)
	}

	var policy PoolRoutingPolicy
	if value, ok := root["accountPoolStrategy"]; ok {
		strategy, err := decodePoolStrategy(value)
		if err != nil {
			return PoolRoutingPolicy{}, err
		}
		policy.Strategy = strategy
	}
	if value, ok := root["accountPoolResetOrder"]; ok {
		order, err := decodeResetOrder(value)
		if err != nil {
			return PoolRoutingPolicy{}, err
		}
		policy.ResetOrder = order
	}
	if value, ok := root["accountPoolStickyLimit"]; ok {
		sticky, err := decodeBoundedInteger(value, "accountPoolStickyLimit", 1, 100)
		if err != nil {
			return PoolRoutingPolicy{}, err
		}
		policy.StickyLimit = sticky
	}
	if value, ok := root["autoSwitchThreshold"]; ok {
		threshold, err := decodeBoundedInteger(value, "autoSwitchThreshold", 0, 100)
		if err != nil {
			return PoolRoutingPolicy{}, err
		}
		converted := float64(threshold)
		policy.AutoSwitchThreshold = &converted
	}
	if value, ok := root["upstreamFailoverThreshold"]; ok {
		threshold, err := decodeBoundedInteger(value, "upstreamFailoverThreshold", 0, 20)
		if err != nil {
			return PoolRoutingPolicy{}, err
		}
		policy.FailoverThreshold = &threshold
	}
	return policy, nil
}

func decodePoolStrategy(raw json.RawMessage) (PoolStrategy, error) {
	if isJSONNullBytes(raw) {
		return "", fmt.Errorf("decode Codex Pool routing policy: accountPoolStrategy must be quota, round-robin, fill-first, or reset-window")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("decode Codex Pool routing policy: accountPoolStrategy must be a string")
	}
	strategy := PoolStrategy(value)
	switch strategy {
	case PoolStrategyQuota, PoolStrategyRoundRobin, PoolStrategyFillFirst, PoolStrategyResetWindow:
		return strategy, nil
	default:
		return "", fmt.Errorf("decode Codex Pool routing policy: accountPoolStrategy must be quota, round-robin, fill-first, or reset-window")
	}
}

func decodeResetOrder(raw json.RawMessage) (ResetOrder, error) {
	if isJSONNullBytes(raw) {
		return "", fmt.Errorf("decode Codex Pool routing policy: accountPoolResetOrder must be soonest or latest")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("decode Codex Pool routing policy: accountPoolResetOrder must be a string")
	}
	order := ResetOrder(value)
	switch order {
	case ResetOrderSoonest, ResetOrderLatest:
		return order, nil
	default:
		return "", fmt.Errorf("decode Codex Pool routing policy: accountPoolResetOrder must be soonest or latest")
	}
}

func decodeBoundedInteger(raw json.RawMessage, field string, minimum, maximum int) (int, error) {
	if isJSONNullBytes(raw) {
		return 0, fmt.Errorf("decode Codex Pool routing policy: %s must be an integer from %d to %d", field, minimum, maximum)
	}
	var value float64
	if err := json.Unmarshal(raw, &value); err != nil ||
		math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value ||
		value < float64(minimum) || value > float64(maximum) {
		return 0, fmt.Errorf("decode Codex Pool routing policy: %s must be an integer from %d to %d", field, minimum, maximum)
	}
	return int(value), nil
}
