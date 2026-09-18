package kiro

import (
	"fmt"
	"strings"
)

const (
	maxKiroSchemaDepth = 24
	maxKiroSchemaNodes = 1024
)

// Keywords Kiro/Bedrock reject at schema-object positions ("ValidationException:
// Invalid tool use format" / TOOL_SCHEMA_INVALID). Never treat these strings as
// property names; properties/$defs maps keep every key.
var kiroRejectedSchemaKeywords = map[string]struct{}{
	"additionalProperties":  {},
	"pattern":               {},
	"format":                {},
	"minLength":             {},
	"maxLength":             {},
	"minimum":               {},
	"maximum":               {},
	"exclusiveMinimum":      {},
	"exclusiveMaximum":      {},
	"multipleOf":            {},
	"minItems":              {},
	"maxItems":              {},
	"uniqueItems":           {},
	"minProperties":         {},
	"maxProperties":         {},
	"contentEncoding":       {},
	"contentMediaType":      {},
	"$schema":               {},
	"patternProperties":     {},
	"propertyNames":         {},
	"dependentSchemas":      {},
	"dependentRequired":     {},
	"if":                    {},
	"then":                  {},
	"else":                  {},
	"contains":              {},
	"unevaluatedProperties": {},
	"unevaluatedItems":      {},
	"encrypted":             {},
}

var kiroSchemaMapKeys = map[string]struct{}{
	"properties":  {},
	"$defs":       {},
	"definitions": {},
}

type kiroSchemaState struct {
	remaining int
}

func SanitizeToolSchema(raw any) (map[string]any, error) {
	state := kiroSchemaState{remaining: maxKiroSchemaNodes}
	cleaned, err := sanitizeKiroSchema(raw, 0, &state)
	if err != nil {
		return nil, err
	}
	return ensureKiroRootObject(cleaned)
}

func sanitizeKiroSchema(node any, depth int, state *kiroSchemaState) (any, error) {
	if state.remaining <= 0 {
		return nil, fmt.Errorf("Kiro tool schema exceeded node bound")
	}
	if depth >= maxKiroSchemaDepth {
		return nil, fmt.Errorf("Kiro tool schema exceeded depth bound")
	}
	state.remaining--
	switch typed := node.(type) {
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			child, err := sanitizeKiroSchema(item, depth+1, state)
			if err != nil {
				return nil, err
			}
			out[i] = child
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if _, rejected := kiroRejectedSchemaKeywords[key]; rejected {
				continue
			}
			if key == "required" {
				required := sanitizeKiroRequired(child)
				if required == nil {
					continue
				}
				out[key] = required
				continue
			}
			var next any
			var err error
			if _, isMap := kiroSchemaMapKeys[key]; isMap {
				next, err = sanitizeKiroSchemaMap(child, depth+1, state)
			} else {
				next, err = sanitizeKiroSchema(child, depth+1, state)
			}
			if err != nil {
				return nil, err
			}
			out[key] = next
		}
		return out, nil
	default:
		return node, nil
	}
}

func sanitizeKiroSchemaMap(node any, depth int, state *kiroSchemaState) (any, error) {
	obj, ok := node.(map[string]any)
	if !ok {
		return sanitizeKiroSchema(node, depth, state)
	}
	out := make(map[string]any, len(obj))
	for name, child := range obj {
		next, err := sanitizeKiroSchema(child, depth, state)
		if err != nil {
			return nil, err
		}
		out[name] = next
	}
	return out, nil
}

func sanitizeKiroRequired(value any) []any {
	arr, ok := value.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}
	out := make([]any, 0, len(arr))
	seen := map[string]struct{}{}
	for _, item := range arr {
		name, ok := item.(string)
		if !ok || strings.TrimSpace(name) == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ensureKiroRootObject(node any) (map[string]any, error) {
	obj, ok := node.(map[string]any)
	if !ok || obj == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}, nil
	}
	if err := flattenKiroRootComposition(obj); err != nil {
		return nil, err
	}
	obj["type"] = "object"
	if _, ok := obj["properties"].(map[string]any); !ok {
		if _, exists := obj["properties"]; !exists {
			obj["properties"] = map[string]any{}
		}
	}
	return obj, nil
}

func flattenKiroRootComposition(obj map[string]any) error {
	allOf, hasAllOf := obj["allOf"].([]any)
	oneOf, hasOneOf := obj["oneOf"].([]any)
	anyOf, hasAnyOf := obj["anyOf"].([]any)
	if !hasAllOf && !hasOneOf && !hasAnyOf {
		return nil
	}
	if hasOneOf || hasAnyOf {
		variants := oneOf
		label := "oneOf"
		if hasAnyOf {
			if hasOneOf {
				return fmt.Errorf("Kiro tool schema uses unsupported root oneOf/anyOf composition")
			}
			variants = anyOf
			label = "anyOf"
		}
		objects := objectSchemaVariants(variants)
		if len(objects) != 1 || hasAllOf {
			return fmt.Errorf("Kiro tool schema uses unsupported root %s; cannot preserve semantics", label)
		}
		if err := mergeKiroObjectSchema(obj, objects[0]); err != nil {
			return err
		}
		delete(obj, "oneOf")
		delete(obj, "anyOf")
		return nil
	}
	for _, item := range allOf {
		child, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("Kiro tool schema uses unsupported root allOf member")
		}
		if err := mergeKiroObjectSchema(obj, child); err != nil {
			return err
		}
	}
	delete(obj, "allOf")
	return nil
}

func objectSchemaVariants(variants []any) []map[string]any {
	var out []map[string]any
	for _, item := range variants {
		child, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if isNullSchema(child) {
			continue
		}
		out = append(out, child)
	}
	return out
}

func isNullSchema(obj map[string]any) bool {
	if typ, ok := obj["type"].(string); ok {
		return typ == "null"
	}
	return false
}

func mergeKiroObjectSchema(dst, src map[string]any) error {
	if typ, ok := src["type"].(string); ok && typ != "object" && typ != "" {
		return fmt.Errorf("Kiro tool schema root composition is not an object")
	}
	if props, ok := src["properties"].(map[string]any); ok {
		dstProps, _ := dst["properties"].(map[string]any)
		if dstProps == nil {
			dstProps = map[string]any{}
			dst["properties"] = dstProps
		}
		for name, schema := range props {
			dstProps[name] = schema
		}
	}
	if required, ok := src["required"].([]any); ok {
		dstReq, _ := dst["required"].([]any)
		seen := map[string]struct{}{}
		for _, item := range dstReq {
			if name, ok := item.(string); ok {
				seen[name] = struct{}{}
			}
		}
		for _, item := range required {
			name, ok := item.(string)
			if !ok {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			dstReq = append(dstReq, name)
		}
		if len(dstReq) > 0 {
			dst["required"] = dstReq
		}
	}
	return nil
}
