package kiro

import (
	"fmt"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/tools"
)

func compileToolCatalog(declared []protocol.Tool, names *ToolNameRegistry) ([]map[string]any, error) {
	if len(declared) == 0 {
		return nil, nil
	}
	if names == nil {
		names = NewToolNameRegistry()
	}
	out := make([]map[string]any, 0, len(declared))
	for _, tool := range declared {
		alias, err := names.Alias(NamespacedToolName(tool.Namespace, tool.Name))
		if err != nil {
			return nil, err
		}
		schema, err := SanitizeToolSchema(tool.Parameters)
		if err != nil {
			return nil, fmt.Errorf("Kiro tool %q schema: %w", alias, err)
		}
		spec := map[string]any{
			"name":        alias,
			"inputSchema": map[string]any{"json": schema},
		}
		if tool.Description != "" {
			spec["description"] = tool.Description
		} else {
			spec["description"] = alias
		}
		out = append(out, map[string]any{"toolSpecification": spec})
	}
	return out, nil
}

func attachToolCatalog(payload map[string]any, catalog []map[string]any) error {
	if len(catalog) == 0 {
		return nil
	}
	state, _ := payload["conversationState"].(map[string]any)
	current, _ := state["currentMessage"].(map[string]any)
	uim, _ := current["userInputMessage"].(map[string]any)
	if uim == nil {
		return fmt.Errorf("Kiro generate payload is missing the current user message")
	}
	ctx, _ := uim["userInputMessageContext"].(map[string]any)
	if ctx == nil {
		ctx = map[string]any{}
		uim["userInputMessageContext"] = ctx
	}
	ctx["tools"] = catalog
	return nil
}

func materializeRequestTools(parsed protocol.ParsedRequest) ([]protocol.Tool, error) {
	catalog, err := tools.Build(parsed.Context.Tools)
	if err != nil {
		return nil, err
	}
	plan, err := catalog.Materialize(tools.MaterializeOptions{
		Choice:   parsed.Options.ToolChoice,
		Messages: parsed.Context.Messages,
	})
	if err != nil {
		return nil, err
	}
	return plan.Tools, nil
}
