package grok

import "strings"

// Registration describes how much of the canonical Benes catalogue the managed block
// carries right now. It is always derived, never stored: the block is the only Grok-side
// state Benes keeps, and the Benes catalogue is the only place model membership is decided.
type Registration struct {
	ConfigPath string `json:"configPath"`
	// The managed block exists in the Grok config.
	Present bool `json:"present"`
	// Loopback base URL the block points at, when it is written.
	BaseURL string `json:"baseUrl"`
	// Distinct model ids the catalogue offers.
	Catalogue int `json:"catalogue"`
	// Distinct model ids the block registers.
	Registered int `json:"registered"`
	// Present, carrying exactly the catalogue's model set, and pointing at expectedBaseURL.
	Current bool `json:"current"`
}

// RegistrationFor compares the catalogue with what the config file carries.
//
// expectedBaseURL is the address a write would use today. An empty string means the caller
// has no listener to compare against (the offline CLI), and then only the model set is
// judged: a block that points at some other address is not current even when its models
// match, because Grok would be calling somewhere else.
func RegistrationFor(models []Model, status Status, expectedBaseURL string) Registration {
	wanted := modelIDSet(models)
	have := make(map[string]struct{}, len(status.Models))
	for _, model := range status.Models {
		if id := strings.TrimSpace(model.ID); id != "" {
			have[id] = struct{}{}
		}
	}
	missing := 0
	for id := range wanted {
		if _, ok := have[id]; !ok {
			missing++
		}
	}
	unlisted := 0
	for id := range have {
		if _, ok := wanted[id]; !ok {
			unlisted++
		}
	}
	current := status.Present && missing == 0 && unlisted == 0
	if current && expectedBaseURL != "" && status.BaseURL != expectedBaseURL {
		current = false
	}
	return Registration{
		ConfigPath: status.ConfigPath,
		Present:    status.Present,
		BaseURL:    status.BaseURL,
		Catalogue:  len(wanted),
		Registered: len(have),
		Current:    current,
	}
}

func modelIDSet(models []Model) map[string]struct{} {
	set := make(map[string]struct{}, len(models))
	for _, model := range models {
		if id := strings.TrimSpace(model.ID); id != "" {
			set[id] = struct{}{}
		}
	}
	return set
}
