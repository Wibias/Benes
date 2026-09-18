package kiro

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestSanitizeToolSchemaStripsKeywordsButKeepsCollidingPropertyNames(t *testing.T) {
	got, err := SanitizeToolSchema(map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"required":             []any{},
		"properties": map[string]any{
			"format":  map[string]any{"type": "string", "format": "email", "minLength": 3},
			"pattern": map[string]any{"type": "string", "pattern": "^x"},
			"nested": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
				"properties": map[string]any{
					"n": map[string]any{"type": "integer", "minimum": 1, "maximum": 9},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	props := got["properties"].(map[string]any)
	if _, ok := props["format"]; !ok {
		t.Fatalf("property format was deleted: %#v", got)
	}
	if _, ok := props["pattern"]; !ok {
		t.Fatalf("property pattern was deleted: %#v", got)
	}
	format := props["format"].(map[string]any)
	if _, ok := format["format"]; ok || format["type"] != "string" {
		t.Fatalf("format schema=%#v", format)
	}
	if _, exists := got["additionalProperties"]; exists {
		t.Fatalf("additionalProperties survived: %#v", got)
	}
	if _, exists := got["required"]; exists {
		t.Fatalf("empty required survived: %#v", got)
	}
	nested := props["nested"].(map[string]any)
	if _, exists := nested["additionalProperties"]; exists {
		t.Fatalf("nested additionalProperties survived: %#v", nested)
	}
}

func TestSanitizeToolSchemaPreservesDefsRefEnumAndItems(t *testing.T) {
	got, err := SanitizeToolSchema(map[string]any{
		"type": "object",
		"$defs": map[string]any{
			"format": map[string]any{"type": "string", "enum": []any{"a", "b"}, "format": "uuid"},
		},
		"properties": map[string]any{
			"refd":  map[string]any{"$ref": "#/$defs/format"},
			"items": map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1}, "maxItems": 3},
		},
		"required": []any{"refd"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defs := got["$defs"].(map[string]any)
	named := defs["format"].(map[string]any)
	if named["type"] != "string" {
		t.Fatalf("defs format=%#v", named)
	}
	if _, ok := named["format"]; ok {
		t.Fatalf("defs format keyword survived: %#v", named)
	}
	props := got["properties"].(map[string]any)
	if props["refd"].(map[string]any)["$ref"] != "#/$defs/format" {
		t.Fatalf("ref lost: %#v", props["refd"])
	}
	items := props["items"].(map[string]any)
	if _, exists := items["maxItems"]; exists {
		t.Fatalf("maxItems survived: %#v", items)
	}
	if items["items"].(map[string]any)["type"] != "string" {
		t.Fatalf("array items=%#v", items["items"])
	}
	req := got["required"].([]any)
	if len(req) != 1 || req[0] != "refd" {
		t.Fatalf("required=%#v", req)
	}
}

func TestSanitizeToolSchemaMergesRootAllOf(t *testing.T) {
	got, err := SanitizeToolSchema(map[string]any{
		"type":        "object",
		"description": "note",
		"properties":  map[string]any{"path": map[string]any{"type": "string"}},
		"required":    []any{"path"},
		"allOf": []any{
			map[string]any{
				"type":       "object",
				"properties": map[string]any{"mode": map[string]any{"type": "string"}},
				"required":   []any{"mode"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := got["allOf"]; exists {
		t.Fatalf("allOf survived: %#v", got)
	}
	props := got["properties"].(map[string]any)
	if _, ok := props["path"]; !ok || props["mode"] == nil {
		t.Fatalf("merged properties=%#v", props)
	}
	req := got["required"].([]any)
	if len(req) != 2 {
		t.Fatalf("required=%#v", req)
	}
}

func TestSanitizeToolSchemaFailsClosedOnAmbiguousRootOneOf(t *testing.T) {
	_, err := SanitizeToolSchema(map[string]any{
		"oneOf": []any{
			map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}, "required": []any{"a"}},
			map[string]any{"type": "object", "properties": map[string]any{"b": map[string]any{"type": "number"}}, "required": []any{"b"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "oneOf") {
		t.Fatalf("err=%v", err)
	}
}

func TestSanitizeToolSchemaKeepsNestedOneOf(t *testing.T) {
	got, err := SanitizeToolSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"choice": map[string]any{
				"oneOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "number"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	choice := got["properties"].(map[string]any)["choice"].(map[string]any)
	if _, ok := choice["oneOf"]; !ok {
		t.Fatalf("nested oneOf dropped: %#v", choice)
	}
}

func TestSanitizeToolSchemaDropsNullRequired(t *testing.T) {
	got, err := SanitizeToolSchema(map[string]any{"type": "object", "required": nil, "properties": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := got["required"]; exists {
		t.Fatalf("null required survived: %#v", got)
	}
}

func TestCompileToolCatalogFailsClosedOnAmbiguousRootOneOf(t *testing.T) {
	_, err := compileToolCatalog([]protocol.Tool{{
		Name: "split",
		Parameters: map[string]any{
			"oneOf": []any{
				map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}, "required": []any{"a"}},
				map[string]any{"type": "object", "properties": map[string]any{"b": map[string]any{"type": "number"}}, "required": []any{"b"}},
			},
		},
	}}, nil)
	if err == nil || !strings.Contains(err.Error(), "oneOf") {
		t.Fatalf("err=%v", err)
	}
}

func TestCompileToolCatalogSanitizesProductionPayload(t *testing.T) {
	catalog, err := compileToolCatalog([]protocol.Tool{{
		Name: "memories__add",
		Parameters: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"format":  map[string]any{"type": "string", "format": "uri"},
				"pattern": map[string]any{"type": "string", "pattern": "^x"},
			},
			"required": []any{},
		},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	spec := catalog[0]["toolSpecification"].(map[string]any)
	schema := spec["inputSchema"].(map[string]any)["json"].(map[string]any)
	raw, _ := json.Marshal(schema)
	if strings.Contains(string(raw), `"additionalProperties"`) || strings.Contains(string(raw), `"required"`) {
		t.Fatalf("rejected keywords leaked: %s", raw)
	}
	props := schema["properties"].(map[string]any)
	if _, ok := props["format"]; !ok || props["pattern"] == nil {
		t.Fatalf("colliding property names lost: %s", raw)
	}
	if _, ok := props["format"].(map[string]any)["format"]; ok {
		t.Fatalf("schema keyword format survived under property: %s", raw)
	}
}
