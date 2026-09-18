package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

type comboTarget struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Weight   int    `json:"weight,omitempty"`
}

type comboRecord struct {
	Targets       []comboTarget `json:"targets"`
	Strategy      string        `json:"strategy,omitempty"`
	StickyLimit   int           `json:"stickyLimit,omitempty"`
	DefaultEffort *string       `json:"defaultEffort,omitempty"`
	ImageInput    string        `json:"imageInput,omitempty"`
	Alias         string        `json:"alias,omitempty"`
	NativeAlias   bool          `json:"nativeAlias,omitempty"`
	DisplayName   string        `json:"displayName,omitempty"`
}

func runCombo(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	args, yes := takeConfigFlag(args, "--yes")
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		return runComboList(jsonOut, stdout, stderr, deps)
	case "show":
		return runComboShow(args[1:], jsonOut, stdout, stderr, deps)
	case "set", "create", "update":
		return runComboSet(args[1:], jsonOut, stdout, stderr, deps)
	case "remove", "delete":
		return runComboRemove(args[1:], yes, jsonOut, stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: combo list|show|set|remove")
		return 2
	}
}

func runRoute(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 || (args[0] != "combo" && args[0] != "policy") {
		fmt.Fprintln(stderr, "benes: usage: route combo <list|show|set|remove> | route policy list|show")
		return 2
	}
	if args[0] == "policy" {
		return runRoutePolicy(args[1:], stdout, stderr, deps)
	}
	return runCombo(args[1:], stdout, stderr, deps)
}

func runComboList(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	combos, err := loadCombos(deps)
	if err != nil {
		return serveFailure(stderr, "load combos", err)
	}
	if jsonOut {
		rows := make([]map[string]any, 0, len(combos))
		for id, combo := range combos {
			rows = append(rows, map[string]any{"id": id, "model": "combo/" + id, "strategy": combo.Strategy, "targets": combo.Targets})
		}
		return encodeJSON(stdout, stderr, map[string]any{"combos": rows})
	}
	if len(combos) == 0 {
		fmt.Fprintln(stdout, "No combos configured.")
		return 0
	}
	for _, id := range sortedKeys(combos) {
		fmt.Fprintf(stdout, "%s  combo/%s\n", id, id)
	}
	return 0
}

func runComboShow(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: combo show <id>")
		return 2
	}
	combos, err := loadCombos(deps)
	if err != nil {
		return serveFailure(stderr, "load combos", err)
	}
	combo, ok := combos[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "benes: unknown combo %s\n", args[0])
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, combo)
	}
	fmt.Fprintf(stdout, "id: %s\nstrategy: %s\nstickyLimit: %d\n", args[0], combo.Strategy, combo.StickyLimit)
	for _, target := range combo.Targets {
		fmt.Fprintf(stdout, "target: %s/%s", target.Provider, target.Model)
		if target.Weight > 0 {
			fmt.Fprintf(stdout, " weight=%d", target.Weight)
		}
		fmt.Fprintln(stdout)
	}
	return 0
}

