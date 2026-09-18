package export

import "strings"

func buildOpencode(ctx Context) (any, error) {
	models := map[string]any{}
	for _, model := range Normalize(ctx.Models) {
		entry := map[string]any{"name": label(model)}
		if context := contextWindow(model); context > 0 {
			entry["limit"] = map[string]any{"context": context, "output": outputFor(context)}
		}
		models[model.Namespaced] = entry
	}
	options := map[string]any{"baseURL": ctx.BaseURL}
	if injectHeader(hostFromBase(ctx.BaseURL, ctx.Hostname)) {
		options["headers"] = map[string]any{"x-benes-api-key": "{env:" + OpenCodeAPIKeyEnv + "}"}
	} else {
		options["apiKey"] = "{env:" + OpenCodeAPIKeyEnv + "}"
	}
	return map[string]any{
		"$schema": schemaURL,
		"provider": map[string]any{
			ProviderID: map[string]any{
				"npm":     opencodeNPM,
				"name":    "Benes",
				"options": options,
				"models":  models,
			},
		},
	}, nil
}

func buildPi(ctx Context) (any, error) {
	models := []any{}
	for _, model := range Normalize(ctx.Models) {
		input := filterModalities(model.InputModalities, []string{"text", "image"})
		if input == nil {
			continue
		}
		entry := map[string]any{"id": model.Namespaced, "name": label(model), "input": stringAny(input)}
		if len(model.ReasoningEfforts) > 0 {
			entry["reasoning"] = true
			entry["thinkingLevelMap"] = thinkingMap(model.ReasoningEfforts)
		}
		if context := contextWindow(model); context > 0 {
			entry["contextWindow"] = context
			entry["maxTokens"] = outputFor(context)
		}
		models = append(models, entry)
	}
	return map[string]any{
		"providers": map[string]any{
			ProviderID: map[string]any{
				"baseUrl": ctx.BaseURL,
				"api":     piAPI,
				"apiKey":  loopbackKey,
				"models":  models,
			},
		},
	}, nil
}

func buildOmp(ctx Context) (any, error) {
	models := []any{}
	for _, model := range Normalize(ctx.Models) {
		input := filterModalities(model.InputModalities, []string{"text", "image"})
		if input == nil {
			continue
		}
		entry := map[string]any{"id": model.Namespaced, "name": label(model), "input": stringAny(input)}
		if model.Native && model.Provider == "openai" {
			entry["api"] = "openai-responses"
		}
		if context := contextWindow(model); context > 0 {
			entry["contextWindow"] = context
			entry["maxTokens"] = outputFor(context)
		}
		if efforts := ompEfforts(model); len(efforts) > 0 {
			thinking := map[string]any{"mode": "effort", "efforts": stringAny(efforts)}
			if def := strings.TrimSpace(strings.ToLower(model.DefaultReasoningEffort)); def != "" {
				for _, effort := range efforts {
					if effort == def {
						thinking["defaultLevel"] = def
						break
					}
				}
			}
			entry["reasoning"] = true
			entry["thinking"] = thinking
		}
		models = append(models, entry)
	}
	return map[string]any{
		"providers": map[string]any{
			ProviderID: map[string]any{
				"baseUrl": ctx.BaseURL,
				"api":     piAPI,
				"apiKey":  loopbackKey,
				"models":  models,
			},
		},
	}, nil
}

func buildHermes(ctx Context) (any, error) {
	models := []any{}
	for _, model := range Normalize(ctx.Models) {
		models = append(models, model.Namespaced)
	}
	block := map[string]any{
		"api":             ctx.BaseURL,
		"api_key":         "${" + HermesAPIKeyEnv + "}",
		"api_mode":        "chat_completions",
		"discover_models": false,
		"models":          models,
	}
	if injectHeader(hostFromBase(ctx.BaseURL, ctx.Hostname)) {
		block["extra_headers"] = map[string]any{"x-benes-api-key": "${" + HermesAPIKeyEnv + "}"}
	}
	return map[string]any{"providers": map[string]any{ProviderID: block}}, nil
}

