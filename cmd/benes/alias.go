package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

const aliasUsage = `benes: usage: alias list|set|remove
  alias set <provider-id> <provider-alias>
  alias set <provider-id>/<canonical-model> <model-alias>
  alias remove <provider-id>
  alias remove <provider-id>/<canonical-model>`

func runAlias(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		return runAliasList(jsonOut, stdout, stderr, deps)
	case "set":
		return runAliasSet(args[1:], jsonOut, stdout, stderr, deps)
	case "remove", "delete":
		return runAliasRemove(args[1:], jsonOut, stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, aliasUsage)
		return 2
	}
}

type aliasRow struct {
	Kind     string `json:"kind"`
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	Alias    string `json:"alias"`
	Selector string `json:"selector"`
}

func runAliasList(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	rows, err := listAliasRows(deps)
	if err != nil {
		return serveFailure(stderr, "load aliases", err)
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"aliases": rows})
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "No aliases configured.")
		return 0
	}
	for _, row := range rows {
		fmt.Fprintf(stdout, "%s  %s\n", row.Selector, row.Alias)
	}
	return 0
}

func runAliasSet(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, aliasUsage)
		return 2
	}
	target := strings.TrimSpace(args[0])
	alias := strings.TrimSpace(args[1])
	if !config.ValidAliasToken(alias) {
		fmt.Fprintln(stderr, "benes: alias must be a single token without slashes or spaces")
		return 2
	}
	providers, err := loadProviderAliasRecords(deps)
	if err != nil {
		return serveFailure(stderr, "load providers", err)
	}
	providerID, canonicalModel, ok := splitAliasTarget(target)
	if !ok {
		fmt.Fprintln(stderr, "benes: set target must be a provider id or provider/model")
		return 2
	}
	if _, exists := providers[providerID]; !exists {
		fmt.Fprintf(stderr, "benes: unknown provider %s\n", providerID)
		return 1
	}
	if canonicalModel == "" {
		if _, exists := providers[alias]; exists {
			fmt.Fprintf(stderr, "benes: alias %q collides with provider id %s\n", alias, alias)
			return 1
		}
		if owner := providerUsingAlias(providers, alias); owner != "" && owner != providerID {
			fmt.Fprintf(stderr, "benes: alias %q is already used by provider %s\n", alias, owner)
			return 1
		}
		encoded, err := json.Marshal(alias)
		if err != nil {
			return serveFailure(stderr, "encode alias", err)
		}
		if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
			return tx.Set(config.JSONPath("providers", providerID, "alias"), encoded)
		}); err != nil {
			return 1
		}
		if jsonOut {
			return encodeJSON(stdout, stderr, map[string]any{"kind": "provider", "provider": providerID, "alias": alias})
		}
		fmt.Fprintf(stdout, "Saved provider alias %s -> %s.\nRestart the listener to apply routing aliases.\n", alias, providerID)
		return 0
	}
	encoded, err := json.Marshal(alias)
	if err != nil {
		return serveFailure(stderr, "encode alias", err)
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("providers", providerID, "modelAliases", canonicalModel), encoded)
	}); err != nil {
		return 1
	}
	selector := providerID + "/" + canonicalModel
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"kind": "model", "provider": providerID, "model": canonicalModel, "alias": alias})
	}
	fmt.Fprintf(stdout, "Saved model alias %s -> %s.\nRestart the listener to apply routing aliases.\n", alias, selector)
	return 0
}

func runAliasRemove(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, aliasUsage)
		return 2
	}
	target := strings.TrimSpace(args[0])
	providers, err := loadProviderAliasRecords(deps)
	if err != nil {
		return serveFailure(stderr, "load providers", err)
	}
	providerID, canonicalModel, ok := splitAliasTarget(target)
	if !ok {
		fmt.Fprintln(stderr, "benes: remove target must be a provider id or provider/model")
		return 2
	}
	record, exists := providers[providerID]
	if !exists {
		fmt.Fprintf(stderr, "benes: unknown provider %s\n", providerID)
		return 1
	}
	if canonicalModel == "" {
		if strings.TrimSpace(record.Alias) == "" {
			fmt.Fprintf(stderr, "benes: provider %s has no alias\n", providerID)
			return 1
		}
		if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
			return tx.Delete(config.JSONPath("providers", providerID, "alias"))
		}); err != nil {
			return 1
		}
		if jsonOut {
			return encodeJSON(stdout, stderr, map[string]any{"removed": providerID, "kind": "provider"})
		}
		fmt.Fprintf(stdout, "Removed provider alias from %s.\nRestart the listener to apply routing aliases.\n", providerID)
		return 0
	}
	if _, ok := record.ModelAliases[canonicalModel]; !ok {
		fmt.Fprintf(stderr, "benes: provider %s has no model alias for %s\n", providerID, canonicalModel)
		return 1
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Delete(config.JSONPath("providers", providerID, "modelAliases", canonicalModel))
	}); err != nil {
		return 1
	}
	selector := providerID + "/" + canonicalModel
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"removed": selector, "kind": "model"})
	}
	fmt.Fprintf(stdout, "Removed model alias from %s.\nRestart the listener to apply routing aliases.\n", selector)
	return 0
}

type providerAliasRecord struct {
	Alias        string            `json:"alias"`
	ModelAliases map[string]string `json:"modelAliases"`
}

func listAliasRows(deps commandDependencies) ([]aliasRow, error) {
	providers, err := loadProviderAliasRecords(deps)
	if err != nil {
		return nil, err
	}
	rows := make([]aliasRow, 0)
	for _, id := range sortedKeys(providers) {
		record := providers[id]
		if alias := strings.TrimSpace(record.Alias); alias != "" {
			rows = append(rows, aliasRow{Kind: "provider", Provider: id, Alias: alias, Selector: id})
		}
		for _, canonical := range sortedKeys(record.ModelAliases) {
			alias := strings.TrimSpace(record.ModelAliases[canonical])
			if alias == "" {
				continue
			}
			rows = append(rows, aliasRow{
				Kind:     "model",
				Provider: id,
				Model:    canonical,
				Alias:    alias,
				Selector: id + "/" + canonical,
			})
		}
	}
	return rows, nil
}

func loadProviderAliasRecords(deps commandDependencies) (map[string]providerAliasRecord, error) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return nil, err
	}
	raw, ok := root["providers"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return map[string]providerAliasRecord{}, nil
	}
	var providers map[string]json.RawMessage
	if err := json.Unmarshal(raw, &providers); err != nil {
		return nil, err
	}
	out := make(map[string]providerAliasRecord, len(providers))
	for id, body := range providers {
		var record providerAliasRecord
		if json.Unmarshal(body, &record) != nil {
			record = providerAliasRecord{}
		}
		if record.ModelAliases == nil {
			record.ModelAliases = map[string]string{}
		}
		out[id] = record
	}
	return out, nil
}

func splitAliasTarget(target string) (providerID, canonicalModel string, ok bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", false
	}
	if !strings.Contains(target, "/") {
		if strings.TrimSpace(target) == "" {
			return "", "", false
		}
		return target, "", true
	}
	providerID, canonicalModel, found := strings.Cut(target, "/")
	if !found || providerID == "" || canonicalModel == "" {
		return "", "", false
	}
	return providerID, canonicalModel, true
}

func providerUsingAlias(providers map[string]providerAliasRecord, alias string) string {
	for _, id := range sortedKeys(providers) {
		if strings.TrimSpace(providers[id].Alias) == alias {
			return id
		}
	}
	return ""
}
