package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/modeldiscovery"
)

func runModelsNewPolicy(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	provider, args := takeOption(args, "--provider")
	if len(args) > 1 {
		fmt.Fprintln(stderr, "benes: usage: models new-policy [show|on|off] [--provider <id>]")
		return 2
	}
	action := "show"
	if len(args) == 1 {
		action = args[0]
	}
	if action != "show" && action != "on" && action != "off" {
		fmt.Fprintln(stderr, "benes: usage: models new-policy [show|on|off] [--provider <id>]")
		return 2
	}
	if action == "show" {
		return showModelsNewPolicy(provider, jsonOut, stdout, stderr, deps)
	}
	if err := setModelsNewPolicy(stderr, deps, provider, action); err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"ok": true, "newModelPolicy": action, "provider": provider})
	}
	if provider == "" {
		fmt.Fprintf(stdout, "new models start disabled: %s\n", action)
	} else {
		fmt.Fprintf(stdout, "%s: new models start disabled: %s\n", provider, action)
	}
	return 0
}

func runModelsNewArrivals(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "benes: usage: models new-arrivals [--json]")
		return 2
	}
	rows, err := reconcileNewArrivals(stderr, deps)
	if err != nil {
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"arrivals": rows})
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "no new-model arrivals")
		return 0
	}
	fmt.Fprintf(stdout, "%-14s %-36s %s\n", "PROVIDER", "MODEL", "STATE")
	for _, row := range rows {
		fmt.Fprintf(stdout, "%-14s %-36s %s\n", row.Provider, row.Model, row.State)
	}
	return 0
}

type arrivalRow struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	State    string `json:"state"`
}

func showModelsNewPolicy(provider string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	snap, _, err := loadDiscoveryWorld(deps)
	if err != nil {
		return serveFailure(stderr, "load model discovery", err)
	}
	global := string(modeldiscovery.Resolve(snap.NewModelPolicy, ""))
	if jsonOut {
		out := map[string]any{"newModelPolicy": global, "providers": map[string]any{}}
		if provider != "" {
			out["provider"] = provider
			out["resolved"] = string(modeldiscovery.Resolve(snap.NewModelPolicy, providerPolicyOverride(deps, provider)))
		}
		return encodeJSON(stdout, stderr, out)
	}
	fmt.Fprintf(stdout, "new models start disabled: %s\n", global)
	if provider != "" {
		fmt.Fprintf(stdout, "%s: %s\n", provider, modeldiscovery.Resolve(snap.NewModelPolicy, providerPolicyOverride(deps, provider)))
	}
	return 0
}

func setModelsNewPolicy(stderr io.Writer, deps commandDependencies, provider, policy string) error {
	return persistDiscovery(stderr, deps, func(snap modeldiscovery.Snapshot, catalogs []modeldiscovery.ProviderCatalog, disabled []string) (modeldiscovery.Snapshot, []modeldiscovery.ProviderCatalog, []string) {
		if provider == "" {
			snap.NewModelPolicy = policy
		} else {
			for i := range catalogs {
				if catalogs[i].ID == provider {
					catalogs[i].PolicyOverride = policy
				}
			}
		}
		return snap, catalogs, disabled
	}, provider)
}

func reconcileNewArrivals(stderr io.Writer, deps commandDependencies) ([]arrivalRow, error) {
	var rows []arrivalRow
	err := persistDiscovery(stderr, deps, func(snap modeldiscovery.Snapshot, catalogs []modeldiscovery.ProviderCatalog, disabled []string) (modeldiscovery.Snapshot, []modeldiscovery.ProviderCatalog, []string) {
		return snap, catalogs, disabled
	}, "")
	if err != nil {
		return nil, err
	}
	snap, disabled, err := loadDiscoveryWorld(deps)
	if err != nil {
		return nil, err
	}
	blocked := map[string]struct{}{}
	for _, id := range disabled {
		blocked[id] = struct{}{}
	}
	names := make([]string, 0, len(snap.ShownArrivals))
	for name := range snap.ShownArrivals {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, id := range snap.Shown(name) {
			state := "enabled"
			if _, ok := blocked[id]; ok {
				state = "auto-disabled"
			}
			rows = append(rows, arrivalRow{Provider: name, Model: id, State: state})
		}
	}
	return rows, nil
}

func persistDiscovery(stderr io.Writer, deps commandDependencies, mutate func(modeldiscovery.Snapshot, []modeldiscovery.ProviderCatalog, []string) (modeldiscovery.Snapshot, []modeldiscovery.ProviderCatalog, []string), writeProviderPolicy string) error {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return fmt.Errorf("resolve paths: %w", err)
	}
	disk, err := config.LoadDiskConfig(paths.Config, 0)
	if err != nil {
		return err
	}
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return err
	}
	snap := modeldiscovery.DecodeRoot(root)
	disabled, err := loadDisabledModels(deps)
	if err != nil {
		return err
	}
	catalogs := modeldiscovery.CatalogsFromConfig(disk.Providers, disk.Raw, snap)
	snap, catalogs, disabled = mutate(snap, catalogs, disabled)
	next, disabledOut, _ := modeldiscovery.Reconcile(snap, catalogs, disabled, time.Time{})
	return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		if writeProviderPolicy != "" {
			override := ""
			for _, catalog := range catalogs {
				if catalog.ID == writeProviderPolicy {
					override = catalog.PolicyOverride
					break
				}
			}
			path := config.JSONPath("providers", writeProviderPolicy, "newModelPolicy")
			if override == "" || override == "inherit" {
				if err := tx.Delete(path); err != nil {
					return err
				}
			} else {
				payload, err := json.Marshal(override)
				if err != nil {
					return err
				}
				if err := tx.Set(path, payload); err != nil {
					return err
				}
			}
		}
		encoded, err := json.Marshal(next)
		if err != nil {
			return err
		}
		if err := tx.Set(config.JSONPath("modelDiscovery"), encoded); err != nil {
			return err
		}
		if len(disabledOut) == 0 {
			return tx.Delete(config.JSONPath("disabledModels"))
		}
		payload, err := json.Marshal(disabledOut)
		if err != nil {
			return err
		}
		return tx.Set(config.JSONPath("disabledModels"), payload)
	})
}

func loadDiscoveryWorld(deps commandDependencies) (modeldiscovery.Snapshot, []string, error) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return modeldiscovery.Snapshot{}, nil, err
	}
	disabled, err := loadDisabledModels(deps)
	if err != nil {
		return modeldiscovery.Snapshot{}, nil, err
	}
	return modeldiscovery.DecodeRoot(root), disabled, nil
}

func providerPolicyOverride(deps commandDependencies, provider string) string {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return ""
	}
	disk, err := config.LoadDiskConfig(paths.Config, 0)
	if err != nil {
		return ""
	}
	snap := modeldiscovery.DecodeConfig(disk.Raw)
	for _, catalog := range modeldiscovery.CatalogsFromConfig(disk.Providers, disk.Raw, snap) {
		if catalog.ID == provider {
			return catalog.PolicyOverride
		}
	}
	return ""
}
