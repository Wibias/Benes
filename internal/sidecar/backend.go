package sidecar

import (
	"errors"
	"strings"
)

var (
	ErrUnprovenCapability = errors.New("sidecar backend capability is unknown or unproven")
	ErrAmbiguousModel     = errors.New("sidecar model is ambiguous across providers")
	ErrUnsupportedBackend = errors.New("sidecar backend is not enabled for this selection")
)

type Modality string

const (
	ModalityWebSearch      Modality = "web_search"
	ModalityVisionDescribe Modality = "vision_describe"
)

type Class string

const (
	ClassDedicatedSearch      Class = "dedicated_search"
	ClassProviderNativeSearch Class = "provider_native_search"
	ClassVisionDescribe       Class = "vision_describe"
)

type Candidate struct {
	Modality    Modality `json:"modality"`
	Class       Class    `json:"class"`
	ProviderID  string   `json:"provider"`
	Destination string   `json:"destination,omitempty"`
	ModelID     string   `json:"model"`
	AuthClass   string   `json:"authClass,omitempty"`
	Wire        string   `json:"wire,omitempty"`
	Citations   bool     `json:"citations,omitempty"`
	ImageInput  bool     `json:"imageInput,omitempty"`
	Proven      bool     `json:"proven"`
}

type Selection struct {
	Modality    Modality
	ProviderID  string
	ModelID     string
	Destination string
	Backend     string
	Candidates  []Candidate
}

func NormalizeBackend(raw string, modality Modality) (Class, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return "", nil
	case "openai":
		if modality == ModalityVisionDescribe {
			return ClassVisionDescribe, nil
		}
		return ClassDedicatedSearch, nil
	case "anthropic":
		if modality == ModalityVisionDescribe {
			return ClassVisionDescribe, nil
		}
		return ClassProviderNativeSearch, nil
	case string(ClassDedicatedSearch):
		if modality == ModalityVisionDescribe {
			return "", ErrUnsupportedBackend
		}
		return ClassDedicatedSearch, nil
	case string(ClassProviderNativeSearch):
		if modality == ModalityVisionDescribe {
			return "", ErrUnsupportedBackend
		}
		return ClassProviderNativeSearch, nil
	case string(ClassVisionDescribe):
		if modality != ModalityVisionDescribe && modality != "" {
			return "", ErrUnsupportedBackend
		}
		return ClassVisionDescribe, nil
	default:
		return "", ErrUnsupportedBackend
	}
}
func ClassForWire(wire string) Class {
	switch strings.ToLower(strings.TrimSpace(wire)) {
	case "openai-responses", "search-json":
		return ClassDedicatedSearch
	case "anthropic-messages":
		return ClassProviderNativeSearch
	default:
		return ""
	}
}

func Resolve(sel Selection) (Candidate, error) {
	wantClass, err := NormalizeBackend(sel.Backend, sel.Modality)
	if err != nil {
		return Candidate{}, err
	}
	provider := strings.TrimSpace(sel.ProviderID)
	model := strings.TrimSpace(sel.ModelID)
	if provider == "" && strings.Contains(model, "/") {
		provider, model = splitNamespaced(model)
	}
	matches := make([]Candidate, 0, len(sel.Candidates))
	for _, item := range sel.Candidates {
		if sel.Modality != "" && item.Modality != sel.Modality {
			continue
		}
		if wantClass != "" && item.Class != wantClass {
			continue
		}
		if provider != "" && item.ProviderID != provider {
			continue
		}
		if dest := strings.TrimSpace(sel.Destination); dest != "" && strings.TrimSpace(item.Destination) != "" && dest != item.Destination {
			continue
		}
		if model != "" && item.ModelID != model && item.ProviderID+"/"+item.ModelID != strings.TrimSpace(sel.ModelID) {
			continue
		}
		matches = append(matches, item)
	}
	if model != "" && provider == "" {
		uniqueProviders := map[string]struct{}{}
		for _, item := range matches {
			uniqueProviders[item.ProviderID] = struct{}{}
		}
		if len(uniqueProviders) > 1 {
			return Candidate{}, ErrAmbiguousModel
		}
	}
	if len(matches) != 1 {
		return Candidate{}, ErrUnsupportedBackend
	}
	got := matches[0]
	if !got.Proven {
		return Candidate{}, ErrUnprovenCapability
	}
	if sel.Modality == ModalityVisionDescribe && !got.ImageInput {
		return Candidate{}, ErrUnprovenCapability
	}
	return got, nil
}

func Catalog(candidates []Candidate, modality Modality) []Candidate {
	out := make([]Candidate, 0, len(candidates))
	for _, item := range candidates {
		if modality != "" && item.Modality != modality {
			continue
		}
		if !item.Proven {
			continue
		}
		out = append(out, item)
	}
	return out
}

func BoundSources(sources []map[string]string, max int) []map[string]string {
	if max <= 0 {
		max = 8
	}
	if len(sources) <= max {
		return sources
	}
	return sources[:max]
}

func BoundText(text string, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = 4000
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}

func splitNamespaced(id string) (provider, model string) {
	i := strings.Index(id, "/")
	if i <= 0 || i == len(id)-1 {
		return "", id
	}
	return id[:i], id[i+1:]
}
