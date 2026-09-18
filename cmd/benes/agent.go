package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

func runAgent(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	if len(args) == 0 {
		args = []string{"status"}
	}
	switch args[0] {
	case "status":
		return runAgentStatus(jsonOut, stdout, stderr, deps)
	case "injection", "guidance":
		return runAgentInjection(args[1:], jsonOut, stdout, stderr, deps)
	case "effort":
		return runAgentEffort(args[1:], jsonOut, stdout, stderr, deps)
	case "subagents", "roster":
		return runAgentSubagents(args[1:], jsonOut, stdout, stderr, deps)
	case "fallback":
		return runAgentFallback(args[1:], jsonOut, stdout, stderr, deps)
	case "sidecar":
		return runAgentSidecar(args[1:], jsonOut, stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: agent status|injection|effort|subagents|fallback|sidecar")
		return 2
	}
}

func runAgentStatus(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return serveFailure(stderr, "load config", err)
	}
	view := map[string]any{
		"injectionModel":              jsonRawString(root["injectionModel"]),
		"injectionEffort":             jsonRawString(root["injectionEffort"]),
		"effortCap":                   jsonRawString(root["effortCap"]),
		"subagentEffortCap":           jsonRawString(root["subagentEffortCap"]),
		"subagentModels":              jsonRawStrings(root["subagentModels"]),
		"subagentModelFallback":       jsonRawStrings(root["subagentModelFallback"]),
		"subagentModelFallbackPollMs": jsonRawInt(root["subagentModelFallbackPollMs"]),
		"webSearchSidecar":            jsonRawObject(root["webSearchSidecar"]),
		"visionSidecar":               jsonRawObject(root["visionSidecar"]),
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, view)
	}
	fmt.Fprintf(stdout, "injectionModel: %s\n", emptyDash(view["injectionModel"].(string)))
	fmt.Fprintf(stdout, "effortCap: %s\n", emptyDash(view["effortCap"].(string)))
	models := view["subagentModels"].([]string)
	fmt.Fprintf(stdout, "subagents: %s\n", strings.Join(models, ", "))
	return 0
}

func runAgentInjection(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 || args[0] == "status" {
		return runAgentStatus(jsonOut, stdout, stderr, deps)
	}
	if args[0] != "set" {
		fmt.Fprintln(stderr, "benes: usage: agent injection set [--model <id|->] [--effort <level|->] [--prompt <text|->] [--guidance on|off]")
		return 2
	}
	rest := append([]string{}, args[1:]...)
	model, rest := takeOption(rest, "--model")
	effort, rest := takeOption(rest, "--effort")
	prompt, rest := takeOption(rest, "--prompt")
	guidance, rest := takeOption(rest, "--guidance")
	if len(rest) > 0 {
		fmt.Fprintf(stderr, "benes: unexpected argument(s): %s\n", strings.Join(rest, " "))
		return 2
	}
	if model == "" && effort == "" && prompt == "" && guidance == "" {
		fmt.Fprintln(stderr, "benes: at least one injection option is required")
		return 2
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		if model != "" {
			if err := setOrDelete(tx, "injectionModel", model); err != nil {
				return err
			}
		}
		if effort != "" {
			if err := setOrDelete(tx, "injectionEffort", effort); err != nil {
				return err
			}
		}
		if prompt != "" {
			if err := setOrDelete(tx, "injectionPrompt", prompt); err != nil {
				return err
			}
		}
		if guidance != "" {
			on, ok := parseOnOff(guidance)
			if !ok {
				return fmt.Errorf("--guidance must be on or off")
			}
			encoded, err := json.Marshal(on)
			if err != nil {
				return err
			}
			return tx.Set(config.JSONPath("multiAgentGuidanceEnabled"), encoded)
		}
		return nil
	}); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"ok": true})
	}
	fmt.Fprintln(stdout, "Agent injection settings updated.")
	return 0
}

func runAgentEffort(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 || args[0] == "status" {
		return runAgentStatus(jsonOut, stdout, stderr, deps)
	}
	if args[0] != "set" {
		fmt.Fprintln(stderr, "benes: usage: agent effort set [--main <level|->] [--subagent <level|->]")
		return 2
	}
	rest := append([]string{}, args[1:]...)
	mainCap, rest := takeOption(rest, "--main")
	subCap, rest := takeOption(rest, "--subagent")
	if len(rest) > 0 || (mainCap == "" && subCap == "") {
		fmt.Fprintln(stderr, "benes: --main and/or --subagent is required")
		return 2
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		if mainCap != "" {
			if err := setOrDelete(tx, "effortCap", mainCap); err != nil {
				return err
			}
		}
		if subCap != "" {
			if err := setOrDelete(tx, "subagentEffortCap", subCap); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"ok": true})
	}
	fmt.Fprintln(stdout, "Agent effort caps updated.")
	return 0
}

