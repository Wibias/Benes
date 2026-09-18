package google

import "testing"

func TestSanitizeToolParametersAllowlistsAndInlinesDefs(t *testing.T) {
	got := SanitizeToolParameters(map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$defs": map[string]any{
			"q": map[string]any{"type": "string", "description": "query"},
		},
		"properties": map[string]any{
			"q":   map[string]any{"$ref": "#/$defs/q"},
			"bad": map[string]any{"type": "string", "pattern": "^x$", "minLength": 1.0},
		},
		"required": []any{"q", "missing"},
	})
	if got["type"] != "object" {
		t.Fatalf("type=%#v", got)
	}
	if _, ok := got["additionalProperties"]; ok {
		t.Fatal("dropped key leaked")
	}
	props := got["properties"].(map[string]any)
	q := props["q"].(map[string]any)
	if q["type"] != "string" || q["description"] != "query" {
		t.Fatalf("ref=%#v", q)
	}
	bad := props["bad"].(map[string]any)
	if _, ok := bad["pattern"]; ok {
		t.Fatalf("pattern leaked=%#v", bad)
	}
	req := got["required"].([]string)
	if len(req) != 1 || req[0] != "q" {
		t.Fatalf("required=%#v", req)
	}
}

func TestSanitizeToolParametersCollapsesNullableAnyOf(t *testing.T) {
	got := SanitizeToolParameters(map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "null"},
		},
	})
	if got["type"] != "object" {
		t.Fatalf("%#v", got)
	}
}
