package websearch

import "github.com/Wibias/Benes/internal/protocol"

const ToolName = "web_search"

func ReplaceHostedTools(tools []protocol.Tool) []protocol.Tool {
	if len(tools) == 0 {
		return tools
	}
	out := make([]protocol.Tool, 0, len(tools))
	hosted := false
	seenKinds := make(map[protocol.HostedWebSearchKind]struct{}, 2)
	kinds := make([]protocol.HostedWebSearchKind, 0, 2)
	for _, tool := range tools {
		if tool.HostedWebSearch {
			hosted = true
			kind := protocol.HostedWebSearchKind(tool.Name)
			switch kind {
			case protocol.HostedWebSearchWebSearch, protocol.HostedWebSearchPreview:
				if _, seen := seenKinds[kind]; !seen {
					seenKinds[kind] = struct{}{}
					kinds = append(kinds, kind)
				}
			}
			continue
		}
		out = append(out, tool)
	}
	if !hosted {
		return tools
	}
	synthetic := SyntheticTool()
	synthetic.SidecarWebSearchKinds = kinds
	return append(out, synthetic)
}

func SyntheticTool() protocol.Tool {
	return protocol.Tool{
		Name:             ToolName,
		SidecarWebSearch: true,
		Description:      "Search the web for current, real-world, or post-training-cutoff information. Returns a concise answer synthesized from live results, with sources. Use it whenever the user asks about recent events, versions, prices, docs, or anything you are unsure is current.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "A single search query — a focused natural-language question or keywords.",
				},
				"queries": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional: run several related queries together in one call. Use instead of query to batch independent searches.",
				},
			},
		},
	}
}
