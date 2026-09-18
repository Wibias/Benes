package websearch

import (
	"encoding/json"
	"net/url"
	"strings"
)

const (
	maxCitationSources         = 8
	maxCitationURLBytes        = 2048
	maxCitationTitleBytes      = 256
	maxCitationSerializedBytes = 16384
)

type citationBudgetSource struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

func SanitizeSources(sources []Source) []Source {
	out := make([]Source, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	used := 0
	for _, source := range sources {
		if len(out) >= maxCitationSources {
			break
		}
		next, ok := sanitizeSource(source, seen)
		if !ok {
			continue
		}
		encoded, err := json.Marshal(citationBudgetSource{URL: next.URL, Title: next.Title})
		if err != nil || used+len(encoded) > maxCitationSerializedBytes {
			continue
		}
		out = append(out, next)
		seen[next.URL] = struct{}{}
		used += len(encoded)
	}
	return out
}

func sanitizeSource(source Source, seen map[string]struct{}) (Source, bool) {
	raw := source.URL
	if raw == "" || strings.TrimSpace(raw) != raw || hasCitationControl(raw) || len(raw) > maxCitationURLBytes {
		return Source{}, false
	}
	if _, dup := seen[raw]; dup {
		return Source{}, false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Hostname() == "" {
		return Source{}, false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Source{}, false
	}
	title := source.Title
	if strings.TrimSpace(title) == "" || hasCitationControl(title) || len(title) > maxCitationTitleBytes {
		title = ""
	}
	return Source{URL: raw, Title: title}, true
}

func hasCitationControl(value string) bool {
	for _, r := range value {
		if r <= 0x1F || (r >= 0x7F && r <= 0x9F) {
			return true
		}
	}
	return false
}
