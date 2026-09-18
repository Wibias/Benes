package history

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

func buildToolCatalog(declared, loaded []json.RawMessage) []protocol.Tool {
	declaredTools := buildTools(declared)
	loadedTools := buildTools(loaded)
	loadedNames := make(map[string]struct{}, len(loadedTools))
	for _, tool := range loadedTools {
		loadedNames[namespacedToolName(tool.Namespace, tool.Name)] = struct{}{}
	}
	seen := make(map[string]struct{}, len(declaredTools)+len(loadedTools))
	merged := make([]protocol.Tool, 0, len(declaredTools)+len(loadedTools))
	for _, tool := range append(declaredTools, loadedTools...) {
		key := namespacedToolName(tool.Namespace, tool.Name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if _, ok := loadedNames[key]; ok {
			tool.LoadedFromToolSearch = true
		}
		merged = append(merged, tool)
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func buildTools(specs []json.RawMessage) []protocol.Tool {
	out := make([]protocol.Tool, 0, len(specs))
	for _, raw := range specs {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			continue
		}
		typ, ok := optionalString(fields["type"])
		if !ok {
			continue
		}
		switch typ {
		case "function":
			if tool, ok := functionTool(fields, ""); ok {
				out = append(out, tool)
			}
		case "namespace":
			namespace, _ := optionalString(fields["name"])
			var inner []json.RawMessage
			if json.Unmarshal(fields["tools"], &inner) != nil {
				continue
			}
			// Codex 0.147 reserves the literal `functions` group for ordinary
			// top-level client tools. Its children must therefore lose the group
			// name instead of being rewritten into MCP-style `functions__*` tools.
			builtinFunctions := namespace == "functions"
			childNamespace := namespace
			if builtinFunctions {
				childNamespace = ""
			}
			for _, rawInner := range inner {
				var in map[string]json.RawMessage
				if json.Unmarshal(rawInner, &in) != nil {
					continue
				}
				innerType, _ := optionalString(in["type"])
				switch innerType {
				case "function":
					if tool, ok := functionTool(in, childNamespace); ok {
						out = append(out, tool)
					}
				case "custom":
					// Reserved `functions` children stay top-level. Other
					// namespace groups keep the logical namespace so later
					// compilers can flatten to a collision-safe wire alias.
					ns := childNamespace
					if builtinFunctions {
						ns = ""
					}
					if tool, ok := customTool(in, ns); ok {
						out = append(out, tool)
					}
				}
			}
		case "custom":
			if tool, ok := customTool(fields, ""); ok {
				out = append(out, tool)
			}
		case "web_search", "web_search_preview":
			name, ok := optionalString(fields["name"])
			if !ok || name == "" {
				name = typ
			}
			out = append(out, protocol.Tool{Name: name, HostedWebSearch: true})
		case "tool_search":
			description, ok := optionalString(fields["description"])
			if !ok {
				description = "Search for additional tools to load for the next turn."
			}
			params, ok := objectValue(fields["parameters"])
			if !ok {
				params = map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string", "description": "Search query for tools to load."},
						"limit": map[string]any{"type": "number", "description": "Maximum number of tools to return."},
					},
					"required": []any{"query"},
				}
			}
			out = append(out, protocol.Tool{Name: "tool_search", Description: description, Parameters: params, ToolSearch: true})
		default:
			_, ok := nonEmptyString(fields["name"])
			if !ok || typ == "web_search" || typ == "image_generation" {
				continue
			}
			if tool, ok := functionTool(fields, ""); ok {
				out = append(out, tool)
			}
		}
	}
	return out
}

func functionTool(fields map[string]json.RawMessage, namespace string) (protocol.Tool, bool) {
	name, ok := nonEmptyString(fields["name"])
	if !ok {
		return protocol.Tool{}, false
	}
	description, _ := optionalString(fields["description"])
	params := normalizeParameters(fields["parameters"])
	tool := protocol.Tool{Name: name, Description: description, Parameters: params, Namespace: namespace}
	if raw := bytes.TrimSpace(fields["strict"]); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		var strict bool
		if json.Unmarshal(raw, &strict) == nil {
			tool.Strict = &strict
		}
	}
	return tool, true
}

