package google

import (
	"net/url"
	"strings"
)

const (
	maxSchemaDepth = 24
	maxSchemaNodes = 1024
	maxSchemaDeref = 16
)

var allowedSchemaTypes = map[string]struct{}{
	"string": {}, "integer": {}, "number": {}, "boolean": {}, "array": {}, "object": {},
}

func SanitizeToolParameters(raw any) (out map[string]any) {
	defer func() {
		if recover() != nil {
			out = map[string]any{"type": "object", "properties": map[string]any{}}
		}
	}()
	defs := map[string]any{}
	collectDefs(raw, defs)
	state := schemaState{remaining: maxSchemaNodes, active: map[string]struct{}{}}
	root := sanitizeSchema(raw, defs, 0, 0, false, &state)
	if root == nil {
		root = map[string]any{}
	}
	root["type"] = "object"
	if _, ok := root["properties"].(map[string]any); !ok {
		root["properties"] = map[string]any{}
	}
	return root
}

type schemaState struct {
	remaining int
	active    map[string]struct{}
}

func collectDefs(node any, defs map[string]any) {
	obj, ok := node.(map[string]any)
	if !ok {
		return
	}
	for _, bag := range []string{"$defs", "definitions"} {
		group, ok := obj[bag].(map[string]any)
		if !ok {
			continue
		}
		for name, value := range group {
			if _, exists := defs[name]; !exists {
				defs[name] = value
			}
		}
	}
}

func sanitizeSchema(node any, defs map[string]any, depth, refDepth int, preserveNull bool, state *schemaState) map[string]any {
	if state.remaining <= 0 || depth >= maxSchemaDepth {
		return map[string]any{}
	}
	state.remaining--
	obj, ok := node.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	if ref, _ := obj["$ref"].(string); ref != "" && refDepth < maxSchemaDeref {
		if target, ok := resolveRef(ref, defs).(map[string]any); ok {
			if _, loop := state.active[ref]; loop {
				return map[string]any{}
			}
			state.active[ref] = struct{}{}
			merged := mergeRef(target, obj)
			out := sanitizeSchema(merged, defs, depth, refDepth+1, preserveNull, state)
			delete(state.active, ref)
			return out
		}
	}
	out := map[string]any{}
	normalizeType(obj["type"], out, preserveNull)
	if v, ok := obj["nullable"].(bool); ok {
		out["nullable"] = v
	}
	if v, ok := obj["description"].(string); ok {
		out["description"] = v
	}
	if v, ok := obj["format"].(string); ok {
		out["format"] = v
	}
	if enum := sanitizeEnum(obj["enum"]); enum != nil {
		out["enum"] = enum
	} else if c, ok := obj["const"].(string); ok {
		out["enum"] = []string{c}
	}
	if props := sanitizeProperties(obj["properties"], defs, depth, refDepth, state); props != nil {
		out["properties"] = props
		if required, ok := obj["required"].([]any); ok {
			var keep []string
			seen := map[string]struct{}{}
			for _, item := range required {
				name, ok := item.(string)
				if !ok {
					continue
				}
				if _, exists := props[name]; !exists {
					continue
				}
				if _, dup := seen[name]; dup {
					continue
				}
				seen[name] = struct{}{}
				keep = append(keep, name)
			}
			if len(keep) > 0 {
				out["required"] = keep
			}
		}
	}
	if items, ok := obj["items"].(map[string]any); ok && state.remaining > 0 {
		if child := sanitizeSchema(items, defs, depth+1, refDepth, false, state); child != nil {
			out["items"] = child
		}
	}
	if anyOf, exists := obj["anyOf"]; exists && state.remaining > 0 {
		for k, v := range normalizeAnyOf(anyOf, defs, depth, refDepth, state) {
			out[k] = v
		}
	}
	return out
}

func sanitizeProperties(value any, defs map[string]any, depth, refDepth int, state *schemaState) map[string]any {
	obj, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := map[string]any{}
	for name, child := range obj {
		if state.remaining <= 0 {
			break
		}
		out[name] = sanitizeSchema(child, defs, depth+1, refDepth, false, state)
	}
	return out
}

func normalizeAnyOf(value any, defs map[string]any, depth, refDepth int, state *schemaState) map[string]any {
	arr, ok := value.([]any)
	if !ok || len(arr) == 0 {
		return map[string]any{}
	}
	var schemas []map[string]any
	for _, item := range arr {
		if state.remaining <= 0 {
			return map[string]any{}
		}
		schemas = append(schemas, sanitizeSchema(item, defs, depth+1, refDepth, true, state))
	}
	var nonNull, onlyNull []map[string]any
	for _, schema := range schemas {
		if schema["type"] == "null" {
			onlyNull = append(onlyNull, schema)
		} else {
			nonNull = append(nonNull, schema)
		}
	}
	if len(nonNull) == 1 && len(onlyNull) > 0 {
		out := map[string]any{}
		for k, v := range nonNull[0] {
			out[k] = v
		}
		out["nullable"] = true
		return out
	}
	return map[string]any{}
}

func normalizeType(value any, out map[string]any, preserveNull bool) {
	var candidates []any
	switch typed := value.(type) {
	case []any:
		candidates = typed
	default:
		candidates = []any{value}
	}
	sawNull := false
	for _, candidate := range candidates {
		text, ok := candidate.(string)
		if !ok {
			continue
		}
		text = strings.ToLower(text)
		if text == "null" {
			sawNull = true
			continue
		}
		if _, ok := out["type"]; !ok {
			if _, allowed := allowedSchemaTypes[text]; allowed {
				out["type"] = text
			}
		}
	}
	if !sawNull {
		return
	}
	if _, ok := out["type"]; ok || !preserveNull {
		out["nullable"] = true
		return
	}
	out["type"] = "null"
}

func sanitizeEnum(value any) []string {
	arr, ok := value.([]any)
	if !ok {
		return nil
	}
	var out []string
	seen := map[string]struct{}{}
	for _, item := range arr {
		text, ok := item.(string)
		if !ok {
			continue
		}
		if _, dup := seen[text]; dup {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func resolveRef(ref string, defs map[string]any) any {
	if !strings.HasPrefix(ref, "#/$defs/") && !strings.HasPrefix(ref, "#/definitions/") {
		return nil
	}
	name := ref
	if rest, ok := strings.CutPrefix(ref, "#/$defs/"); ok {
		name = rest
	} else if rest, ok := strings.CutPrefix(ref, "#/definitions/"); ok {
		name = rest
	}
	name = strings.ReplaceAll(strings.ReplaceAll(name, "~1", "/"), "~0", "~")
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	return defs[name]
}

func mergeRef(target, overlay map[string]any) map[string]any {
	merged := map[string]any{}
	if v, ok := target["$ref"]; ok {
		merged["$ref"] = v
	}
	for _, key := range []string{"type", "nullable", "description", "format", "enum", "const", "properties", "items", "required", "anyOf"} {
		if v, ok := overlay[key]; ok {
			merged[key] = v
		} else if v, ok := target[key]; ok {
			merged[key] = v
		}
	}
	return merged
}
