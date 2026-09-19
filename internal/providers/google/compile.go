package google

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

const (
	missingToolResultFmt = `[benes] no tool result was recorded for %q; execution status unknown - do not treat this as success, failure, or user-provided input.`
	orphanToolResultFmt  = `[benes] orphaned tool result for %q: %s`
)

type pendingCall struct {
	id   string
	name string
}

func decodeStrictBase64(raw string) ([]byte, error) {
	return base64.StdEncoding.Strict().DecodeString(raw)
}

func CompileRequest(parsed protocol.ParsedRequest) (map[string]any, error) {
	var (
		contents []map[string]any
		pending  []pendingCall
		buffered []protocol.Message
	)
	flush := func() {
		contents = append(contents, repairedToolTurns(pending, buffered)...)
		pending = nil
		buffered = nil
	}
	for _, message := range parsed.Context.Messages {
		switch message.Role {
		case protocol.RoleUser, protocol.RoleDeveloper:
			flush()
			parts, err := userParts(message.Content)
			if err != nil {
				return nil, err
			}
			if len(parts) == 0 {
				return nil, fmt.Errorf("Google messages must contain text")
			}
			contents = append(contents, map[string]any{"role": "user", "parts": parts})
		case protocol.RoleAssistant:
			flush()
			var parts []map[string]any
			for _, part := range message.Content {
				switch part.Type {
				case protocol.ContentText:
					if part.Text != "" {
						parts = append(parts, map[string]any{"text": part.Text})
					}
				case protocol.ContentToolCall:
					name := strings.TrimSpace(part.ToolName)
					if name == "" {
						return nil, fmt.Errorf("Google tool call is missing a name")
					}
					call := map[string]any{"name": name, "args": part.Arguments}
					if part.ToolCallID != "" {
						call["id"] = part.ToolCallID
					}
					item := map[string]any{"functionCall": call}
					if sig := thoughtSignature(part); sig != "" {
						item["thoughtSignature"] = sig
					}
					parts = append(parts, item)
					pending = append(pending, pendingCall{id: strings.TrimSpace(part.ToolCallID), name: name})
				}
			}
			if len(parts) == 0 {
				return nil, fmt.Errorf("Google assistant messages must contain text or a tool call")
			}
			contents = append(contents, map[string]any{"role": "model", "parts": parts})
		case protocol.RoleToolResult:
			if len(pending) == 0 {
				contents = append(contents, orphanToolResultTurn(message))
				continue
			}
			buffered = append(buffered, message)
		default:
			return nil, fmt.Errorf("Google conversation role %q is unsupported", message.Role)
		}
	}
	flush()
	if len(contents) == 0 {
		return nil, fmt.Errorf("Google request must contain at least one message")
	}
	body := map[string]any{"contents": contents}
	if prompt := strings.TrimSpace(strings.Join(parsed.Context.SystemPrompt, "\n")); prompt != "" {
		body["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": prompt}}}
	}
	if decls := functionDeclarations(parsed.Context.Tools, parsed.Options.ToolChoice); len(decls) > 0 {
		body["tools"] = []map[string]any{{"functionDeclarations": decls}}
		if cfg := toolConfig(parsed.Options.ToolChoice, parsed.Context.Tools); cfg != nil {
			body["toolConfig"] = cfg
		}
	}
	if parsed.StructuredOutput || parsed.Options.TextFormat != nil {
		generationConfig, err := structuredGenerationConfig(parsed.Options.TextFormat)
		if err != nil {
			return nil, err
		}
		body["generationConfig"] = generationConfig
	}
	return body, nil
}

