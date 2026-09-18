package router

import (
	"fmt"
	"sort"
	"strings"
)

type AliasTable struct {
	Providers       map[string]struct{}
	ProviderByAlias map[string]string
	ModelByProvider map[string]map[string]string
}

type AmbiguousAliasError struct {
	Alias      string
	Candidates []string
}

func (e *AmbiguousAliasError) Error() string {
	if e == nil {
		return "model alias is ambiguous"
	}
	return fmt.Sprintf("model alias %q is ambiguous: %s", e.Alias, strings.Join(e.Candidates, ", "))
}

func Resolve(selector string, namespaces map[string]string, table AliasTable) (Route, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return ParseExplicitWithCodexAccounts(selector, namespaces)
	}
	if !strings.Contains(selector, "/") {
		return resolveBareModelAlias(selector, namespaces, table)
	}
	return ParseExplicitWithCodexAccounts(rewriteQualified(selector, namespaces, table), namespaces)
}

func rewriteQualified(selector string, namespaces map[string]string, table AliasTable) string {
	provider, model, found := strings.Cut(selector, "/")
	if !found {
		return selector
	}
	if _, ok := namespaces[provider]; ok {
		if aliases := table.ModelByProvider["openai"]; aliases != nil {
			if mapped, ok := aliases[model]; ok {
				model = mapped
			}
		}
		return provider + "/" + model
	}
	canonicalProvider := resolveProviderID(provider, table)
	if aliases := table.ModelByProvider[canonicalProvider]; aliases != nil {
		if mapped, ok := aliases[model]; ok {
			model = mapped
		}
	}
	return canonicalProvider + "/" + model
}

func (t AliasTable) DisplaySelector(canonical string) string {
	canonical = strings.TrimSpace(canonical)
	provider, model, found := strings.Cut(canonical, "/")
	if !found || provider == "" || model == "" {
		return canonical
	}
	providerAlias := uniqueProviderAlias(t, provider)
	modelAlias := uniqueModelAlias(t, provider, model)
	if providerAlias == "" && modelAlias == "" {
		return canonical
	}
	if providerAlias == "" {
		providerAlias = provider
	}
	if modelAlias == "" {
		modelAlias = model
	}
	return providerAlias + "/" + modelAlias
}

func uniqueProviderAlias(table AliasTable, provider string) string {
	found := ""
	for alias, id := range table.ProviderByAlias {
		if id != provider {
			continue
		}
		if found != "" && found != alias {
			return ""
		}
		found = alias
	}
	return found
}

func uniqueModelAlias(table AliasTable, provider, canonicalModel string) string {
	aliases := table.ModelByProvider[provider]
	found := ""
	for alias, mapped := range aliases {
		if mapped != canonicalModel {
			continue
		}
		if found != "" && found != alias {
			return ""
		}
		found = alias
	}
	return found
}

func (t AliasTable) Clone() AliasTable {
	out := AliasTable{
		Providers:       make(map[string]struct{}, len(t.Providers)),
		ProviderByAlias: make(map[string]string, len(t.ProviderByAlias)),
		ModelByProvider: make(map[string]map[string]string, len(t.ModelByProvider)),
	}
	for id := range t.Providers {
		out.Providers[id] = struct{}{}
	}
	for alias, id := range t.ProviderByAlias {
		out.ProviderByAlias[alias] = id
	}
	for provider, aliases := range t.ModelByProvider {
		cloned := make(map[string]string, len(aliases))
		for alias, canonical := range aliases {
			cloned[alias] = canonical
		}
		out.ModelByProvider[provider] = cloned
	}
	return out
}

func resolveProviderID(provider string, table AliasTable) string {
	if _, ok := table.Providers[provider]; ok {
		return provider
	}
	if mapped, ok := table.ProviderByAlias[provider]; ok {
		return mapped
	}
	return provider
}

func resolveBareModelAlias(alias string, namespaces map[string]string, table AliasTable) (Route, error) {
	candidates := make([]string, 0)
	var match Route
	for provider, aliases := range table.ModelByProvider {
		canonical, ok := aliases[alias]
		if !ok {
			continue
		}
		candidates = append(candidates, provider+"/"+canonical)
		match = Route{Provider: provider, Model: canonical}
	}
	sort.Strings(candidates)
	switch len(candidates) {
	case 1:
		return match, nil
	case 0:
		return ParseExplicitWithCodexAccounts(alias, namespaces)
	default:
		return Route{}, &AmbiguousAliasError{Alias: alias, Candidates: candidates}
	}
}
