package codexcatalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/router"
	"github.com/Wibias/Benes/internal/store/atomicfile"
)

const catalogFileName = "benes-catalog.json"
const cacheFileName = "models_cache.json"

func CatalogPath(home string) string {
	return filepath.Join(home, catalogFileName)
}

// Write rebuilds CODEX_HOME/benes-catalog.json: keep native Codex rows, replace
// namespaced rows from the live Benes projection, and clone only a roster-supported
// native template so Reserve/unknown bare rows cannot leak ChatGPT eligibility.
func Write(home string, models []catalog.Model) (bool, error) {
	return WriteWithAliases(home, models, router.AliasTable{})
}

func WriteWithAliases(home string, models []catalog.Model, aliases router.AliasTable) (bool, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		return false, nil
	}
	existing := readExistingModels(home)
	natives := nativeRows(existing)
	template := FindSupportedNativeTemplate(existing)
	routed := deriveRoutedModels(models, template, aliases)
	if len(natives) == 0 && len(routed) == 0 {
		return false, nil
	}
	out := append([]map[string]any(nil), natives...)
	out = append(out, routed...)
	body, err := json.MarshalIndent(map[string]any{"models": out}, "", "  ")
	if err != nil {
		return false, err
	}
	if err := atomicfile.Write(CatalogPath(home), append(body, '\n'), atomicfile.Options{Mode: 0o600}); err != nil {
		return false, err
	}
	return true, nil
}

func deriveRoutedModels(models []catalog.Model, template map[string]any, aliases router.AliasTable) []map[string]any {
	out := make([]map[string]any, 0, len(models))
	seen := map[string]struct{}{}
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if !IsRoutedSlug(id) {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, deriveRouted(model, template, aliases))
	}
	return out
}

func deriveRouted(model catalog.Model, template map[string]any, aliases router.AliasTable) map[string]any {
	var entry map[string]any
	if template != nil {
		entry = cloneEntry(template)
	}
	if entry == nil {
		entry = map[string]any{
			"shell_type":        "unified_exec",
			"base_instructions": "You are a helpful coding assistant.",
		}
	}
	id := strings.TrimSpace(model.ID)
	entry["slug"] = id
	entry["display_name"] = pickerDisplayName(model, aliases)
	entry["visibility"] = "list"
	entry["description"] = "Routed via Benes"
	delete(entry, "benes_account_observed_native")
	if model.Context.Tokens > 0 {
		entry["context_window"] = model.Context.Tokens
	}
	SanitizeRouted(entry)
	return entry
}

func pickerDisplayName(model catalog.Model, aliases router.AliasTable) string {
	if name := strings.TrimSpace(model.DisplayName); name != "" {
		return name
	}
	id := strings.TrimSpace(model.ID)
	if display := strings.TrimSpace(aliases.DisplaySelector(id)); display != "" {
		return display
	}
	return id
}

func nativeRows(models []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(models))
	seen := map[string]struct{}{}
	for _, model := range models {
		slug := slugOf(model)
		if slug == "" || IsRoutedSlug(slug) {
			continue
		}
		if _, dup := seen[slug]; dup {
			continue
		}
		seen[slug] = struct{}{}
		cloned := cloneEntry(model)
		if cloned == nil {
			continue
		}
		out = append(out, cloned)
	}
	return out
}

func readExistingModels(home string) []map[string]any {
	seen := map[string]struct{}{}
	out := []map[string]any{}
	for _, name := range []string{catalogFileName, cacheFileName} {
		models, err := ReadModelsFile(filepath.Join(home, name))
		if err != nil {
			continue
		}
		for _, model := range models {
			slug := slugOf(model)
			if slug == "" {
				continue
			}
			if _, dup := seen[slug]; dup {
				continue
			}
			seen[slug] = struct{}{}
			out = append(out, model)
		}
	}
	return out
}

func ReadModelsFile(path string) ([]map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseModels(raw)
}

func parseModels(raw []byte) ([]map[string]any, error) {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	switch parsed := root.(type) {
	case []any:
		return asModelMaps(parsed), nil
	case map[string]any:
		if models, ok := parsed["models"].([]any); ok {
			return asModelMaps(models), nil
		}
	}
	return nil, nil
}

func asModelMaps(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		model, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, model)
	}
	return out
}