func structuredGenerationConfig(format *protocol.TextFormat) (map[string]any, error) {
	if format == nil {
		return nil, fmt.Errorf("Google structured output requires an explicit format")
	}
	out := map[string]any{"responseMimeType": "application/json"}
	switch strings.TrimSpace(format.Type) {
	case "json_object":
		return out, nil
	case "json_schema":
		if len(format.Schema) == 0 {
			return nil, fmt.Errorf("Google JSON schema structured output requires a schema")
		}
		raw, err := json.Marshal(format.Schema)
		if err != nil {
			return nil, fmt.Errorf("encode Google response JSON schema: %w", err)
		}
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil || schema == nil {
			if err == nil {
				err = fmt.Errorf("schema must be a JSON object")
			}
			return nil, fmt.Errorf("clone Google response JSON schema: %w", err)
		}
		out["responseJsonSchema"] = schema
		return out, nil
	default:
		return nil, fmt.Errorf("Google structured output format %q is unsupported", format.Type)
	}
}

func repairedToolTurns(calls []pendingCall, results []protocol.Message) []map[string]any {
	if len(calls) == 0 && len(results) == 0 {
		return nil
	}
	used := make([]bool, len(results))
	var responses []map[string]any
	for _, call := range calls {
		idx := matchToolResult(call, results, used)
		if idx >= 0 {
			used[idx] = true
			responses = append(responses, functionResponsePart(call, results[idx]))
			continue
		}
		responses = append(responses, missingFunctionResponse(call))
	}
	var turns []map[string]any
	if len(responses) > 0 {
		turns = append(turns, map[string]any{"role": "user", "parts": responses})
	}
	for i, result := range results {
		if used[i] {
			continue
		}
		turns = append(turns, orphanToolResultTurn(result))
	}
	return turns
}

func matchToolResult(call pendingCall, results []protocol.Message, used []bool) int {
	for i, result := range results {
		if used[i] {
			continue
		}
		if strings.TrimSpace(result.ToolCallID) == call.id {
			return i
		}
	}
	return -1
}

func functionResponsePart(call pendingCall, message protocol.Message) map[string]any {
	text := strings.TrimSpace(joinText(message.Content))
	if text == "" {
		text = "The tool completed without textual output."
	}
	name := strings.TrimSpace(message.ToolName)
	if name == "" {
		name = call.name
	}
	response := map[string]any{"name": name, "response": map[string]any{"result": text}}
	if call.id != "" {
		response["id"] = call.id
	}
	return map[string]any{"functionResponse": response}
}

func missingFunctionResponse(call pendingCall) map[string]any {
	response := map[string]any{
		"name":     call.name,
		"response": map[string]any{"result": fmt.Sprintf(missingToolResultFmt, call.name)},
	}
	if call.id != "" {
		response["id"] = call.id
	}
	return map[string]any{"functionResponse": response}
}

func orphanToolResultTurn(message protocol.Message) map[string]any {
	name := strings.TrimSpace(message.ToolName)
	if name == "" {
		name = "tool_result"
	}
	text := strings.TrimSpace(joinText(message.Content))
	if text == "" {
		text = "(empty)"
	}
	return map[string]any{
		"role":  "user",
		"parts": []map[string]any{{"text": fmt.Sprintf(orphanToolResultFmt, name, text)}},
	}
}

func userParts(parts []protocol.ContentPart) ([]map[string]any, error) {
	var out []map[string]any
	for _, part := range parts {
		switch part.Type {
		case protocol.ContentText:
			if part.Text != "" {
				out = append(out, map[string]any{"text": part.Text})
			}
		case protocol.ContentImage:
			inline, marker, err := inlineImage(part.ImageURL)
			if err != nil {
				return nil, err
			}
			if inline != nil {
				out = append(out, inline)
			} else if marker != "" {
				out = append(out, map[string]any{"text": marker})
			}
		}
	}
	return out, nil
}

