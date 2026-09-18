package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Wibias/Benes/internal/bootstrap"
	"github.com/Wibias/Benes/internal/config"
)

func runModelsEnable(args []string, enabled bool, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	args, native := takeConfigFlag(args, "--native")
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: models enable|disable <provider/model|native-id> [--native]")
		return 2
	}
	provider, id, err := parseModelSelector(args[0], native)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v\n", err)
		return 2
	}
	disabled, loadErr := loadDisabledModels(deps)
	if loadErr != nil {
		return serveFailure(stderr, "load disabled models", loadErr)
	}
	canonical := provider + "/" + id
	if native {
		canonical = id
	}
	if enabled {
		disabled = filterStrings(disabled, func(stored string) bool {
			return !modelSelectorMatch(stored, provider, id, native)
		})
	} else if !containsString(disabled, canonical) && !disabledMatches(disabled, provider, id, native) {
		disabled = append(disabled, canonical)
	}
	if err := saveDisabledModels(stderr, deps, disabled); err != nil {
		return 1
	}
	verb := "Disabled"
	if enabled {
		verb = "Enabled"
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"enabled": enabled, "selector": args[0], "disabled": disabled})
	}
	fmt.Fprintf(stdout, "%s %s.\n", verb, args[0])
	return 0
}

func runModelsProviderVisibility(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 2 || (args[1] != "on" && args[1] != "off") {
		fmt.Fprintln(stderr, "benes: usage: models provider <name> <on|off>")
		return 2
	}
	provider := args[0]
	enable := args[1] == "on"
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	disk, err := config.LoadDiskConfig(paths.Config, 0)
	if err != nil {
		return serveFailure(stderr, "load config", err)
	}
	if _, ok := disk.Providers[provider]; !ok {
		fmt.Fprintf(stderr, "benes: provider %q is not configured\n", provider)
		return 1
	}
	disabled, err := loadDisabledModels(deps)
	if err != nil {
		return serveFailure(stderr, "load disabled models", err)
	}
	prefix := provider + "/"
	if enable {
		disabled = filterStrings(disabled, func(stored string) bool {
			return !strings.HasPrefix(stored, prefix)
		})
	} else {
		for _, model := range bootstrap.ListCatalogModels(disk) {
			if !strings.HasPrefix(model.ID, prefix) {
				continue
			}
			if !containsString(disabled, model.ID) {
				disabled = append(disabled, model.ID)
			}
		}
	}
	if err := saveDisabledModels(stderr, deps, disabled); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"provider": provider, "enabled": enable})
	}
	fmt.Fprintf(stdout, "%s: %s\n", provider, args[1])
	return 0
}

func runModelsSelected(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	args, clear := takeConfigFlag(args, "--clear")
	setRaw, args := takeOption(args, "--set")
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: models selected <provider> [--set id,id|--clear]")
		return 2
	}
	if setRaw != "" && clear {
		fmt.Fprintln(stderr, "benes: --set and --clear cannot be combined")
		return 2
	}
	provider := args[0]
	providers, _, err := loadProviders(deps)
	if err != nil {
		return serveFailure(stderr, "load providers", err)
	}
	if _, ok := providers[provider]; !ok {
		fmt.Fprintf(stderr, "benes: provider %q is not configured\n", provider)
		return 1
	}
	if setRaw == "" && !clear {
		selected, err := loadProviderSelectedModels(deps, provider)
		if err != nil {
			return serveFailure(stderr, "load selected models", err)
		}
		if jsonOut {
			return encodeJSON(stdout, stderr, map[string]any{"provider": provider, "selected": selected})
		}
		if len(selected) == 0 {
			fmt.Fprintf(stdout, "%s: all models\n", provider)
			return 0
		}
		fmt.Fprintf(stdout, "%s: %s\n", provider, strings.Join(selected, ", "))
		return 0
	}
	var models []string
	if !clear {
		for _, part := range strings.Split(setRaw, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				models = append(models, part)
			}
		}
	}
	if err := saveProviderSelectedModels(stderr, deps, provider, models); err != nil {
		return 1
	}
	if err := markProviderSelectionCustom(stderr, deps, provider); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"provider": provider, "selected": models})
	}
	if len(models) == 0 {
		fmt.Fprintf(stdout, "%s: all models\n", provider)
		return 0
	}
	fmt.Fprintf(stdout, "%s: %s\n", provider, strings.Join(models, ", "))
	return 0
}

func parseModelSelector(selector string, native bool) (provider, id string, err error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", "", fmt.Errorf("model selector is required")
	}
	if native || !strings.Contains(selector, "/") {
		return "openai", selector, nil
	}
	slash := strings.Index(selector, "/")
	provider = selector[:slash]
	id = selector[slash+1:]
	if provider == "" || id == "" {
		return "", "", fmt.Errorf("model selector must be provider/model or a native model id")
	}
	return provider, id, nil
}

func modelSelectorMatch(stored, provider, id string, native bool) bool {
	if native {
		return stored == id
	}
	return stored == provider+"/"+id || stored == id
}

func disabledMatches(disabled []string, provider, id string, native bool) bool {
	for _, stored := range disabled {
		if modelSelectorMatch(stored, provider, id, native) {
			return true
		}
	}
	return false
}

func loadDisabledModels(deps commandDependencies) ([]string, error) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return nil, err
	}
	raw, ok := root["disabledModels"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var models []string
	if err := json.Unmarshal(raw, &models); err != nil {
		return nil, err
	}
	return models, nil
}

func saveDisabledModels(stderr io.Writer, deps commandDependencies, models []string) error {
	if len(models) == 0 {
		return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
			return tx.Delete(config.JSONPath("disabledModels"))
		})
	}
	encoded, err := json.Marshal(models)
	if err != nil {
		return err
	}
	return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("disabledModels"), encoded)
	})
}

func loadProviderSelectedModels(deps commandDependencies, provider string) ([]string, error) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return nil, err
	}
	rawProviders, ok := root["providers"]
	if !ok {
		return nil, nil
	}
	var blob map[string]json.RawMessage
	if err := json.Unmarshal(rawProviders, &blob); err != nil {
		return nil, err
	}
	body, ok := blob[provider]
	if !ok {
		return nil, nil
	}
	var rec map[string]json.RawMessage
	if err := json.Unmarshal(body, &rec); err != nil {
		return nil, err
	}
	raw, ok := rec["selectedModels"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var models []string
	if err := json.Unmarshal(raw, &models); err != nil {
		return nil, err
	}
	return models, nil
}

func saveProviderSelectedModels(stderr io.Writer, deps commandDependencies, provider string, models []string) error {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return err
	}
	rawProviders, ok := root["providers"]
	if !ok {
		return fmt.Errorf("providers missing")
	}
	var blob map[string]json.RawMessage
	if err := json.Unmarshal(rawProviders, &blob); err != nil {
		return err
	}
	body, ok := blob[provider]
	if !ok {
		return fmt.Errorf("provider %q is not configured", provider)
	}
	var rec map[string]any
	if err := json.Unmarshal(body, &rec); err != nil {
		return err
	}
	if len(models) == 0 {
		delete(rec, "selectedModels")
	} else {
		rec["selectedModels"] = models
	}
	encoded, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("providers", provider), encoded)
	})
}

func filterStrings(in []string, keep func(string) bool) []string {
	var out []string
	for _, value := range in {
		if keep(value) {
			out = append(out, value)
		}
	}
	return out
}

func containsString(in []string, want string) bool {
	for _, value := range in {
		if value == want {
			return true
		}
	}
	return false
}
