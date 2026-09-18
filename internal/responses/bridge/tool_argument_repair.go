package bridge

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

const (
	maxSafeToolInteger = float64(1<<53 - 1)
	maxToolSchemaDepth = 64
)

type toolArgumentRepairResult struct {
	value   any
	changed bool
}

func repairToolArguments(args string, parameters map[string]any) string {
	if len(parameters) == 0 || args == "" || !containsASCIIDigit(args) {
		return args
	}
	decoder := json.NewDecoder(strings.NewReader(args))
	decoder.UseNumber()
	var parsed any
	if err := decoder.Decode(&parsed); err != nil {
		return args
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return args
	}
	result := repairToolArgumentValue(parsed, parameters, parameters, 0)
	if !result.changed {
		return args
	}
	encoded, err := json.Marshal(result.value)
	if err != nil {
		return args
	}
	return string(encoded)
}

func containsASCIIDigit(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] >= '0' && value[i] <= '9' {
			return true
		}
	}
	return false
}

func repairToolArgumentValue(value any, schema, root map[string]any, depth int) toolArgumentRepairResult {
	if depth > maxToolSchemaDepth {
		return toolArgumentRepairResult{value: value}
	}
	resolved := resolveToolSchemaRef(schema, root, map[string]struct{}{})
	if number, ok := value.(json.Number); ok {
		if resolved == nil {
			return toolArgumentRepairResult{value: value}
		}
		branches := toolSchemaCompositionBranches(resolved)
		integerDeclared := declaresToolSchemaType(resolved, "integer") || branchesDeclareToolSchemaType(branches, "integer")
		safeValue, safelyIntegral := safeIntegralToolNumber(number)
		if !integerDeclared && safelyIntegral {
			stringDeclared := declaresToolSchemaType(resolved, "string") || branchesDeclareToolSchemaType(branches, "string")
			numericDeclared := declaresToolSchemaNumeric(resolved) || branchesDeclareToolSchemaNumeric(branches)
			if stringDeclared && !numericDeclared {
				return toolArgumentRepairResult{value: strconv.FormatInt(int64(safeValue), 10), changed: true}
			}
		}
		if !integerDeclared || !safelyIntegral {
			return toolArgumentRepairResult{value: value}
		}
		canonical := json.Number(strconv.FormatInt(int64(safeValue), 10))
		if canonical.String() == number.String() {
			return toolArgumentRepairResult{value: value}
		}
		return toolArgumentRepairResult{value: canonical, changed: true}
	}
	if array, ok := value.([]any); ok {
		var itemSchema map[string]any
		if resolved != nil {
			itemSchema = asToolSchema(resolved["items"])
		}
		next := make([]any, len(array))
		changed := false
		for i, entry := range array {
			result := repairToolArgumentValue(entry, itemSchema, root, depth+1)
			next[i] = result.value
			changed = changed || result.changed
		}
		if changed {
			return toolArgumentRepairResult{value: next, changed: true}
		}
		return toolArgumentRepairResult{value: value}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return toolArgumentRepairResult{value: value}
	}
	var properties map[string]any
	var additional map[string]any
	if resolved != nil {
		properties = asToolSchema(resolved["properties"])
		additional = asToolSchema(resolved["additionalProperties"])
	}
	next := make(map[string]any, len(object))
	changed := false
	for key, entry := range object {
		childSchema := additional
		if properties != nil {
			if candidate := asToolSchema(properties[key]); candidate != nil {
				childSchema = candidate
			}
		}
		result := repairToolArgumentValue(entry, childSchema, root, depth+1)
		next[key] = result.value
		changed = changed || result.changed
	}
	if changed {
		return toolArgumentRepairResult{value: next, changed: true}
	}
	return toolArgumentRepairResult{value: value}
}

func safeIntegralToolNumber(number json.Number) (float64, bool) {
	value, err := strconv.ParseFloat(number.String(), 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) || math.Trunc(value) != value || math.Abs(value) > maxSafeToolInteger {
		return 0, false
	}
	return value, true
}

func asToolSchema(value any) map[string]any {
	schema, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return schema
}

func declaresToolSchemaType(schema map[string]any, want string) bool {
	if schema == nil {
		return false
	}
	switch declared := schema["type"].(type) {
	case string:
		return declared == want
	case []any:
		for _, entry := range declared {
			if value, ok := entry.(string); ok && value == want {
				return true
			}
		}
	}
	return false
}

func declaresToolSchemaNumeric(schema map[string]any) bool {
	return declaresToolSchemaType(schema, "integer") || declaresToolSchemaType(schema, "number")
}

func branchesDeclareToolSchemaType(branches []map[string]any, want string) bool {
	for _, branch := range branches {
		if declaresToolSchemaType(branch, want) {
			return true
		}
	}
	return false
}

func branchesDeclareToolSchemaNumeric(branches []map[string]any) bool {
	for _, branch := range branches {
		if declaresToolSchemaNumeric(branch) {
			return true
		}
	}
	return false
}

func toolSchemaCompositionBranches(schema map[string]any) []map[string]any {
	var branches []map[string]any
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		values, ok := schema[key].([]any)
		if !ok {
			continue
		}
		for _, value := range values {
			if branch := asToolSchema(value); branch != nil {
				branches = append(branches, branch)
			}
		}
	}
	return branches
}

func resolveToolSchemaRef(schema, root map[string]any, seen map[string]struct{}) map[string]any {
	if schema == nil {
		return nil
	}
	ref, ok := schema["$ref"].(string)
	if !ok || !strings.HasPrefix(ref, "#/") {
		return schema
	}
	if _, exists := seen[ref]; exists {
		return nil
	}
	seen[ref] = struct{}{}
	var node any = root
	for _, rawSegment := range strings.Split(ref[2:], "/") {
		segment := strings.ReplaceAll(strings.ReplaceAll(rawSegment, "~1", "/"), "~0", "~")
		current := asToolSchema(node)
		if current == nil {
			return nil
		}
		node = current[segment]
	}
	return resolveToolSchemaRef(asToolSchema(node), root, seen)
}
