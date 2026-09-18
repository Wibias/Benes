package google

import (
	"fmt"
	"strings"
)

type Kind string

const (
	KindAIStudio Kind = "ai-studio"
	KindVertex   Kind = "vertex"
)

type WireRenamePolicy int

const (
	// WireRenameDefault applies the known AI Studio Flash -tiered mappings.
	WireRenameDefault WireRenamePolicy = iota
	// WireRenameDisabled sends the picker-visible id unchanged.
	WireRenameDisabled
)

var directGeminiWireRenames = map[string]string{
	"gemini-3.7-flash": "gemini-3.7-flash-tiered",
	"gemini-3.6-flash": "gemini-3.6-flash-tiered",
}

type Identity struct {
	Kind     Kind
	PublicID string
	WireID   string
	Project  string
	Location string
}

func ResolveIdentity(kind Kind, requested string, policy WireRenamePolicy) (Identity, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return Identity{}, fmt.Errorf("Google model id is required")
	}
	id := Identity{Kind: kind, PublicID: requested, WireID: requested}
	if kind == KindVertex {
		return id, nil
	}
	if kind != KindAIStudio {
		return Identity{}, fmt.Errorf("Google kind %q is unsupported", kind)
	}
	if policy != WireRenameDisabled {
		if wire, ok := directGeminiWireRenames[requested]; ok {
			id.WireID = wire
		}
	}
	return id, nil
}

func ParseWireRenamePolicy(raw *bool) WireRenamePolicy {
	if raw != nil && !*raw {
		return WireRenameDisabled
	}
	return WireRenameDefault
}
