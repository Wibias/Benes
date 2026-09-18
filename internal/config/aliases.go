package config

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	"github.com/Wibias/Benes/internal/router"
)

const reservedComboNamespace = "combo"

func ValidAliasToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsRune(token, '/') {
		return false
	}
	if strings.EqualFold(token, reservedComboNamespace) {
		return false
	}
	for _, r := range token {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func ProjectRouteAliases(disk DiskConfig) router.AliasTable {
	ids := make([]string, 0, len(disk.Providers))
	table := router.AliasTable{
		Providers:       make(map[string]struct{}, len(disk.Providers)),
		ProviderByAlias: make(map[string]string),
		ModelByProvider: make(map[string]map[string]string),
	}
	for id := range disk.Providers {
		table.Providers[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)

	claimed := make(map[string]string, len(ids))
	poisoned := make(map[string]struct{})
	for _, id := range ids {
		raw := disk.Providers[id]
		var body struct {
			Disabled     bool              `json:"disabled"`
			Alias        string            `json:"alias"`
			ModelAliases map[string]string `json:"modelAliases"`
		}
		if json.Unmarshal(raw, &body) != nil || body.Disabled {
			continue
		}
		alias := strings.TrimSpace(body.Alias)
		if ValidAliasToken(alias) {
			_, canonical := table.Providers[alias]
			_, bad := poisoned[alias]
			if !canonical && !bad {
				if owner, taken := claimed[alias]; taken && owner != id {
					delete(table.ProviderByAlias, alias)
					delete(claimed, alias)
					poisoned[alias] = struct{}{}
				} else {
					claimed[alias] = id
					table.ProviderByAlias[alias] = id
				}
			}
		}
		models := make(map[string]string)
		for canonical, mapped := range body.ModelAliases {
			canonical = strings.TrimSpace(canonical)
			mapped = strings.TrimSpace(mapped)
			if canonical == "" || !ValidAliasToken(mapped) {
				continue
			}
			models[mapped] = canonical
		}
		if len(models) > 0 {
			table.ModelByProvider[id] = models
		}
	}
	return table
}