func customTool(fields map[string]json.RawMessage, namespace string) (protocol.Tool, bool) {
	name, ok := nonEmptyString(fields["name"])
	if !ok {
		return protocol.Tool{}, false
	}
	description, _ := optionalString(fields["description"])
	inputDescription := "Raw freeform input for this tool."
	if name == "apply_patch" {
		inputDescription = "Raw tool input. For apply_patch, begin exactly with `*** Begin Patch` (no trailing `***`), then use its standard patch envelope."
	}
	return protocol.Tool{
		Name: name, Description: description, Freeform: true, Namespace: namespace,
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"input": map[string]any{"type": "string", "description": inputDescription}},
			"required":   []any{"input"},
		},
	}, true
}

func normalizeParameters(raw json.RawMessage) map[string]any {
	params, ok := objectValue(raw)
	if !ok {
		return map[string]any{"type": "object"}
	}
	if typ, ok := params["type"].(string); ok && typ == "object" {
		return params
	}
	params["type"] = "object"
	return params
}

func objectValue(raw json.RawMessage) (map[string]any, bool) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, false
	}
	obj, ok := value.(map[string]any)
	return obj, ok
}

func namespacedToolName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "__" + name
}

func toolSpecsFromItem(raw json.RawMessage) []json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	var specs []json.RawMessage
	if json.Unmarshal(fields["tools"], &specs) != nil {
		return nil
	}
	return specs
}

func decodeToolSearchCall(raw json.RawMessage) protocol.ContentPart {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	callID, _ := optionalString(fields["call_id"])
	if callID == "" {
		callID, _ = optionalString(fields["id"])
	}
	args, ok := objectValue(fields["arguments"])
	if !ok {
		args = map[string]any{}
	}
	return protocol.ContentPart{Type: protocol.ContentToolCall, ToolCallID: callID, ToolName: "tool_search", Arguments: args}
}

type toolSearchOutput struct {
	callID string
	status string
	specs  []json.RawMessage
}

func decodeToolSearchOutput(raw json.RawMessage) toolSearchOutput {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	callID, _ := optionalString(fields["call_id"])
	status, _ := optionalString(fields["status"])
	var specs []json.RawMessage
	_ = json.Unmarshal(fields["tools"], &specs)
	return toolSearchOutput{callID: callID, status: status, specs: specs}
}

func toolSearchWireNames(specs []json.RawMessage) []string {
	var names []string
	for _, raw := range specs {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			continue
		}
		typ, _ := optionalString(fields["type"])
		if typ == "namespace" {
			namespace, _ := optionalString(fields["name"])
			flatten := namespace == "functions"
			var inner []json.RawMessage
			if json.Unmarshal(fields["tools"], &inner) != nil {
				continue
			}
			for _, rawInner := range inner {
				var in map[string]json.RawMessage
				if json.Unmarshal(rawInner, &in) != nil {
					continue
				}
				if name, ok := optionalString(in["name"]); ok {
					if flatten {
						names = append(names, name)
					} else {
						names = append(names, namespacedToolName(namespace, name))
					}
				}
			}
			continue
		}
		if name, ok := optionalString(fields["name"]); ok {
			names = append(names, name)
		}
	}
	return names
}

func toolSearchResultMessage(out toolSearchOutput, now int64) protocol.Message {
	names := toolSearchWireNames(out.specs)
	failed := out.status != "" && out.status != "completed" && out.status != "success"
	var text string
	isError := false
	switch {
	case failed && len(names) == 0:
		text = fmt.Sprintf("Tool search failed (status: %s).", out.status)
		isError = true
	case len(names) > 0:
		text = "Tool search loaded these tools — they are now in your available tools. Call one by its EXACT name: " + strings.Join(names, ", ") + "."
	default:
		text = "Tool search returned no tools."
	}
	return protocol.Message{Role: protocol.RoleToolResult, ToolCallID: out.callID, ToolName: "tool_search", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: text}}, IsError: isError, Timestamp: now}
}
