package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

const defaultProviderContextCap = 350_000

func runModelsEdit(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, "benes: usage: models edit <custom-id> [--model-id <id>] [--display-name <name|->]")
		return 2
	}
	id := args[0]
	rest := append([]string{}, args[1:]...)
	modelID, rest := takeOption(rest, "--model-id")
	displayName, rest := takeOption(rest, "--display-name")
	contextRaw, rest := takeOption(rest, "--context-window")
	modalitiesRaw, rest := takeOption(rest, "--modalities")
	if len(rest) > 0 {
		fmt.Fprintf(stderr, "benes: unexpected argument(s): %s\n", strings.Join(rest, " "))
		return 2
	}
	if modelID == "" && displayName == "" && contextRaw == "" && modalitiesRaw == "" {
		fmt.Fprintln(stderr, "benes: at least one edit option is required")
		return 2
	}
	models, err := loadCustomModels(deps)
	if err != nil {
		return serveFailure(stderr, "load custom models", err)
	}
	idx := -1
	for i, model := range models {
		if model.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		fmt.Fprintf(stderr, "benes: custom model %q not found\n", id)
		return 1
	}
	if modelID != "" {
		models[idx].ModelID = modelID
	}
	if displayName != "" {
		if displayName == "-" {
			models[idx].DisplayName = ""
		} else {
			models[idx].DisplayName = displayName
		}
	}
	if contextRaw != "" {
		n, convErr := strconv.Atoi(strings.ReplaceAll(contextRaw, "_", ""))
		if convErr != nil || n < 0 {
			fmt.Fprintln(stderr, "benes: --context-window must be an integer >= 0")
			return 2
		}
		models[idx].ContextWindow = n
	}
	if modalitiesRaw != "" {
		if modalitiesRaw == "-" {
			models[idx].InputModalities = nil
		} else {
			models[idx].InputModalities = splitCSV(modalitiesRaw)
		}
	}
	if err := saveCustomModels(stderr, deps, models); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, models[idx])
	}
	fmt.Fprintf(stdout, "Updated custom model %s.\n", id)
	return 0
}

func runModelsContext(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 {
		args = []string{"status"}
	}
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return serveFailure(stderr, "load config", err)
	}
	switch args[0] {
	case "status":
		caps := loadContextCaps(root)
		value := jsonRawInt(root["contextCapValue"])
		if value <= 0 {
			value = defaultProviderContextCap
		}
		if jsonOut {
			return encodeJSON(stdout, stderr, map[string]any{"value": value, "caps": caps})
		}
		fmt.Fprintf(stdout, "value: %d\n", value)
		for _, name := range sortedKeys(caps) {
			fmt.Fprintf(stdout, "%s: %d\n", name, caps[name])
		}
		return 0
	case "value":
		if len(args) < 2 {
			fmt.Fprintln(stderr, "benes: context value is required")
			return 2
		}
		n, convErr := strconv.Atoi(strings.ReplaceAll(args[1], "_", ""))
		if convErr != nil || n <= 0 {
			fmt.Fprintln(stderr, "benes: context value must be a positive integer")
			return 2
		}
		rest := args[2:]
		rest, setAll := takeConfigFlag(rest, "--set-all")
		if len(rest) > 0 {
			fmt.Fprintln(stderr, "benes: unexpected arguments")
			return 2
		}
		encoded, _ := json.Marshal(n)
		if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
			if err := tx.Set(config.JSONPath("contextCapValue"), encoded); err != nil {
				return err
			}
			if !setAll {
				return nil
			}
			caps := loadContextCaps(root)
			for name := range caps {
				caps[name] = n
			}
			body, err := json.Marshal(caps)
			if err != nil {
				return err
			}
			if len(caps) == 0 {
				return nil
			}
			return tx.Set(config.JSONPath("providerContextCaps"), body)
		}); err != nil {
			return 1
		}
	case "provider":
		if len(args) < 3 || (args[2] != "on" && args[2] != "off") {
			fmt.Fprintln(stderr, "benes: usage: models context provider <name> on|off [--value <tokens>]")
			return 2
		}
		provider := args[1]
		enabled := args[2] == "on"
		rest := args[3:]
		valueRaw, rest := takeOption(rest, "--value")
		if len(rest) > 0 {
			fmt.Fprintln(stderr, "benes: unexpected arguments")
			return 2
		}
		if valueRaw != "" && !enabled {
			fmt.Fprintln(stderr, "benes: --value can only be used with on")
			return 2
		}
		caps := loadContextCaps(root)
		if enabled {
			n := jsonRawInt(root["contextCapValue"])
			if n <= 0 {
				n = defaultProviderContextCap
			}
			if valueRaw != "" {
				parsed, convErr := strconv.Atoi(strings.ReplaceAll(valueRaw, "_", ""))
				if convErr != nil || parsed <= 0 {
					fmt.Fprintln(stderr, "benes: --value must be a positive integer")
					return 2
				}
				n = parsed
			}
			caps[provider] = n
		} else {
			delete(caps, provider)
		}
		if err := saveContextCaps(stderr, deps, caps); err != nil {
			return 1
		}
	case "all":
		if len(args) != 2 || (args[1] != "on" && args[1] != "off") {
			fmt.Fprintln(stderr, "benes: usage: models context all on|off")
			return 2
		}
		if args[1] == "off" {
			if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
				return tx.Delete(config.JSONPath("providerContextCaps"))
			}); err != nil {
				return 1
			}
		} else {
			providers, _, loadErr := loadProviders(deps)
			if loadErr != nil {
				return serveFailure(stderr, "load providers", loadErr)
			}
			n := jsonRawInt(root["contextCapValue"])
			if n <= 0 {
				n = defaultProviderContextCap
			}
			caps := map[string]int{}
			for name := range providers {
				caps[name] = n
			}
			if err := saveContextCaps(stderr, deps, caps); err != nil {
				return 1
			}
		}
	default:
		fmt.Fprintln(stderr, "benes: usage: models context status|value|provider|all")
		return 2
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"ok": true})
	}
	fmt.Fprintln(stdout, "Context cap settings updated.")
	return 0
}

