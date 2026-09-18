package codexauth

import (
	"strings"
	"sync"
)

// MainPhysicalFence binds credential-derived Main runtime state to the physical
// account behind auth.json. The stable __main__ route alias remains the
// user-facing slot; replacement of that physical account must not inherit the
// prior account's health, reauth, failure, cooldown, or affinity evidence.
type MainPhysicalFence struct {
	mu       sync.Mutex
	identity string
	Health   *HealthState
	Failures *FailureState
	Reauth   *ReauthState
	Affinity *ThreadAffinityState
}

func NewMainPhysicalFence(health *HealthState, failures *FailureState, reauth *ReauthState, affinity *ThreadAffinityState) *MainPhysicalFence {
	return &MainPhysicalFence{Health: health, Failures: failures, Reauth: reauth, Affinity: affinity}
}

func (f *MainPhysicalFence) Bind(identity string) {
	if f == nil {
		return
	}
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.identity == identity {
		return
	}
	if f.identity != "" {
		f.clearMainLocked()
	}
	f.identity = identity
}

func (f *MainPhysicalFence) Accept(identity string) bool {
	if f == nil {
		return true
	}
	identity = strings.TrimSpace(identity)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.identity == "" {
		if identity != "" {
			f.identity = identity
		}
		return true
	}
	return identity == "" || identity == f.identity
}

func (f *MainPhysicalFence) Identity() string {
	if f == nil {
		return ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.identity
}

func (f *MainPhysicalFence) clearMainLocked() {
	if f.Health != nil {
		f.Health.ClearAccount(MainAccountID)
	}
	if f.Failures != nil {
		f.Failures.Clear(MainAccountID)
	}
	if f.Reauth != nil {
		f.Reauth.Clear(MainAccountID)
	}
	if f.Affinity != nil {
		f.Affinity.ClearAccount(MainAccountID)
	}
}
