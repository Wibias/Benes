package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Wibias/Benes/internal/bootstrap"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/modelpreset"
)

func runModelsPreset(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "benes: usage: models preset show|apply <provider> [--all]")
		return 2
	}
	switch args[0] {
	case "show":
		return runModelsPresetShow(args[1:], jsonOut, stdout, stderr, deps)
	case "apply":
		return runModelsPresetApply(args[1:], jsonOut, stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: models preset show|apply <provider> [--all]")
		return 2
	}
}

func runModelsPresetShow(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: models preset show <provider>")
		return 2
	}
	snapshot, err := loadPresetSnapshot(deps, args[0])
	if err != nil {
		return serveFailure(stderr, "load model preset", err)
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, snapshot)
	}
	fmt.Fprintf(stdout, "%s: mode=%s", snapshot.Provider, snapshot.Mode)
	if snapshot.Available {
		fmt.Fprintf(stdout, "  available preset v%d (%d models of %d)", snapshot.Version, snapshot.Matched, snapshot.Catalog)
	}
	fmt.Fprintln(stdout)
	if snapshot.Warning != "" {
		fmt.Fprintln(stderr, snapshot.Warning)
	}
	return 0
}

func runModelsPresetApply(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	args, all := takeConfigFlag(args, "--all")
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: models preset apply <provider> [--all]")
		return 2
	}
	provider := args[0]
	snapshot, err := loadPresetSnapshot(deps, provider)
	if err != nil {
		return serveFailure(stderr, "load model preset", err)
	}
	mode := modelpreset.ModePreset
	if all {
		mode = modelpreset.ModeAll
	}
	result := modelpreset.Apply(modelpreset.Decision{
		Mode:     mode,
		Catalog:  snapshot.catalogIDs,
		Custom:   snapshot.customIDs,
		Previous: snapshot.Selected,
		Marker:   snapshot.marker,
		Spec:     snapshot.spec,
	})
	if result.Changed {
		if err := persistPresetSelection(stderr, deps, provider, result); err != nil {
			return 1
		}
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{
			"provider": provider,
			"mode":     result.Marker.Mode,
			"selected": result.Selected,
			"warning":  result.Warning,
			"changed":  result.Changed,
		})
	}
	if result.Warning != "" {
		fmt.Fprintln(stdout, result.Warning)
	}
	if !result.Changed && !all {
		return 0
	}
	if all {
		fmt.Fprintf(stdout, "%s: all models\n", provider)
		return 0
	}
	fmt.Fprintf(stdout, "%s: preset v%d — %s\n", provider, result.Marker.AppliedVersion, strings.Join(result.Selected, ", "))
	return 0
}

type presetSnapshot struct {
	Provider  string   `json:"provider"`
	Mode      string   `json:"mode"`
	Available bool     `json:"available"`
	Version   int      `json:"version,omitempty"`
	Matched   int      `json:"matched"`
	Catalog   int      `json:"catalog"`
	Selected  []string `json:"selected,omitempty"`
	Warning   string   `json:"warning,omitempty"`

	catalogIDs []string
	customIDs  []string
	marker     modelpreset.Marker
	spec       modelpreset.Spec
}

func loadPresetSnapshot(deps commandDependencies, provider string) (presetSnapshot, error) {
	providers, _, err := loadProviders(deps)
	if err != nil {
		return presetSnapshot{}, err
	}
	if _, ok := providers[provider]; !ok {
		return presetSnapshot{}, fmt.Errorf("provider %q is not configured", provider)
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return presetSnapshot{}, err
	}
	disk, err := config.LoadDiskConfig(paths.Config, 0)
	if err != nil {
		return presetSnapshot{}, err
	}
	catalogIDs := providerCatalogModelIDs(disk, provider)
	customIDs := providerCustomModelIDs(disk, provider)
	selected, err := loadProviderSelectedModels(deps, provider)
	if err != nil {
		return presetSnapshot{}, err
	}
	marker, err := loadProviderModelPreset(deps, provider)
	if err != nil {
		return presetSnapshot{}, err
	}
	spec, available := modelpreset.Lookup(provider)
	matched := 0
	if available {
		matched = len(modelpreset.Match(catalogIDs, spec))
	}
	mode := string(marker.Mode)
	if mode == "" {
		if len(selected) == 0 {
			mode = string(modelpreset.ModeAll)
		} else {
			mode = string(modelpreset.ModeCustom)
		}
	}
	return presetSnapshot{
		Provider:   provider,
		Mode:       mode,
		Available:  available,
		Version:    spec.Version,
		Matched:    matched,
		Catalog:    len(catalogIDs),
		Selected:   selected,
		Warning:    marker.Warning,
		catalogIDs: catalogIDs,
		customIDs:  customIDs,
		marker:     marker,
		spec:       spec,
	}, nil
}