func runComboSet(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, "benes: usage: combo set <id> --targets <provider/model,...> [--strategy failover]")
		return 2
	}
	id := strings.TrimSpace(args[0])
	rest := append([]string{}, args[1:]...)
	targetsRaw, rest := takeOption(rest, "--targets")
	strategy, rest := takeOption(rest, "--strategy")
	stickyRaw, rest := takeOption(rest, "--sticky")
	effort, rest := takeOption(rest, "--effort")
	alias, rest := takeOption(rest, "--alias")
	displayName, rest := takeOption(rest, "--display-name")
	renameFrom, rest := takeOption(rest, "--rename-from")
	rest, nativeAlias := takeConfigFlag(rest, "--native-alias")
	if len(rest) > 0 {
		fmt.Fprintf(stderr, "benes: unexpected argument(s): %s\n", strings.Join(rest, " "))
		return 2
	}
	if targetsRaw == "" {
		fmt.Fprintln(stderr, "benes: --targets is required")
		return 2
	}
	if strategy == "" {
		strategy = "failover"
	}
	if strategy != "failover" {
		fmt.Fprintln(stderr, "benes: --strategy must be failover; combos are failover only")
		return 2
	}
	targets, err := parseComboTargets(targetsRaw)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v\n", err)
		return 2
	}
	existing, loadErr := loadCombos(deps)
	if loadErr != nil {
		return serveFailure(stderr, "load combos", loadErr)
	}
	srcID := id
	if renameFrom != "" {
		srcID = renameFrom
	}
	combo := comboRecord{Targets: targets, Strategy: strategy, NativeAlias: nativeAlias}
	if stickyRaw != "" {
		n, err := strconv.Atoi(stickyRaw)
		if err != nil || n < 1 || n > 100 {
			fmt.Fprintln(stderr, "benes: --sticky must be 1..100")
			return 2
		}
		combo.StickyLimit = n
	} else if prev, ok := existing[srcID]; ok && prev.StickyLimit > 0 {
		// Preserve explicit legacy stickyLimit; do not invent a default.
		combo.StickyLimit = prev.StickyLimit
	}
	if effort != "" && effort != "-" {
		combo.DefaultEffort = &effort
	}
	if alias != "" && alias != "-" {
		combo.Alias = alias
	}
	if displayName != "" && displayName != "-" {
		combo.DisplayName = displayName
	}
	if prev, ok := existing[srcID]; ok && prev.ImageInput == "disabled" {
		combo.ImageInput = "disabled"
	}
	encoded, err := json.Marshal(combo)
	if err != nil {
		return serveFailure(stderr, "encode combo", err)
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		if renameFrom != "" && renameFrom != id {
			if err := tx.Delete(config.JSONPath("combos", renameFrom)); err != nil {
				return err
			}
		}
		return tx.Set(config.JSONPath("combos", id), encoded)
	}); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"id": id, "combo": combo})
	}
	fmt.Fprintf(stdout, "Saved combo %s.\n", id)
	return 0
}

func runComboRemove(args []string, yes, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: combo remove <id> --yes")
		return 2
	}
	if !yes {
		fmt.Fprintln(stderr, "benes: remove requires --yes")
		return 1
	}
	id := args[0]
	combos, err := loadCombos(deps)
	if err != nil {
		return serveFailure(stderr, "load combos", err)
	}
	if _, ok := combos[id]; !ok {
		fmt.Fprintf(stderr, "benes: unknown combo %s\n", id)
		return 1
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Delete(config.JSONPath("combos", id))
	}); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"removed": id})
	}
	fmt.Fprintf(stdout, "Removed combo %s.\n", id)
	return 0
}

func loadCombos(deps commandDependencies) (map[string]comboRecord, error) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return nil, err
	}
	raw, ok := root["combos"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return map[string]comboRecord{}, nil
	}
	var combos map[string]comboRecord
	if err := json.Unmarshal(raw, &combos); err != nil {
		return nil, err
	}
	if combos == nil {
		return map[string]comboRecord{}, nil
	}
	return combos, nil
}

func parseComboTargets(raw string) ([]comboTarget, error) {
	parts := strings.Split(raw, ",")
	var targets []comboTarget
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		weight := 0
		if cut := strings.LastIndex(part, ":"); cut > 0 && !strings.Contains(part[cut+1:], "/") {
			if n, err := strconv.Atoi(part[cut+1:]); err == nil {
				weight = n
				part = part[:cut]
			}
		}
		slash := strings.Index(part, "/")
		if slash <= 0 || slash == len(part)-1 {
			return nil, fmt.Errorf("target %q must be provider/model", part)
		}
		targets = append(targets, comboTarget{Provider: part[:slash], Model: part[slash+1:], Weight: weight})
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("--targets requires at least one provider/model")
	}
	return targets, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
