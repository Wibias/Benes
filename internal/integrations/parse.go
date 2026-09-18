package integrations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/export"
	toml "github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

func parseDocument(format export.Format, text string) (any, error) {
	if strings.TrimSpace(text) == "" {
		return map[string]any{}, nil
	}
	switch format {
	case export.FormatJSON, export.FormatJSON5:
		cleaned := stripJSON5(text)
		var doc any
		dec := json.NewDecoder(strings.NewReader(cleaned))
		dec.UseNumber()
		if err := dec.Decode(&doc); err != nil {
			return nil, err
		}
		return normalizeParsed(doc), nil
	case export.FormatYAML:
		var doc any
		if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
			return nil, err
		}
		if doc == nil {
			return map[string]any{}, nil
		}
		return normalizeParsed(doc), nil
	case export.FormatTOML:
		var doc map[string]any
		if err := toml.Unmarshal([]byte(text), &doc); err != nil {
			return nil, err
		}
		if doc == nil {
			return map[string]any{}, nil
		}
		return normalizeParsed(doc), nil
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}

func stripJSON5(text string) string {
	var buf bytes.Buffer
	inString := false
	escaped := false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if inString {
			buf.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			buf.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(text) && text[i+1] == '/' {
			for i < len(text) && text[i] != '\n' {
				i++
			}
			if i < len(text) {
				buf.WriteByte('\n')
			}
			continue
		}
		if ch == '/' && i+1 < len(text) && text[i+1] == '*' {
			i += 2
			for i+1 < len(text) && !(text[i] == '*' && text[i+1] == '/') {
				i++
			}
			i++
			continue
		}
		buf.WriteByte(ch)
	}
	return buf.String()
}

func normalizeParsed(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = normalizeParsed(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[fmt.Sprint(key)] = normalizeParsed(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalizeParsed(item)
		}
		return out
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return float64(i)
		}
		f, _ := v.Float64()
		return f
	default:
		return v
	}
}