func persistPresetSelection(stderr io.Writer, deps commandDependencies, provider string, result modelpreset.Result) error {
	marker, err := json.Marshal(result.Marker)
	if err != nil {
		return err
	}
	return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		selectedPath := config.JSONPath("providers", provider, "selectedModels")
		if len(result.Selected) == 0 {
			if err := tx.Delete(selectedPath); err != nil {
				return err
			}
		} else {
			payload, err := json.Marshal(result.Selected)
			if err != nil {
				return err
			}
			if err := tx.Set(selectedPath, payload); err != nil {
				return err
			}
		}
		return tx.Set(config.JSONPath("providers", provider, "modelPreset"), marker)
	})
}

func loadProviderModelPreset(deps commandDependencies, provider string) (modelpreset.Marker, error) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return modelpreset.Marker{}, err
	}
	rawProviders, ok := root["providers"]
	if !ok {
		return modelpreset.Marker{}, nil
	}
	var blob map[string]json.RawMessage
	if err := json.Unmarshal(rawProviders, &blob); err != nil {
		return modelpreset.Marker{}, err
	}
	body, ok := blob[provider]
	if !ok {
		return modelpreset.Marker{}, nil
	}
	var rec map[string]json.RawMessage
	if err := json.Unmarshal(body, &rec); err != nil {
		return modelpreset.Marker{}, err
	}
	raw, ok := rec["modelPreset"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return modelpreset.Marker{}, nil
	}
	var marker modelpreset.Marker
	if err := json.Unmarshal(raw, &marker); err != nil {
		return modelpreset.Marker{}, err
	}
	return marker, nil
}

func markProviderSelectionCustom(stderr io.Writer, deps commandDependencies, provider string) error {
	marker, err := loadProviderModelPreset(deps, provider)
	if err != nil {
		return err
	}
	if !modelpreset.MarkCustom(marker.Mode) {
		return nil
	}
	payload, err := json.Marshal(modelpreset.Marker{Mode: modelpreset.ModeCustom})
	if err != nil {
		return err
	}
	return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("providers", provider, "modelPreset"), payload)
	})
}

func retainCustomModelOnPreset(stderr io.Writer, deps commandDependencies, provider, modelID string) error {
	marker, err := loadProviderModelPreset(deps, provider)
	if err != nil {
		return err
	}
	if marker.Mode != modelpreset.ModePreset {
		return nil
	}
	selected, err := loadProviderSelectedModels(deps, provider)
	if err != nil {
		return err
	}
	if len(selected) == 0 || containsString(selected, modelID) {
		return nil
	}
	return saveProviderSelectedModels(stderr, deps, provider, append(selected, modelID))
}

func providerCatalogModelIDs(disk config.DiskConfig, provider string) []string {
	prefix := provider + "/"
	seen := map[string]struct{}{}
	out := make([]string, 0)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if strings.HasPrefix(id, prefix) {
			id = strings.TrimPrefix(id, prefix)
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, model := range bootstrap.ListCatalogModels(disk) {
		add(model.ID)
	}
	raw, ok := disk.Providers[provider]
	if !ok {
		return out
	}
	var rec struct {
		DefaultModel string   `json:"defaultModel"`
		Models       []string `json:"models"`
	}
	if json.Unmarshal(raw, &rec) != nil {
		return out
	}
	add(rec.DefaultModel)
	for _, id := range rec.Models {
		add(id)
	}
	return out
}

func providerCustomModelIDs(disk config.DiskConfig, provider string) []string {
	var root struct {
		CustomModels []struct {
			Provider string `json:"provider"`
			ModelID  string `json:"modelId"`
		} `json:"customModels"`
	}
	if json.Unmarshal(disk.Raw, &root) != nil {
		return nil
	}
	out := make([]string, 0)
	for _, row := range root.CustomModels {
		if row.Provider != provider {
			continue
		}
		id := strings.TrimSpace(row.ModelID)
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	return out
}
