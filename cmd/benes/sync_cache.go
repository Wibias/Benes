package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Wibias/Benes/internal/config"
)

func runSyncCache(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	restartCodex := false
	for _, arg := range args {
		if arg == "--restart-codex" {
			restartCodex = true
			continue
		}
		fmt.Fprintln(stderr, "benes: usage: sync-cache [--restart-codex]")
		return 2
	}
	if !codexIntegrationWanted(deps) {
		fmt.Fprintln(stdout, "Codex integration is OFF; cache sync skipped and no Codex files changed.")
		return 0
	}
	home, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve CODEX_HOME: %v\n", err)
		return 1
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve paths: %v\n", err)
		return 1
	}
	models, aliases := catalogModelsAndAliasesFromPaths(deps, paths)
	written, err := refreshCodexPickerCatalogWithAliases(home, models, aliases)
	if err != nil {
		fmt.Fprintf(stderr, "benes: sync-cache: %v\n", err)
		return 1
	}
	if !written {
		return 0
	}
	fmt.Fprintln(stdout, "Refreshed Codex models_cache.json from the active catalog.")
	afterSyncWrite(stdout, stderr, restartCodex, appserverIO(deps, home))
	return 0
}

func codexIntegrationWanted(deps commandDependencies) bool {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return true
	}
	raw, ok := root["clientIntegrations"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return true
	}
	var integrations map[string]json.RawMessage
	if json.Unmarshal(raw, &integrations) != nil {
		return true
	}
	value, ok := integrations["codex"]
	if !ok {
		return true
	}
	var enabled bool
	if json.Unmarshal(value, &enabled) != nil {
		return true
	}
	return enabled
}
