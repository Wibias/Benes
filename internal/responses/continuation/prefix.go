package continuation

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
)

type Occurrence struct {
	Kind    string          `json:"kind"`
	ID      string          `json:"id,omitempty"`
	CallID  string          `json:"call_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Prefix struct {
	Items                  []Occurrence `json:"items"`
	ProviderOutputBoundary int          `json:"provider_output_boundary"`
}

type CompareLimits struct {
	MaxItemBytes int
	MaxDepth     int
}

// ShouldExpandPrefix returns true unless Benes can prove that the client already
// carries the exact stored prefix through the recorded provider-output occurrence.
// Any ambiguity fails closed to normal expansion rather than deleting history.
func ShouldExpandPrefix(stored Prefix, client []Occurrence, limits CompareLimits) bool {
	if limits.MaxItemBytes <= 0 {
		limits.MaxItemBytes = 1 << 20
	}
	if limits.MaxDepth <= 0 {
		limits.MaxDepth = 32
	}
	if !validPrefixAnchor(stored) || len(client) < len(stored.Items) {
		return true
	}

	for index, want := range stored.Items {
		got := client[index]
		if want.Kind != got.Kind || want.ID != got.ID || want.CallID != got.CallID {
			return true
		}
		equal, safe := boundedJSONEqual(want.Payload, got.Payload, limits)
		if !safe || !equal {
			return true
		}
	}

	anchorStored := stored.Items[stored.ProviderOutputBoundary]
	anchorClient := client[stored.ProviderOutputBoundary]
	if !sameProviderOccurrenceEvidence(anchorStored, anchorClient) {
		return true
	}
	return false
}

func validPrefixAnchor(prefix Prefix) bool {
	if prefix.ProviderOutputBoundary < 0 || prefix.ProviderOutputBoundary >= len(prefix.Items) {
		return false
	}
	anchor := prefix.Items[prefix.ProviderOutputBoundary]
	return strings.TrimSpace(anchor.ID) != "" || strings.TrimSpace(anchor.CallID) != ""
}

func sameProviderOccurrenceEvidence(left, right Occurrence) bool {
	if left.ID != "" {
		return left.ID == right.ID && right.ID != ""
	}
	if left.CallID != "" {
		return left.CallID == right.CallID && right.CallID != ""
	}
	return false
}

func boundedJSONEqual(left, right json.RawMessage, limits CompareLimits) (equal bool, safe bool) {
	left = bytes.TrimSpace(left)
	right = bytes.TrimSpace(right)
	if len(left) == 0 || len(right) == 0 {
		return len(left) == 0 && len(right) == 0, true
	}
	if len(left) > limits.MaxItemBytes || len(right) > limits.MaxItemBytes {
		return false, false
	}
	leftValue, ok := decodeBoundedJSON(left, limits.MaxDepth)
	if !ok {
		return false, false
	}
	rightValue, ok := decodeBoundedJSON(right, limits.MaxDepth)
	if !ok {
		return false, false
	}
	return reflect.DeepEqual(leftValue, rightValue), true
}

func decodeBoundedJSON(raw []byte, maxDepth int) (any, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, false
	}
	if depthOf(value) > maxDepth {
		return nil, false
	}
	return value, true
}

func depthOf(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		depth := 1
		for _, child := range typed {
			if childDepth := 1 + depthOf(child); childDepth > depth {
				depth = childDepth
			}
		}
		return depth
	case []any:
		depth := 1
		for _, child := range typed {
			if childDepth := 1 + depthOf(child); childDepth > depth {
				depth = childDepth
			}
		}
		return depth
	default:
		return 0
	}
}