func runAgentSubagents(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 || args[0] == "status" {
		return runAgentStatus(jsonOut, stdout, stderr, deps)
	}
	var models []string
	switch args[0] {
	case "clear":
		models = nil
	case "set":
		if len(args) < 2 {
			fmt.Fprintln(stderr, "benes: comma-separated subagent models are required")
			return 2
		}
		models = splitCSV(args[1])
		if len(models) > 5 {
			fmt.Fprintln(stderr, "benes: at most 5 subagent models are allowed")
			return 2
		}
	default:
		fmt.Fprintln(stderr, "benes: usage: agent subagents status|set|clear")
		return 2
	}
	if err := setStringSlice(stderr, deps, "subagentModels", models); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"models": models})
	}
	if len(models) == 0 {
		fmt.Fprintln(stdout, "Subagent roster: cleared")
		return 0
	}
	fmt.Fprintf(stdout, "Subagent roster: %s\n", strings.Join(models, ", "))
	return 0
}

func runAgentFallback(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 || args[0] == "status" {
		return runAgentStatus(jsonOut, stdout, stderr, deps)
	}
	rest := append([]string{}, args[1:]...)
	pollRaw, rest := takeOption(rest, "--poll-ms")
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		switch args[0] {
		case "clear":
			if err := tx.Delete(config.JSONPath("subagentModelFallback")); err != nil {
				return err
			}
		case "set":
			raw := ""
			if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
				raw = rest[0]
				rest = rest[1:]
			}
			if raw != "" {
				encoded, err := json.Marshal(splitCSV(raw))
				if err != nil {
					return err
				}
				if err := tx.Set(config.JSONPath("subagentModelFallback"), encoded); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unknown fallback action")
		}
		if pollRaw != "" {
			n, err := strconv.Atoi(pollRaw)
			if err != nil || n < 5000 || n > 600000 {
				return fmt.Errorf("--poll-ms must be 5000..600000")
			}
			encoded, err := json.Marshal(n)
			if err != nil {
				return err
			}
			return tx.Set(config.JSONPath("subagentModelFallbackPollMs"), encoded)
		}
		if len(rest) > 0 {
			return fmt.Errorf("unexpected arguments")
		}
		return nil
	}); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"ok": true})
	}
	fmt.Fprintln(stdout, "Subagent fallback settings updated.")
	return 0
}

func runAgentSidecar(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 || args[0] == "status" {
		return runAgentStatus(jsonOut, stdout, stderr, deps)
	}
	section := args[0]
	if section != "web" && section != "vision" {
		fmt.Fprintln(stderr, "benes: sidecar must be web, vision, or status")
		return 2
	}
	rest := append([]string{}, args[1:]...)
	model, rest := takeOption(rest, "--model")
	backend, rest := takeOption(rest, "--backend")
	reasoning, rest := takeOption(rest, "--reasoning")
	maxRaw, rest := takeOption(rest, "--max-descriptions")
	if len(rest) > 0 || (model == "" && backend == "" && reasoning == "" && maxRaw == "") {
		fmt.Fprintln(stderr, "benes: at least one sidecar option is required")
		return 2
	}
	key := "webSearchSidecar"
	if section == "vision" {
		key = "visionSidecar"
	}
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return serveFailure(stderr, "load config", err)
	}
	settings := jsonRawObject(root[key])
	if model != "" {
		if model == "-" {
			delete(settings, "model")
		} else {
			settings["model"] = model
		}
	}
	if backend != "" {
		if backend == "-" {
			delete(settings, "backend")
		} else if backend != "openai" && backend != "anthropic" {
			fmt.Fprintln(stderr, "benes: --backend must be openai or anthropic")
			return 2
		} else {
			settings["backend"] = backend
		}
	}
	if reasoning != "" {
		settings["reasoning"] = reasoning
	}
	if maxRaw != "" {
		n, convErr := strconv.Atoi(maxRaw)
		if convErr != nil || n < 1 {
			fmt.Fprintln(stderr, "benes: --max-descriptions must be >= 1")
			return 2
		}
		settings["maxDescriptionsPerTurn"] = n
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return serveFailure(stderr, "encode sidecar", err)
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath(key), encoded)
	}); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"ok": true, "section": section})
	}
	fmt.Fprintf(stdout, "%s sidecar settings updated.\n", section)
	return 0
}

func setOrDelete(tx *config.Transaction, key, value string) error {
	if value == "-" {
		return tx.Delete(config.JSONPath(key))
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return tx.Set(config.JSONPath(key), encoded)
}

func setStringSlice(stderr io.Writer, deps commandDependencies, key string, values []string) error {
	if len(values) == 0 {
		return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
			return tx.Delete(config.JSONPath(key))
		})
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath(key), encoded)
	})
}

func splitCSV(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func jsonRawString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func jsonRawInt(raw json.RawMessage) int {
	var value int
	_ = json.Unmarshal(raw, &value)
	return value
}

func jsonRawStrings(raw json.RawMessage) []string {
	var value []string
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return []string{}
	}
	return value
}

func jsonRawObject(raw json.RawMessage) map[string]any {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return map[string]any{}
	}
	return value
}

func emptyDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