func buildOpenclaw(ctx Context) (any, error) {
	models := []any{}
	for _, model := range Normalize(ctx.Models) {
		entry := map[string]any{"id": model.Namespaced, "name": label(model)}
		if context := contextWindow(model); context > 0 {
			entry["contextWindow"] = context
		}
		models = append(models, entry)
	}
	block := map[string]any{
		"baseUrl": ctx.BaseURL,
		"apiKey":  "${" + OpenclawAPIKeyEnv + "}",
		"api":     "openai-completions",
		"models":  models,
	}
	if injectHeader(hostFromBase(ctx.BaseURL, ctx.Hostname)) {
		block["headers"] = map[string]any{"x-benes-api-key": "${" + OpenclawAPIKeyEnv + "}"}
	}
	return map[string]any{
		"models": map[string]any{
			"mode":      "merge",
			"providers": map[string]any{ProviderID: block},
		},
	}, nil
}

func buildKimi(ctx Context) (any, error) {
	models := map[string]any{}
	for _, model := range Normalize(ctx.Models) {
		context := contextWindow(model)
		if context <= 0 {
			continue
		}
		entry := map[string]any{
			"provider":         ProviderID,
			"model":            model.Namespaced,
			"max_context_size": context,
		}
		if model.DisplayName != "" {
			entry["display_name"] = model.DisplayName
		}
		models[ProviderID+"/"+model.Namespaced] = entry
	}
	return map[string]any{
		"providers": map[string]any{
			ProviderID: map[string]any{
				"type":     "openai",
				"base_url": ctx.BaseURL,
				"api_key":  loopbackKey,
			},
		},
		"models": models,
	}, nil
}

func buildGajae(ctx Context) (any, error) {
	models := []any{}
	for _, model := range Normalize(ctx.Models) {
		input := filterModalities(model.InputModalities, []string{"text", "image"})
		if input == nil {
			continue
		}
		entry := map[string]any{"id": model.Namespaced, "name": label(model), "input": stringAny(input)}
		if context := contextWindow(model); context > 0 {
			entry["contextWindow"] = context
			entry["maxTokens"] = outputFor(context)
		}
		models = append(models, entry)
	}
	return map[string]any{
		"providers": map[string]any{
			ProviderID: map[string]any{
				"baseUrl":   ctx.BaseURL,
				"apiKeyEnv": GajaeAPIKeyEnv,
				"api":       "openai-completions",
				"models":    models,
			},
		},
	}, nil
}

func buildDsh(ctx Context) (any, error) {
	models := []any{}
	for _, model := range Normalize(ctx.Models) {
		if ctx.Direct && (model.Native || model.Provider == "openai") {
			continue
		}
		input := dshModalities(model.InputModalities)
		if input == nil {
			continue
		}
		entry := map[string]any{"id": model.Namespaced, "name": label(model), "input": stringAny(input)}
		if context := contextWindow(model); context > 0 {
			entry["contextWindow"] = context
		}
		if efforts := dshEfforts(model); efforts != nil {
			entry["reasoningEfforts"] = efforts
		}
		models = append(models, entry)
	}
	return map[string]any{
		"llm-pi-ai": map[string]any{
			"providers": map[string]any{
				ProviderID: map[string]any{
					"displayName": "Benes",
					"api":         "openai-responses",
					"baseURL":     ctx.BaseURL,
					"headers":     map[string]any{"Authorization": "Bearer benes_data_dsh"},
					"models":      models,
				},
			},
		},
	}, nil
}

func buildMcode(ctx Context) (any, error) {
	models := map[string]any{}
	for _, model := range Normalize(ctx.Models) {
		models[model.Namespaced] = map[string]any{}
	}
	base := strings.TrimSuffix(strings.TrimSuffix(ctx.BaseURL, "/"), "/v1")
	return map[string]any{
		"custom_provider": map[string]any{
			ProviderID: map[string]any{
				"name":    "Benes",
				"kind":    "custom",
				"enabled": true,
				"api":     "anthropic-messages",
				"options": map[string]any{
					"apiKey":   loopbackKey,
					"baseURL":  base,
					"authMode": "api-key",
				},
				"models": models,
			},
		},
	}, nil
}

func filterModalities(declared, accepted []string) []string {
	if len(declared) == 0 {
		return []string{"text"}
	}
	keep := []string{}
	for _, value := range declared {
		ok := false
		for _, allow := range accepted {
			if value == allow {
				ok = true
				break
			}
		}
		if !ok {
			continue
		}
		dup := false
		for _, existing := range keep {
			if existing == value {
				dup = true
				break
			}
		}
		if !dup {
			keep = append(keep, value)
		}
	}
	if len(keep) == 0 {
		return nil
	}
	return keep
}

