package tools

import (
	"encoding/json"
	"fmt"

	"github.com/Wibias/Benes/internal/protocol"
)

const DefaultMaxCatalogBytes = 32 * 1024

type Strategy string

const (
	StrategyFull    Strategy = "full"
	StrategySearch  Strategy = "search"
	StrategyBounded Strategy = "bounded"
)

type MaterializeOptions struct {
	MaxBytes int
	Choice   *protocol.ToolChoice
	Messages []protocol.Message
}

type Plan struct {
	Strategy     Strategy
	Tools        []protocol.Tool
	Bytes        int
	TopLevel     int
	Deferred     int
	Materialized int
}

func (c *Catalog) Materialize(opts MaterializeOptions) (Plan, error) {
	if c == nil {
		return Plan{Strategy: StrategyFull}, nil
	}
	entries := c.Entries()
	if len(entries) == 0 {
		return Plan{Strategy: StrategyFull}, nil
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxCatalogBytes
	}

	required := map[string]struct{}{}
	if opts.Choice != nil {
		switch opts.Choice.Kind {
		case protocol.ToolChoiceNamed:
			if wire, err := c.ResolveChoice(opts.Choice.Name); err == nil {
				required[wire] = struct{}{}
			}
		case protocol.ToolChoiceAllowed:
			for _, name := range opts.Choice.AllowedTools {
				if wire, err := c.ResolveChoice(name); err == nil {
					required[wire] = struct{}{}
				}
			}
		}
	}
	for _, message := range opts.Messages {
		for _, part := range message.Content {
			if part.Type != protocol.ContentToolCall {
				continue
			}
			if wire, err := c.WireForCall(part); err == nil {
				required[wire] = struct{}{}
			}
		}
		if message.Role == protocol.RoleToolResult && message.ToolName != "" {
			if wire, err := c.ResolveChoice(WireName(protocol.Tool{Namespace: message.ToolNamespace, Name: message.ToolName})); err == nil {
				required[wire] = struct{}{}
			}
		}
	}
	for _, entry := range entries {
		if entry.CodeMode {
			required[entry.WireName] = struct{}{}
		}
	}

	hasSearch := false
	deferred := 0
	for _, entry := range entries {
		if entry.Tool.ToolSearch {
			hasSearch = true
		}
		if entry.Tool.LoadedFromToolSearch {
			deferred++
		}
	}

	selected := make([]Entry, 0, len(entries))
	strategy := StrategyFull
	if hasSearch {
		strategy = StrategySearch
		for _, entry := range entries {
			_, need := required[entry.WireName]
			if entry.Tool.ToolSearch || need || !entry.Tool.LoadedFromToolSearch {
				selected = append(selected, entry)
			}
		}
	} else {
		selected = append(selected, entries...)
	}

	fullBytes := catalogBytes(selected)
	if fullBytes > maxBytes {
		strategy = StrategyBounded
		selected, fullBytes = boundEntries(selected, required, maxBytes)
		if fullBytes < 0 {
			return Plan{}, fmt.Errorf("required tool catalog exceeds %d bytes", maxBytes)
		}
	}

	tools := make([]protocol.Tool, 0, len(selected))
	for _, entry := range selected {
		tools = append(tools, entry.Tool)
	}
	return Plan{
		Strategy:     strategy,
		Tools:        tools,
		Bytes:        fullBytes,
		TopLevel:     len(entries) - deferred,
		Deferred:     deferred,
		Materialized: len(selected),
	}, nil
}

func boundEntries(entries []Entry, required map[string]struct{}, maxBytes int) ([]Entry, int) {
	must := make([]Entry, 0, len(required))
	rest := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if _, ok := required[entry.WireName]; ok {
			must = append(must, entry)
		} else {
			rest = append(rest, entry)
		}
	}
	out := append([]Entry(nil), must...)
	n := catalogBytes(out)
	if n > maxBytes {
		return nil, -1
	}
	for _, entry := range rest {
		next := catalogBytes([]Entry{entry})
		if n+next > maxBytes {
			continue
		}
		out = append(out, entry)
		n += next
	}
	return out, n
}

func catalogBytes(entries []Entry) int {
	n := 0
	for _, entry := range entries {
		raw, err := json.Marshal(entry.Tool.Parameters)
		if err != nil {
			n += 64
			continue
		}
		n += len(raw) + len(entry.WireName) + len(entry.Tool.Description)
	}
	return n
}
