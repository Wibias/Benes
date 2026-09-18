package tools

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

var (
	ErrCollision  = errors.New("tool wire-name collision")
	ErrAmbiguous  = errors.New("ambiguous tool choice")
	ErrUndeclared = errors.New("undeclared tool")
)

type Entry struct {
	Tool     protocol.Tool
	WireName string
	CodeMode bool
}

type Catalog struct {
	entries   []Entry
	byWire    map[string]int
	byLogical map[string][]int
}

func Build(declared []protocol.Tool) (*Catalog, error) {
	catalog := &Catalog{
		entries:   make([]Entry, 0, len(declared)),
		byWire:    make(map[string]int, len(declared)),
		byLogical: make(map[string][]int, len(declared)),
	}
	shell := visibleBareShell(declared)
	for _, tool := range declared {
		wire := WireName(tool)
		if wire == "" {
			continue
		}
		if _, exists := catalog.byWire[wire]; exists {
			return nil, fmt.Errorf("%w: %s", ErrCollision, wire)
		}
		entry := Entry{
			Tool:     tool,
			WireName: wire,
			CodeMode: isCodeMode(tool, shell),
		}
		idx := len(catalog.entries)
		catalog.entries = append(catalog.entries, entry)
		catalog.byWire[wire] = idx
		catalog.byLogical[tool.Name] = append(catalog.byLogical[tool.Name], idx)
	}
	return catalog, nil
}

func (c *Catalog) Entries() []Entry {
	if c == nil {
		return nil
	}
	return append([]Entry(nil), c.entries...)
}

func (c *Catalog) ResolveChoice(name string) (string, error) {
	if c == nil {
		return "", ErrUndeclared
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrUndeclared
	}
	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		alias := WireName(protocol.Tool{Namespace: parts[0], Name: parts[1]})
		if idx, ok := c.byWire[alias]; ok {
			return c.entries[idx].WireName, nil
		}
		return "", fmt.Errorf("%w: %s", ErrUndeclared, name)
	}
	if isHostedWebSearchChoice(name) {
		if wire := c.sidecarWebSearchWire(protocol.HostedWebSearchKind(name)); wire != "" {
			return wire, nil
		}
	}
	if matches := c.byLogical[name]; len(matches) == 1 {
		return c.entries[matches[0]].WireName, nil
	} else if len(matches) > 1 {
		return "", fmt.Errorf("%w: %s", ErrAmbiguous, name)
	}
	if idx, ok := c.byWire[name]; ok {
		return c.entries[idx].WireName, nil
	}
	if wire := c.codeModeExecWire(); wire != "" && isCodeModeHelper(name) {
		return wire, nil
	}
	return "", fmt.Errorf("%w: %s", ErrUndeclared, name)
}

func (c *Catalog) WireForCall(part protocol.ContentPart) (string, error) {
	if strings.TrimSpace(part.CustomWireName) != "" {
		if c == nil {
			return "", ErrUndeclared
		}
		if _, ok := c.byWire[part.CustomWireName]; !ok {
			return "", fmt.Errorf("%w: %s", ErrUndeclared, part.CustomWireName)
		}
		return part.CustomWireName, nil
	}
	return c.ResolveChoice(WireName(protocol.Tool{Namespace: part.ToolNamespace, Name: part.ToolName}))
}

func WireName(tool protocol.Tool) string {
	name := sanitize(tool.Name)
	if name == "" {
		return ""
	}
	if ns := sanitize(tool.Namespace); ns != "" {
		return ns + "__" + name
	}
	return name
}

func isCodeMode(tool protocol.Tool, visibleBareShell bool) bool {
	if !tool.Freeform || tool.Namespace != "" || tool.Name != "exec" {
		return false
	}
	return !visibleBareShell
}

func (c *Catalog) sidecarWebSearchWire(kind protocol.HostedWebSearchKind) string {
	if c == nil {
		return ""
	}
	for _, entry := range c.entries {
		if !entry.Tool.SidecarWebSearch {
			continue
		}
		for _, declared := range entry.Tool.SidecarWebSearchKinds {
			if declared == kind {
				return entry.WireName
			}
		}
	}
	return ""
}

func isHostedWebSearchChoice(name string) bool {
	return name == string(protocol.HostedWebSearchWebSearch) || name == string(protocol.HostedWebSearchPreview)
}

func (c *Catalog) codeModeExecWire() string {
	if c == nil {
		return ""
	}
	for _, entry := range c.entries {
		if entry.CodeMode {
			return entry.WireName
		}
	}
	return ""
}

func isCodeModeHelper(name string) bool {
	switch sanitize(name) {
	case "shell", "bash", "command", "exec_command", "shell_command", "apply_patch", "applyPatch":
		return true
	default:
		return false
	}
}

func visibleBareShell(tools []protocol.Tool) bool {
	for _, tool := range tools {
		if tool.Namespace != "" {
			continue
		}
		switch tool.Name {
		case "exec_command", "shell_command", "shell":
			return true
		}
	}
	return false
}

func sanitize(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
