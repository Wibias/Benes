package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func serialize(format Format, doc any) (string, error) {
	switch format {
	case FormatJSON, FormatJSON5:
		raw, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return "", err
		}
		return string(raw) + "\n", nil
	case FormatYAML:
		var buf strings.Builder
		if err := writeYAML(&buf, doc, 0); err != nil {
			return "", err
		}
		text := buf.String()
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		return text, nil
	case FormatTOML:
		return writeTOML(doc)
	default:
		return "", fmt.Errorf("unsupported export format %q", format)
	}
}

func writeYAML(buf *strings.Builder, value any, indent int) error {
	pad := strings.Repeat("  ", indent)
	switch v := value.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case int:
		buf.WriteString(strconv.Itoa(v))
	case int64:
		buf.WriteString(strconv.FormatInt(v, 10))
	case float64:
		if v == float64(int64(v)) {
			buf.WriteString(strconv.FormatInt(int64(v), 10))
		} else {
			buf.WriteString(strconv.FormatFloat(v, 'f', -1, 64))
		}
	case string:
		buf.WriteString(yamlString(v))
	case []any:
		if len(v) == 0 {
			buf.WriteString("[]")
			return nil
		}
		for i, item := range v {
			if i > 0 {
				buf.WriteByte('\n')
				buf.WriteString(pad)
			}
			buf.WriteString("- ")
			if isYAMLMap(item) || isYAMLList(item) {
				buf.WriteByte('\n')
				buf.WriteString(strings.Repeat("  ", indent+1))
				if err := writeYAML(buf, item, indent+1); err != nil {
					return err
				}
			} else if err := writeYAML(buf, item, indent+1); err != nil {
				return err
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte('\n')
				buf.WriteString(pad)
			}
			buf.WriteString(yamlKey(key))
			buf.WriteString(": ")
			child := v[key]
			if isYAMLMap(child) || isYAMLList(child) {
				if childMap, ok := child.(map[string]any); ok && len(childMap) == 0 {
					buf.WriteString("{}")
					continue
				}
				if childList, ok := child.([]any); ok && len(childList) == 0 {
					buf.WriteString("[]")
					continue
				}
				buf.WriteByte('\n')
				buf.WriteString(strings.Repeat("  ", indent+1))
				if err := writeYAML(buf, child, indent+1); err != nil {
					return err
				}
			} else if err := writeYAML(buf, child, 0); err != nil {
				return err
			}
		}
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var generic any
		if json.Unmarshal(raw, &generic) != nil {
			return fmt.Errorf("unserializable yaml value %T", v)
		}
		return writeYAML(buf, generic, indent)
	}
	return nil
}

func isYAMLMap(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

func isYAMLList(v any) bool {
	_, ok := v.([]any)
	return ok
}

func yamlKey(key string) string {
	if key == "" || strings.ContainsAny(key, ":#{}[]&*!|>'\"%@`,") {
		return strconv.Quote(key)
	}
	return key
}

func yamlString(value string) string {
	if value == "" || strings.ContainsAny(value, ":#{}[]&*!|>'\"%@`\n") || value == "true" || value == "false" || value == "null" {
		return strconv.Quote(value)
	}
	return value
}

func writeTOML(doc any) (string, error) {
	root, ok := doc.(map[string]any)
	if !ok {
		raw, err := json.Marshal(doc)
		if err != nil {
			return "", err
		}
		if json.Unmarshal(raw, &root) != nil {
			return "", fmt.Errorf("toml document must be an object")
		}
	}
	var buf bytes.Buffer
	if providers, ok := root["providers"].(map[string]any); ok {
		keys := sortedKeys(providers)
		for _, key := range keys {
			block, _ := providers[key].(map[string]any)
			fmt.Fprintf(&buf, "[providers.%s]\n", tomlIdent(key))
			writeTOMLFields(&buf, block)
			buf.WriteByte('\n')
		}
	}
	if models, ok := root["models"].(map[string]any); ok {
		keys := sortedKeys(models)
		for _, key := range keys {
			block, _ := models[key].(map[string]any)
			fmt.Fprintf(&buf, "[models.%s]\n", tomlQuotedKey(key))
			writeTOMLFields(&buf, block)
			buf.WriteByte('\n')
		}
	}
	return buf.String(), nil
}

func writeTOMLFields(buf *bytes.Buffer, fields map[string]any) {
	keys := sortedKeys(fields)
	for _, key := range keys {
		fmt.Fprintf(buf, "%s = %s\n", tomlIdent(key), tomlValue(fields[key]))
	}
}

func tomlIdent(key string) string {
	return key
}

func tomlQuotedKey(key string) string {
	return strconv.Quote(key)
}

func tomlValue(value any) string {
	switch v := value.(type) {
	case string:
		return strconv.Quote(v)
	case int:
		return strconv.Itoa(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}

func sortedKeys(in map[string]any) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
