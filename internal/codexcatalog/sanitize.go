package codexcatalog

import "strings"

// SupportedNativeOpenAISlugs are the bare ChatGPT/Codex ids this release can
// restore with authoritative metadata. Template selection for routed rows must
// stay inside this set so a client-injected Reserve fallback cannot become the
// clone source for every provider/model picker row.
var SupportedNativeOpenAISlugs = map[string]struct{}{
	"gpt-5.5":                  {},
	"gpt-5.4":                  {},
	"gpt-5.4-mini":             {},
	"gpt-5.3-codex-spark":      {},
	"gpt-5.6-sol":              {},
	"gpt-5.6-terra":            {},
	"gpt-5.6-luna":             {},
	"gpt-daybreak-blue-latest": {},
}

func IsRoutedSlug(slug string) bool {
	return strings.Contains(strings.TrimSpace(slug), "/")
}

func slugOf(entry map[string]any) string {
	slug, _ := entry["slug"].(string)
	return strings.TrimSpace(slug)
}

// SanitizeRouted drops ChatGPT plan-gating metadata from namespaced rows.
// Native rows keep their own eligibility fields.
func SanitizeRouted(entry map[string]any) {
	if entry == nil || !IsRoutedSlug(slugOf(entry)) {
		return
	}
	entry["supported_in_api"] = true
	delete(entry, "available_in_plans")
	delete(entry, "minimal_client_version")
	delete(entry, "availability_nux")
	delete(entry, "upgrade")
}

// FindSupportedNativeTemplate returns a clone of the first roster-supported
// native row that still looks like a real Codex catalog template. Unknown bare
// rows (including gpt-reserve) are skipped: cloning them would copy native
// eligibility onto every routed model.
func FindSupportedNativeTemplate(models []map[string]any) map[string]any {
	for _, model := range models {
		slug := slugOf(model)
		if slug == "" || IsRoutedSlug(slug) {
			continue
		}
		if _, ok := SupportedNativeOpenAISlugs[slug]; !ok {
			continue
		}
		if _, ok := model["base_instructions"]; !ok {
			continue
		}
		return cloneEntry(model)
	}
	return nil
}