func runModelsShadow(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 {
		args = []string{"status"}
	}
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return serveFailure(stderr, "load config", err)
	}
	settings := jsonRawObject(root["shadowCallIntercept"])
	if args[0] == "status" {
		if jsonOut {
			return encodeJSON(stdout, stderr, settings)
		}
		enabled, _ := settings["enabled"].(bool)
		model, _ := settings["model"].(string)
		fmt.Fprintf(stdout, "enabled: %v\nmodel: %s\n", enabled, emptyDash(model))
		return 0
	}
	if args[0] != "set" {
		fmt.Fprintln(stderr, "benes: usage: models shadow status|set")
		return 2
	}
	rest := args[1:]
	modelRaw := ""
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		modelRaw = rest[0]
		rest = rest[1:]
	}
	enabledRaw, rest := takeOption(rest, "--enabled")
	if len(rest) > 0 || (modelRaw == "" && enabledRaw == "") {
		fmt.Fprintln(stderr, "benes: model and/or --enabled is required")
		return 2
	}
	if modelRaw != "" {
		if modelRaw == "-" {
			delete(settings, "model")
		} else {
			settings["model"] = modelRaw
		}
	}
	if enabledRaw != "" {
		on, ok := parseOnOff(enabledRaw)
		if !ok {
			fmt.Fprintln(stderr, "benes: --enabled must be on or off")
			return 2
		}
		settings["enabled"] = on
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return serveFailure(stderr, "encode shadow settings", err)
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("shadowCallIntercept"), encoded)
	}); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, settings)
	}
	fmt.Fprintln(stdout, "Shadow-call settings updated.")
	return 0
}

func loadContextCaps(root map[string]json.RawMessage) map[string]int {
	raw, ok := root["providerContextCaps"]
	if !ok {
		return map[string]int{}
	}
	var caps map[string]int
	if json.Unmarshal(raw, &caps) != nil || caps == nil {
		return map[string]int{}
	}
	return caps
}

func saveContextCaps(stderr io.Writer, deps commandDependencies, caps map[string]int) error {
	if len(caps) == 0 {
		return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
			return tx.Delete(config.JSONPath("providerContextCaps"))
		})
	}
	encoded, err := json.Marshal(caps)
	if err != nil {
		return err
	}
	return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("providerContextCaps"), encoded)
	})
}
