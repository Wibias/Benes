package codexcache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/codexcatalog"
	"github.com/Wibias/Benes/internal/store/atomicfile"
)

const staleFetchedAt = "2000-01-01T00:00:00Z"

func Invalidate(home string) (bool, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		return false, nil
	}
	catalogPath := filepath.Join(home, "benes-catalog.json")
	raw, err := os.ReadFile(catalogPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	models, err := catalogModels(raw)
	if err != nil {
		return false, nil
	}
	if len(models) == 0 {
		return false, nil
	}
	slugs := map[string]struct{}{}
	for _, model := range models {
		if slug, _ := model["slug"].(string); slug != "" {
			slugs[slug] = struct{}{}
		}
		codexcatalog.SanitizeRouted(model)
	}
	cachePath := filepath.Join(home, "models_cache.json")
	for _, observed := range observedNativeEntries(cachePath, slugs) {
		models = append(models, observed)
	}
	wrapper, err := json.MarshalIndent(map[string]any{
		"fetched_at":     staleFetchedAt,
		"client_version": "0.0.0",
		"models":         models,
	}, "", "  ")
	if err != nil {
		return false, err
	}
	if err := atomicfile.Write(cachePath, append(wrapper, '\n'), atomicfile.Options{Mode: 0o600}); err != nil {
		return false, err
	}
	return true, nil
}

func catalogModels(raw []byte) ([]map[string]any, error) {
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

func observedNativeEntries(cachePath string, existing map[string]struct{}) []map[string]any {
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		return nil
	}
	models, err := catalogModels(raw)
	if err != nil {
		return nil
	}
	out := []map[string]any{}
	for _, model := range models {
		if model["benes_account_observed_native"] != true {
			continue
		}
		slug, _ := model["slug"].(string)
		if slug != "" {
			if _, exists := existing[slug]; exists {
				continue
			}
		}
		model["visibility"] = "hide"
		out = append(out, model)
	}
	return out
}
