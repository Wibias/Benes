package requestpolicy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
)

// Policy is the single Benes-owned request policy applied before provider capability gates.
type Policy struct {
	ServiceTier string `json:"serviceTier,omitempty"`
}

var current atomic.Value

func init() {
	current.Store(Policy{})
}

func (p Policy) Validate() error {
	switch p.ServiceTier {
	case "", "auto", "default", "flex", "priority":
		return nil
	default:
		return fmt.Errorf("requestPolicy.serviceTier must be auto, default, flex, priority, or unset")
	}
}

func (p Policy) Empty() bool {
	return p.ServiceTier == ""
}

// ResolveServiceTier preserves explicit client intent byte-for-byte and otherwise returns the configured default.
func (p Policy) ResolveServiceTier(explicit *string) *string {
	return ResolveServiceTier(explicit, p.ServiceTier)
}

// ResolveServiceTier applies the approved precedence for one admitted request: an
// explicit client tier is preserved byte-for-byte, otherwise the tier admitted for
// this request applies, otherwise Benes sends no tier at all. It never reads live
// settings, so a request in flight keeps the policy it was admitted with.
func ResolveServiceTier(explicit *string, configuredServiceTier string) *string {
	if explicit != nil && strings.TrimSpace(*explicit) != "" {
		value := *explicit
		return &value
	}
	value := strings.TrimSpace(configuredServiceTier)
	if value == "" {
		return nil
	}
	return &value
}

func Decode(raw json.RawMessage) (Policy, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return Policy{}, nil
	}
	var p Policy
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return Policy{}, fmt.Errorf("requestPolicy must be an object with an optional serviceTier: %w", err)
	}
	if err := p.Validate(); err != nil {
		return Policy{}, err
	}
	return p, nil
}

// Publish makes a validated persisted policy the process-wide request default.
func Publish(p Policy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	current.Store(p)
	return nil
}

func Current() Policy {
	value, _ := current.Load().(Policy)
	return value
}

// AdmittedServiceTier freezes the configured service tier for one logical request.
// Call it exactly once at the request boundary and carry the result on the request /
// dispatch state: retries, failovers, combos, and continuations must reuse that value
// instead of reading live settings again. A settings change therefore affects only
// requests admitted afterwards.
func AdmittedServiceTier() string {
	return strings.TrimSpace(Current().ServiceTier)
}