func inlineImage(raw string) (map[string]any, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", nil
	}
	if !strings.HasPrefix(raw, "data:") {
		return nil, "[image: " + raw + "]", nil
	}
	meta, rest, ok := strings.Cut(strings.TrimPrefix(raw, "data:"), ",")
	if !ok || rest == "" {
		return nil, "", fmt.Errorf("Google image data URL is incomplete")
	}
	if _, err := decodeStrictBase64(rest); err != nil {
		return nil, "", fmt.Errorf("Google image payload is not strict base64")
	}
	mime := strings.TrimSpace(strings.TrimSuffix(meta, ";base64"))
	if mime == "" {
		mime = "image/png"
	}
	return map[string]any{"inline_data": map[string]any{"mime_type": mime, "data": rest}}, "", nil
}

func joinText(parts []protocol.ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Type == protocol.ContentText {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func thoughtSignature(part protocol.ContentPart) string {
	sig := strings.TrimSpace(part.ThoughtSignature)
	if sig != "" && !strings.HasPrefix(sig, "fc_") {
		return sig
	}
	if part.ProviderMetadata != nil && part.ProviderMetadata.Google != nil {
		return strings.TrimSpace(part.ProviderMetadata.Google.ThoughtSignature)
	}
	return ""
}

func namespacedToolName(tool protocol.Tool) string {
	name := strings.TrimSpace(tool.Name)
	if ns := strings.TrimSpace(tool.Namespace); ns != "" && name != "" {
		return ns + "__" + name
	}
	return name
}

func toolAllowed(tool protocol.Tool, allowed map[string]struct{}) bool {
	if allowed == nil {
		return true
	}
	for _, alias := range toolAliases(tool) {
		if _, ok := allowed[alias]; ok {
			return true
		}
	}
	return false
}

func toolAliases(tool protocol.Tool) []string {
	wire := namespacedToolName(tool)
	if ns := strings.TrimSpace(tool.Namespace); ns != "" {
		return []string{wire, ns + "." + strings.TrimSpace(tool.Name), strings.TrimSpace(tool.Name)}
	}
	return []string{wire}
}

func resolveToolChoiceWireName(tools []protocol.Tool, requested string) string {
	requested = strings.TrimSpace(requested)
	for _, tool := range tools {
		for _, alias := range toolAliases(tool) {
			if alias == requested {
				return namespacedToolName(tool)
			}
		}
	}
	return requested
}

func toolConfig(choice *protocol.ToolChoice, tools []protocol.Tool) map[string]any {
	if choice == nil || choice.Kind == protocol.ToolChoiceAuto {
		return nil
	}
	switch choice.Kind {
	case protocol.ToolChoiceNone:
		return map[string]any{"functionCallingConfig": map[string]any{"mode": "NONE"}}
	case protocol.ToolChoiceRequired:
		return map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY"}}
	case protocol.ToolChoiceAllowed:
		if choice.AllowedMode == protocol.ToolChoiceModeRequired {
			return map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY"}}
		}
		return nil
	case protocol.ToolChoiceNamed:
		return map[string]any{"functionCallingConfig": map[string]any{
			"mode":                 "ANY",
			"allowedFunctionNames": []string{resolveToolChoiceWireName(tools, choice.Name)},
		}}
	default:
		return nil
	}
}

func functionDeclarations(tools []protocol.Tool, choice *protocol.ToolChoice) []map[string]any {
	var allowed map[string]struct{}
	if choice != nil && choice.Kind == protocol.ToolChoiceAllowed {
		allowed = make(map[string]struct{}, len(choice.AllowedTools))
		for _, name := range choice.AllowedTools {
			if name = strings.TrimSpace(name); name != "" {
				allowed[name] = struct{}{}
			}
		}
	}
	var out []map[string]any
	for _, tool := range tools {
		if !toolAllowed(tool, allowed) {
			continue
		}
		name := namespacedToolName(tool)
		if name == "" {
			continue
		}
		decl := map[string]any{"name": name}
		if tool.Description != "" {
			decl["description"] = tool.Description
		}
		if tool.Parameters != nil {
			decl["parameters"] = SanitizeToolParameters(tool.Parameters)
		}
		out = append(out, decl)
	}
	return out
}