func dshModalities(declared []string) []string {
	if len(declared) == 0 {
		return []string{"text"}
	}
	keep := filterModalities(declared, []string{"text", "image"})
	if keep != nil {
		return keep
	}
	audioOnly := true
	for _, value := range declared {
		if value != "audio" {
			audioOnly = false
			break
		}
	}
	if audioOnly {
		return nil
	}
	return []string{"text"}
}

func thinkingMap(efforts []string) map[string]any {
	has := map[string]bool{}
	for _, effort := range efforts {
		has[effort] = true
	}
	max := any(nil)
	if has["max"] {
		max = "max"
	} else if has["ultra"] {
		max = "ultra"
	}
	off := any(nil)
	if has["none"] {
		off = "none"
	}
	pick := func(name string) any {
		if has[name] {
			return name
		}
		return nil
	}
	return map[string]any{
		"off":     off,
		"minimal": pick("minimal"),
		"low":     pick("low"),
		"medium":  pick("medium"),
		"high":    pick("high"),
		"xhigh":   pick("xhigh"),
		"max":     max,
	}
}

func ompEfforts(model Model) []string {
	vocab := map[string]struct{}{"minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {}}
	out := []string{}
	for _, effort := range model.ReasoningEfforts {
		normalized := strings.ToLower(strings.TrimSpace(effort))
		if _, ok := vocab[normalized]; !ok {
			continue
		}
		dup := false
		for _, existing := range out {
			if existing == normalized {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, normalized)
		}
	}
	return out
}

func dshEfforts(model Model) map[string]any {
	offered := map[string]struct{}{}
	for _, raw := range model.ReasoningEfforts {
		effort := strings.ToLower(strings.TrimSpace(raw))
		if effort == "ultra" || effort == "low" || effort == "medium" || effort == "high" || effort == "xhigh" || effort == "max" {
			offered[effort] = struct{}{}
		}
	}
	if len(offered) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, effort := range []string{"low", "medium", "high", "xhigh"} {
		if _, ok := offered[effort]; ok {
			out[effort] = effort
		}
	}
	if _, ok := offered["max"]; ok {
		out["max"] = "max"
	} else if _, ok := offered["ultra"]; ok {
		out["max"] = "ultra"
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stringAny(in []string) []any {
	out := make([]any, len(in))
	for i, value := range in {
		out[i] = value
	}
	return out
}

func providerModels(doc any, path ...string) []any {
	cur, _ := doc.(map[string]any)
	for _, key := range path {
		next, _ := cur[key].(map[string]any)
		cur = next
	}
	if cur == nil {
		return nil
	}
	if models, ok := cur["models"].([]any); ok {
		return models
	}
	if models, ok := cur["models"].(map[string]any); ok {
		out := make([]any, 0, len(models))
		for _, value := range models {
			out = append(out, value)
		}
		return out
	}
	return nil
}

func countMissingContext(models []any, field string) (int, int) {
	missing := 0
	for _, item := range models {
		row, _ := item.(map[string]any)
		if row == nil {
			continue
		}
		if _, ok := row[field]; !ok {
			if limit, ok := row["limit"].(map[string]any); ok {
				if _, ok := limit["context"]; ok {
					continue
				}
			}
			missing++
		}
	}
	return len(models), missing
}

func summarizeOpencode(doc any) (int, int) {
	models := providerModels(doc, "provider", ProviderID)
	return countMissingContext(models, "limit")
}
func summarizePi(doc any) (int, int) {
	return countMissingContext(providerModels(doc, "providers", ProviderID), "contextWindow")
}
func summarizeOmp(doc any) (int, int) {
	return countMissingContext(providerModels(doc, "providers", ProviderID), "contextWindow")
}
func summarizeHermes(doc any) (int, int) {
	models := providerModels(doc, "providers", ProviderID)
	return len(models), 0
}
func summarizeOpenclaw(doc any) (int, int) {
	return countMissingContext(providerModels(doc, "models", "providers", ProviderID), "contextWindow")
}
func summarizeKimi(doc any) (int, int) {
	root, _ := doc.(map[string]any)
	models, _ := root["models"].(map[string]any)
	return len(models), 0
}
func summarizeGajae(doc any) (int, int) {
	return countMissingContext(providerModels(doc, "providers", ProviderID), "contextWindow")
}
func summarizeDsh(doc any) (int, int) {
	return countMissingContext(providerModels(doc, "llm-pi-ai", "providers", ProviderID), "contextWindow")
}
func summarizeMcode(doc any) (int, int) {
	models := providerModels(doc, "custom_provider", ProviderID)
	return len(models), 0
}
