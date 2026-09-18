package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

func positiveIntegerField(root rec, name string) (float64, bool) {
	raw, exists := root[name]
	if !exists {
		return 0, false
	}
	value, ok := numberFromRaw(raw)
	if !ok || value <= 0 || math.Trunc(value) != value {
		return 0, false
	}
	return value, true
}

func positiveNumberField(root rec, name string) (float64, bool) {
	raw, exists := root[name]
	if !exists {
		return 0, false
	}
	value, ok := numberFromRaw(raw)
	return value, ok && value > 0
}

func optionalBoolField(root rec, name string) (bool, bool, error) {
	raw, exists := root[name]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false, false, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, false, fmt.Errorf("%s must be a boolean", name)
	}
	return value, true, nil
}

func optionalNumber(root rec, name string) (float64, bool, error) {
	raw, exists := root[name]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, false, nil
	}
	value, ok := numberFromRaw(raw)
	if !ok {
		return 0, false, fmt.Errorf("%s must be a number", name)
	}
	return value, true, nil
}

func numberFromRaw(raw json.RawMessage) (float64, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !((trimmed[0] >= '0' && trimmed[0] <= '9') || trimmed[0] == '-') {
		return 0, false
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil {
		return 0, false
	}
	value, err := number.Float64()
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

func stringArrayField(root rec, name string) ([]string, error) {
	raw, exists := root[name]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("%s must be an array of strings", name)
	}
	return values, nil
}

func stringField(root rec, name string) (string, bool) {
	raw, exists := root[name]
	if !exists {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}
