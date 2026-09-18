package kiro

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
)

const (
	completionToolName = "codex_kiro_final_answer"
	maxToolNameLen     = 64
	toolNamePrefix     = 55
)

type ToolNameRegistry struct {
	used    map[string]struct{}
	aliases map[string]string
	restore map[string]string
}

func NewToolNameRegistry() *ToolNameRegistry {
	return &ToolNameRegistry{
		used:    map[string]struct{}{completionToolName: {}},
		aliases: map[string]string{},
		restore: map[string]string{},
	}
}

func NamespacedToolName(namespace, name string) string {
	name = strings.TrimSpace(name)
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return name
	}
	return namespace + "__" + name
}

func (r *ToolNameRegistry) Alias(wire string) (string, error) {
	if r == nil {
		r = NewToolNameRegistry()
	}
	if wire == completionToolName {
		return "", fmt.Errorf("Kiro reserves the tool name %q", completionToolName)
	}
	if existing, ok := r.aliases[wire]; ok {
		return existing, nil
	}
	alias := kiroToolName(wire, r.used)
	r.aliases[wire] = alias
	if alias != wire {
		r.restore[alias] = wire
	}
	return alias, nil
}

func (r *ToolNameRegistry) Restore(name string) string {
	if r == nil {
		return name
	}
	if wire, ok := r.restore[name]; ok {
		return wire
	}
	return name
}

func kiroToolName(wire string, used map[string]struct{}) string {
	cleaned := sanitizeToolName(wire)
	if cleaned == wire && cleaned != "" && len(cleaned) <= maxToolNameLen {
		if _, exists := used[cleaned]; !exists {
			used[cleaned] = struct{}{}
			return cleaned
		}
	}
	base := cleaned
	if len(base) > toolNamePrefix {
		base = base[:toolNamePrefix]
	}
	if base == "" {
		base = "tool"
	}
	for salt := 0; ; salt++ {
		input := wire
		if salt > 0 {
			input = fmt.Sprintf("%s#%d", wire, salt)
		}
		sum := sha256.Sum256([]byte(input))
		candidate := base + "_" + hex.EncodeToString(sum[:])[:8]
		if _, exists := used[candidate]; exists {
			continue
		}
		used[candidate] = struct{}{}
		return candidate
	}
}

func sanitizeToolName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return b.String()
}

func normalizeToolID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	out := b.String()
	if len(out) > 64 {
		return out[:64]
	}
	return out
}
