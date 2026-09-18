package config

import (
	"bytes"
	"encoding/json"
	"strconv"
)

func projectMaxUpstreamBodyBytes(provider map[string]json.RawMessage) (int64, bool, bool) {
	raw, exists := provider["maxUpstreamBodyBytes"]
	if !exists {
		return 0, false, true
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return 0, true, false
	}
	if trimmed[0] != '-' && (trimmed[0] < '0' || trimmed[0] > '9') {
		return 0, true, false
	}
	var value json.Number
	if json.Unmarshal(trimmed, &value) != nil {
		return 0, true, false
	}
	parsed, err := strconv.ParseInt(value.String(), 10, 64)
	if err != nil || parsed < 0 {
		return 0, true, false
	}
	return parsed, true, true
}
