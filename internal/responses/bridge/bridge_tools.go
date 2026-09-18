package bridge

import (
	"encoding/json"
	"fmt"
)

func (b *Bridge) closeToolCompleted() ([]Frame, error) {
	if b.tool == nil {
		return nil, nil
	}
	tool := b.tool
	arguments := tool.arguments
	if arguments == "" {
		arguments = "{}"
	}
	if !validArguments(arguments) {
		return b.failTool(), fmt.Errorf("malformed function call arguments")
	}
	schemaName := tool.name
	if tool.namespace != "" {
		schemaName = tool.namespace + "__" + tool.name
	}
	if schema := b.toolSchemas[schemaName]; schema != nil {
		arguments = repairToolArguments(arguments, schema)
	}
	item := map[string]any{
		"type": "function_call", "id": tool.id, "call_id": tool.callID,
		"name": tool.name, "arguments": arguments, "status": "completed",
	}
	if tool.namespace != "" {
		item["namespace"] = tool.namespace
	}
	if len(tool.providerMetadata) > 0 {
		// Canonical Responses opaque state: extra_content.google.thought_signature.
		// Do not invent a parallel public provider_metadata field on bridge output.
		if extra := bridgeExtraContentFromProviderMetadata(tool.providerMetadata); len(extra) > 0 {
			item["extra_content"] = json.RawMessage(extra)
		}
	}
	frames := []Frame{
		b.emit("response.function_call_arguments.done", map[string]any{
			"item_id": tool.id, "output_index": tool.outputIndex, "arguments": arguments,
		}),
		b.emit("response.output_item.done", map[string]any{"output_index": tool.outputIndex, "item": item}),
	}
	b.output = append(b.output, item)
	b.outputIndex++
	b.tool = nil
	return frames, nil
}

func (b *Bridge) failTool() []Frame {
	if b.tool == nil {
		return nil
	}
	tool := b.tool
	arguments := tool.arguments
	if arguments == "" {
		arguments = "{}"
	}
	item := map[string]any{
		"type": "function_call", "id": tool.id, "call_id": tool.callID,
		"name": tool.name, "arguments": arguments, "status": "incomplete",
	}
	if tool.namespace != "" {
		item["namespace"] = tool.namespace
	}
	frame := b.emit("response.output_item.done", map[string]any{"output_index": tool.outputIndex, "item": item})
	b.output = append(b.output, item)
	b.outputIndex++
	b.tool = nil
	return []Frame{frame}
}
