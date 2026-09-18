package openairesponses

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/hostedwebsearch"
)

func validateMigratedRequestForCompile(request protocol.ParsedRequest) error {
	if len(request.HostedWebSearchTools) == 0 && hasSidecarWebSearchTool(request.Context.Tools) {
		typed, err := hostedWebSearchToolsFromRaw(request.Raw)
		if err != nil {
			return err
		}
		if len(typed) == 0 {
			return fmt.Errorf("%w: synthetic sidecar web search has no hosted declaration", ErrUnsupportedRequestShape)
		}
		request.HostedWebSearchTools = typed
	}
	if request.CompactionRequest {
		if len(bytes.TrimSpace(request.Raw)) == 0 {
			return fmt.Errorf("%w: empty preserved request", ErrUnsupportedRequestShape)
		}
		return nil
	}
	// Fabric (and other Context-only synthesizers) compile from canonical Context
	// without a preserved /v1/responses Raw wire body.
	if len(bytes.TrimSpace(request.Raw)) == 0 {
		if len(request.Context.Messages) == 0 && len(request.Context.Tools) == 0 && len(request.Context.SystemPrompt) == 0 {
			return fmt.Errorf("%w: empty preserved request", ErrUnsupportedRequestShape)
		}
		if request.Options.ParallelToolCalls != nil && *request.Options.ParallelToolCalls {
			return fmt.Errorf("%w: parallel_tool_calls=true", ErrUnsupportedRequestShape)
		}
		return nil
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(request.Raw, &rawFields); err != nil {
		return fmt.Errorf("%w: malformed preserved request", ErrUnsupportedRequestShape)
	}
	if hasNonNullField(rawFields, "conversation") {
		return fmt.Errorf("%w: conversation", ErrUnsupportedRequestShape)
	}
	if request.Options.ParallelToolCalls != nil && *request.Options.ParallelToolCalls {
		return fmt.Errorf("%w: parallel_tool_calls=true", ErrUnsupportedRequestShape)
	}
	if requestsEncryptedReasoning(rawFields["include"]) {
		return fmt.Errorf("%w: reasoning.encrypted_content", ErrUnsupportedRequestShape)
	}
	if hasNonNullField(rawFields, "background") {
		return fmt.Errorf("%w: background", ErrUnsupportedRequestShape)
	}
	if hasNonNullField(rawFields, "prompt") {
		return fmt.Errorf("%w: stored prompt reference", ErrUnsupportedRequestShape)
	}
	if len(request.HostedWebSearchTools) == 0 {
		if err := validateNativeToolsForCompile(rawFields["tools"]); err != nil {
			return err
		}
	} else if err := validateNativeToolsWithHosted(rawFields["tools"], request.HostedWebSearchTools); err != nil {
		return err
	}
	if err := validateNativeInput(rawFields["input"]); err != nil {
		return err
	}
	return nil
}

func validateNativeToolsForCompile(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("%w: tools", ErrUnsupportedRequestShape)
	}
	var nativeTools []json.RawMessage
	if err := json.Unmarshal(trimmed, &nativeTools); err != nil {
		return fmt.Errorf("%w: tools", ErrUnsupportedRequestShape)
	}
	for index, rawTool := range nativeTools {
		if err := validateNativeTopLevelTool(rawTool, fmt.Sprintf("tools[%d]", index)); err != nil {
			return err
		}
	}
	return nil
}

func hasSidecarWebSearchTool(tools []protocol.Tool) bool {
	for _, tool := range tools {
		if tool.SidecarWebSearch {
			return true
		}
	}
	return false
}

func hostedWebSearchToolsFromRaw(raw json.RawMessage) ([]protocol.HostedWebSearchTool, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: empty preserved request", ErrUnsupportedRequestShape)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("%w: malformed preserved request", ErrUnsupportedRequestShape)
	}
	toolsRaw := bytes.TrimSpace(fields["tools"])
	if len(toolsRaw) == 0 || bytes.Equal(toolsRaw, []byte("null")) {
		return nil, nil
	}
	var nativeTools []json.RawMessage
	if err := json.Unmarshal(toolsRaw, &nativeTools); err != nil {
		return nil, fmt.Errorf("%w: tools", ErrUnsupportedRequestShape)
	}
	out := make([]protocol.HostedWebSearchTool, 0, len(nativeTools))
	for index, rawTool := range nativeTools {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawTool, &fields); err != nil || fields == nil {
			return nil, fmt.Errorf("%w: tools[%d]", ErrUnsupportedRequestShape, index)
		}
		var typeName string
		if err := json.Unmarshal(fields["type"], &typeName); err != nil {
			return nil, fmt.Errorf("%w: tools[%d]", ErrUnsupportedRequestShape, index)
		}
		switch typeName {
		case "function", "namespace":
			continue
		case string(protocol.HostedWebSearchWebSearch), string(protocol.HostedWebSearchPreview):
			parsed, err := hostedwebsearch.Parse(rawTool)
			if err != nil {
				return nil, fmt.Errorf("%w: tools[%d]", ErrUnsupportedRequestShape, index)
			}
			out = append(out, parsed)
		default:
			return nil, fmt.Errorf("%w: tools[%d]", ErrUnsupportedRequestShape, index)
		}
	}
	return out, nil
}

func validateNativeToolsWithHosted(raw json.RawMessage, typed []protocol.HostedWebSearchTool) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		if len(typed) != 0 {
			return fmt.Errorf("%w: hosted tools are missing from preserved request", ErrUnsupportedRequestShape)
		}
		return nil
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("%w: tools", ErrUnsupportedRequestShape)
	}
	var nativeTools []json.RawMessage
	if err := json.Unmarshal(trimmed, &nativeTools); err != nil {
		return fmt.Errorf("%w: tools", ErrUnsupportedRequestShape)
	}
	hostedIndex := 0
	for index, rawTool := range nativeTools {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(rawTool, &fields); err != nil || fields == nil {
			return fmt.Errorf("%w: tools[%d]", ErrUnsupportedRequestShape, index)
		}
		var typeName string
		if err := json.Unmarshal(fields["type"], &typeName); err != nil {
			return fmt.Errorf("%w: tools[%d]", ErrUnsupportedRequestShape, index)
		}
		switch typeName {
		case "function", "namespace":
			if err := validateNativeTopLevelTool(rawTool, fmt.Sprintf("tools[%d]", index)); err != nil {
				return err
			}
		case string(protocol.HostedWebSearchWebSearch), string(protocol.HostedWebSearchPreview):
			parsed, err := hostedwebsearch.Parse(rawTool)
			if err != nil || hostedIndex >= len(typed) || typed[hostedIndex].Type != parsed.Type {
				return fmt.Errorf("%w: tools[%d]", ErrUnsupportedRequestShape, index)
			}
			hostedIndex++
		default:
			return fmt.Errorf("%w: tools[%d]", ErrUnsupportedRequestShape, index)
		}
	}
	if hostedIndex != len(typed) {
		return fmt.Errorf("%w: hosted tool state does not match preserved request", ErrUnsupportedRequestShape)
	}
	return nil
}
