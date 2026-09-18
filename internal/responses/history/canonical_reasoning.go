package history

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

func canonicalReasoningSignature(raw json.RawMessage) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", fmt.Errorf("reasoning item: %w", err)
	}
	typeRaw, ok := fields["type"]
	if !ok {
		return "", fmt.Errorf("reasoning item requires type")
	}
	typ, err := decodeJSONString(typeRaw)
	if err != nil || typ != "reasoning" {
		return "", fmt.Errorf("reasoning item type must be reasoning")
	}
	var b strings.Builder
	// Zod returns the known object fields in schema order and strips unknown fields.
	// Build that exact shape before applying JSON.stringify-compatible string escaping.
	b.WriteString(`{"type":"reasoning"`)
	if v, ok := fields["id"]; ok {
		c, err := canonicalJSONString(v)
		if err != nil {
			return "", fmt.Errorf("reasoning id: %w", err)
		}
		b.WriteString(`,"id":`)
		b.WriteString(c)
	}
	if v, ok := fields["summary"]; ok {
		c, err := canonicalReasoningParts(v, "summary_text")
		if err != nil {
			return "", fmt.Errorf("reasoning summary: %w", err)
		}
		b.WriteString(`,"summary":`)
		b.WriteString(c)
	}
	if v, ok := fields["content"]; ok {
		c, err := canonicalReasoningParts(v, "reasoning_text")
		if err != nil {
			return "", fmt.Errorf("reasoning content: %w", err)
		}
		b.WriteString(`,"content":`)
		b.WriteString(c)
	}
	if v, ok := fields["encrypted_content"]; ok {
		c, err := canonicalJSONString(v)
		if err != nil {
			return "", fmt.Errorf("reasoning encrypted_content: %w", err)
		}
		b.WriteString(`,"encrypted_content":`)
		b.WriteString(c)
	}
	b.WriteByte('}')
	return b.String(), nil
}

func canonicalReasoningParts(raw json.RawMessage, wantType string) (string, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", fmt.Errorf("must be an array")
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", fmt.Errorf("must be an array")
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, p := range parts {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(p, &fields); err != nil {
			return "", fmt.Errorf("item %d must be object", i)
		}
		tr, ok := fields["type"]
		if !ok {
			return "", fmt.Errorf("item %d requires type", i)
		}
		typ, err := decodeJSONString(tr)
		if err != nil || typ != wantType {
			return "", fmt.Errorf("item %d type must be %s", i, wantType)
		}
		tx, ok := fields["text"]
		if !ok {
			return "", fmt.Errorf("item %d requires text", i)
		}
		text, err := canonicalJSONString(tx)
		if err != nil {
			return "", fmt.Errorf("item %d text: %w", i, err)
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"type":"`)
		b.WriteString(wantType)
		b.WriteString(`","text":`)
		b.WriteString(text)
		b.WriteByte('}')
	}
	b.WriteByte(']')
	return b.String(), nil
}

func decodeJSONString(raw json.RawMessage) (string, error) {
	c, err := canonicalJSONString(raw)
	if err != nil {
		return "", err
	}
	var s string
	if err := json.Unmarshal([]byte(c), &s); err != nil {
		return "", err
	}
	return s, nil
}

func canonicalJSONString(raw json.RawMessage) (string, error) {
	s := bytes.TrimSpace(raw)
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("must be string")
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 1; i < len(s)-1; {
		c := s[i]
		if c == '\\' {
			if i+1 >= len(s)-1 {
				return "", fmt.Errorf("invalid escape")
			}
			e := s[i+1]
			i += 2
			switch e {
			case '"':
				b.WriteString(`\"`)
			case '\\':
				b.WriteString(`\\`)
			case '/':
				b.WriteByte('/')
			case 'b':
				b.WriteString(`\b`)
			case 'f':
				b.WriteString(`\f`)
			case 'n':
				b.WriteString(`\n`)
			case 'r':
				b.WriteString(`\r`)
			case 't':
				b.WriteString(`\t`)
			case 'u':
				if i+4 > len(s)-1 {
					return "", fmt.Errorf("short unicode escape")
				}
				u, ok := hex4(s[i : i+4])
				if !ok {
					return "", fmt.Errorf("invalid unicode escape")
				}
				i += 4
				if u >= 0xD800 && u <= 0xDBFF {
					if i+6 <= len(s)-1 && s[i] == '\\' && s[i+1] == 'u' {
						lo, ok := hex4(s[i+2 : i+6])
						if ok && lo >= 0xDC00 && lo <= 0xDFFF {
							i += 6
							r := rune(0x10000 + (int(u)-0xD800)*0x400 + (int(lo) - 0xDC00))
							appendJSRune(&b, r)
							continue
						}
					}
					// Well-formed JSON.stringify preserves lone UTF-16 surrogates as escapes.
					fmt.Fprintf(&b, "\\u%04x", u)
				} else if u >= 0xDC00 && u <= 0xDFFF {
					fmt.Fprintf(&b, "\\u%04x", u)
				} else {
					appendJSRune(&b, rune(u))
				}
			default:
				return "", fmt.Errorf("invalid escape")
			}
			continue
		}
		if c < 0x20 {
			return "", fmt.Errorf("unescaped control character")
		}
		r, n := utf8.DecodeRune(s[i : len(s)-1])
		if r == utf8.RuneError && n == 1 {
			return "", fmt.Errorf("invalid UTF-8")
		}
		appendJSRune(&b, r)
		i += n
	}
	b.WriteByte('"')
	return b.String(), nil
}

func appendJSRune(b *strings.Builder, r rune) {
	switch r {
	case '"':
		b.WriteString(`\"`)
	case '\\':
		b.WriteString(`\\`)
	case '\b':
		b.WriteString(`\b`)
	case '\f':
		b.WriteString(`\f`)
	case '\n':
		b.WriteString(`\n`)
	case '\r':
		b.WriteString(`\r`)
	case '\t':
		b.WriteString(`\t`)
	default:
		if r < 0x20 {
			fmt.Fprintf(b, "\\u%04x", r)
		} else {
			b.WriteRune(r)
		}
	}
}

func hex4(s []byte) (uint16, bool) {
	if len(s) != 4 {
		return 0, false
	}
	var v uint16
	for _, c := range s {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v += uint16(c - '0')
		case c >= 'a' && c <= 'f':
			v += uint16(c - 'a' + 10)
		case c >= 'A' && c <= 'F':
			v += uint16(c - 'A' + 10)
		default:
			return 0, false
		}
	}
	return v, true
}
