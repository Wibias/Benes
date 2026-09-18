package request

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
)

var (
	ErrTooLarge        = errors.New("chat completions request exceeds configured byte limit")
	ErrUnsupportedTool = errors.New("chat completions tool is not represented by the canonical tool contract")
)

type DecodeOptions struct {
	NowMillis   int64
	IDGenerator func() string
}

type rec map[string]json.RawMessage

func Decode(reader io.Reader, maxBytes int64, options DecodeOptions) (protocol.ParsedRequest, error) {
	if reader == nil {
		return protocol.ParsedRequest{}, fmt.Errorf("chat request body is required")
	}
	if maxBytes <= 0 {
		maxBytes = 16 << 20
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return protocol.ParsedRequest{}, fmt.Errorf("read Chat Completions request: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return protocol.ParsedRequest{}, ErrTooLarge
	}
	var root rec
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil || root == nil {
		if err == nil {
			err = fmt.Errorf("request body must be a JSON object")
		}
		return protocol.ParsedRequest{}, fmt.Errorf("decode Chat Completions request: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return protocol.ParsedRequest{}, fmt.Errorf("decode Chat Completions request: %w", err)
	}

	model, ok := stringField(root, "model")
	if !ok || model == "" {
		return protocol.ParsedRequest{}, fmt.Errorf("model is required")
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(root["messages"], &messages); err != nil || len(messages) == 0 {
		return protocol.ParsedRequest{}, fmt.Errorf("messages must be a non-empty array")
	}

	now := options.NowMillis
	if now == 0 {
		now = time.Now().UnixMilli()
	}
	idGenerator := options.IDGenerator
	if idGenerator == nil {
		idGenerator = randomID
	}

	ctx, err := buildContext(messages, root["tools"], now, idGenerator)
	if err != nil {
		return protocol.ParsedRequest{}, err
	}
	if len(ctx.Messages) == 0 && len(ctx.SystemPrompt) == 0 {
		return protocol.ParsedRequest{}, fmt.Errorf("messages must include at least one user/assistant/tool turn")
	}

	opts, structured, err := buildOptions(root, ctx.Tools)
	if err != nil {
		return protocol.ParsedRequest{}, err
	}
	stream := boolField(root, "stream")
	return protocol.ParsedRequest{
		Source:           protocol.RequestSourceChatCompletions,
		ModelID:          model,
		Context:          ctx,
		Stream:           stream,
		Options:          opts,
		Raw:              append(json.RawMessage(nil), body...),
		StructuredOutput: structured,
	}, nil
}

func buildContext(messages []json.RawMessage, toolsRaw json.RawMessage, now int64, idGenerator func() string) (protocol.Context, error) {
	ctx := protocol.Context{}
	knownNames := make(map[string]string)
	for _, raw := range messages {
		var msg rec
		if json.Unmarshal(raw, &msg) != nil || msg == nil {
			continue
		}
		role, _ := stringField(msg, "role")
		switch role {
		case "system", "developer":
			if text := contentToText(msg["content"]); strings.TrimSpace(text) != "" {
				ctx.SystemPrompt = append(ctx.SystemPrompt, strings.TrimSpace(text))
			}
		case "user":
			parts := userContent(msg["content"])
			if len(parts) > 0 {
				ctx.Messages = append(ctx.Messages, protocol.Message{Role: protocol.RoleUser, Content: parts, Timestamp: now})
			}
		case "assistant":
			parts := assistantContent(msg["content"])
			if thinking := assistantThinking(msg); thinking != nil {
				parts = append([]protocol.ContentPart{*thinking}, parts...)
			}
			calls, err := assistantToolCalls(msg["tool_calls"], knownNames, idGenerator)
			if err != nil {
				return protocol.Context{}, err
			}
			parts = append(parts, calls...)
			if len(parts) > 0 {
				ctx.Messages = append(ctx.Messages, protocol.Message{Role: protocol.RoleAssistant, Content: parts, Timestamp: now})
			}
		case "tool":
			callID, _ := stringField(msg, "tool_call_id")
			if callID == "" {
				callID, _ = stringField(msg, "tool_use_id")
			}
			if callID == "" {
				return protocol.Context{}, fmt.Errorf("tool messages require tool_call_id")
			}
			ctx.Messages = append(ctx.Messages, protocol.Message{
				Role: protocol.RoleToolResult, ToolCallID: callID, ToolName: knownNames[callID], Timestamp: now,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: contentToText(msg["content"])}},
			})
		}
	}
	tools, err := decodeTools(toolsRaw)
	if err != nil {
		return protocol.Context{}, err
	}
	ctx.Tools = tools
	return ctx, nil
}

func contentToText(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	var text string
	if json.Unmarshal(trimmed, &text) == nil {
		return text
	}
	var blocks []json.RawMessage
	if json.Unmarshal(trimmed, &blocks) != nil {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, blockRaw := range blocks {
		var direct string
		if json.Unmarshal(blockRaw, &direct) == nil {
			parts = append(parts, direct)
			continue
		}
		var block rec
		if json.Unmarshal(blockRaw, &block) != nil {
			continue
		}
		typ, _ := stringField(block, "type")
		if typ != "text" && typ != "input_text" && typ != "output_text" {
			continue
		}
		if value, ok := stringField(block, "text"); ok {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, "\n")
}

func userContent(raw json.RawMessage) []protocol.ContentPart {
	trimmed := bytes.TrimSpace(raw)
	var text string
	if json.Unmarshal(trimmed, &text) == nil {
		if text == "" {
			return nil
		}
		return []protocol.ContentPart{{Type: protocol.ContentText, Text: text}}
	}
	var blocks []json.RawMessage
	if json.Unmarshal(trimmed, &blocks) != nil {
		return nil
	}
	parts := make([]protocol.ContentPart, 0, len(blocks))
	for _, blockRaw := range blocks {
		var direct string
		if json.Unmarshal(blockRaw, &direct) == nil {
			if direct != "" {
				parts = append(parts, protocol.ContentPart{Type: protocol.ContentText, Text: direct})
			}
			continue
		}
		var block rec
		if json.Unmarshal(blockRaw, &block) != nil {
			continue
		}
		typ, _ := stringField(block, "type")
		if typ == "text" || typ == "input_text" {
			if value, ok := stringField(block, "text"); ok {
				parts = append(parts, protocol.ContentPart{Type: protocol.ContentText, Text: value})
			}
			continue
		}
		if typ == "image_url" {
			if imageURL := decodeImageURL(block["image_url"]); imageURL != "" {
				parts = append(parts, protocol.ContentPart{Type: protocol.ContentImage, ImageURL: imageURL})
			}
		}
	}
	return parts
}

func assistantThinking(msg rec) *protocol.ContentPart {
	details := decodeReasoningDetails(msg["reasoning_details"])
	reasoning, _ := stringField(msg, "reasoning_content")
	if reasoning == "" && len(details) == 0 {
		return nil
	}
	part := protocol.ContentPart{Type: protocol.ContentThinking, Thinking: reasoning}
	if part.Thinking == "" {
		var builder strings.Builder
		for _, detail := range details {
			builder.WriteString(detail.Text)
		}
		part.Thinking = builder.String()
	}
	if len(details) > 0 {
		part.ProviderMetadata = &protocol.ProviderOpaqueMetadata{
			MiniMax: &protocol.MiniMaxOpaqueMetadata{ReasoningDetails: details},
		}
	}
	return &part
}

func decodeReasoningDetails(raw json.RawMessage) []protocol.MiniMaxReasoningDetail {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(trimmed, &items) != nil {
		return nil
	}
	out := make([]protocol.MiniMaxReasoningDetail, 0, len(items))
	for _, itemRaw := range items {
		var item rec
		if json.Unmarshal(itemRaw, &item) != nil || item == nil {
			continue
		}
		detail := protocol.MiniMaxReasoningDetail{
			Type:   stringValue(item, "type"),
			ID:     stringValue(item, "id"),
			Format: stringValue(item, "format"),
			Text:   stringValue(item, "text"),
		}
		if rawIndex := bytes.TrimSpace(item["index"]); len(rawIndex) > 0 && !bytes.Equal(rawIndex, []byte("null")) {
			var index int
			if json.Unmarshal(rawIndex, &index) == nil {
				detail.Index = &index
			}
		}
		if detail.Type == "" && detail.ID == "" && detail.Format == "" && detail.Text == "" && detail.Index == nil {
			continue
		}
		out = append(out, detail)
	}
	return out
}

func stringValue(fields rec, key string) string {
	value, _ := stringField(fields, key)
	return value
}

func assistantContent(raw json.RawMessage) []protocol.ContentPart {
	trimmed := bytes.TrimSpace(raw)
	var text string
	if json.Unmarshal(trimmed, &text) == nil {
		if text == "" {
			return nil
		}
		return []protocol.ContentPart{{Type: protocol.ContentText, Text: text}}
	}
	var blocks []json.RawMessage
	if json.Unmarshal(trimmed, &blocks) != nil {
		return nil
	}
	parts := make([]protocol.ContentPart, 0, len(blocks))
	for _, blockRaw := range blocks {
		var direct string
		if json.Unmarshal(blockRaw, &direct) == nil {
			if direct != "" {
				parts = append(parts, protocol.ContentPart{Type: protocol.ContentText, Text: direct})
			}
			continue
		}
		var block rec
		if json.Unmarshal(blockRaw, &block) != nil {
			continue
		}
		typ, _ := stringField(block, "type")
		if typ == "text" || typ == "output_text" {
			if value, ok := stringField(block, "text"); ok {
				parts = append(parts, protocol.ContentPart{Type: protocol.ContentText, Text: value})
			}
		}
	}
	return parts
}

func decodeImageURL(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var obj rec
	if json.Unmarshal(raw, &obj) != nil {
		return ""
	}
	value, _ = stringField(obj, "url")
	return value
}

func assistantToolCalls(raw json.RawMessage, knownNames map[string]string, idGenerator func() string) ([]protocol.ContentPart, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var calls []json.RawMessage
	if json.Unmarshal(raw, &calls) != nil {
		return nil, nil
	}
	parts := make([]protocol.ContentPart, 0, len(calls))
	for _, callRaw := range calls {
		var call rec
		if json.Unmarshal(callRaw, &call) != nil {
			continue
		}
		var fn rec
		_ = json.Unmarshal(call["function"], &fn)
		name, _ := stringField(fn, "name")
		if name == "" {
			name, _ = stringField(call, "name")
		}
		callID, _ := stringField(call, "id")
		if callID == "" {
			callID, _ = stringField(call, "call_id")
		}
		if callID == "" {
			callID = "call_" + idGenerator()
		}
		if name == "" {
			name = knownNames[callID]
		}
		if name == "" {
			return nil, fmt.Errorf("tool_calls entries require function.name")
		}
		knownNames[callID] = name
		argsRaw := fn["arguments"]
		if len(bytes.TrimSpace(argsRaw)) == 0 {
			argsRaw = call["arguments"]
		}
		parts = append(parts, protocol.ContentPart{Type: protocol.ContentToolCall, ToolCallID: callID, ToolName: name, Arguments: decodeArguments(argsRaw)})
	}
	return parts, nil
}

func decodeArguments(raw json.RawMessage) map[string]any {
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		var value any
		if json.Unmarshal([]byte(strings.TrimSpace(encoded)), &value) == nil {
			if obj, ok := value.(map[string]any); ok {
				return obj
			}
		}
		return map[string]any{}
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) == nil && obj != nil {
		return obj
	}
	return map[string]any{}
}

func decodeTools(raw json.RawMessage) ([]protocol.Tool, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return nil, fmt.Errorf("tools must be an array")
	}
	out := make([]protocol.Tool, 0, len(values))
	for _, valueRaw := range values {
		var value rec
		if json.Unmarshal(valueRaw, &value) != nil {
			continue
		}
		typ, _ := stringField(value, "type")
		if typ == "web_search" || typ == "web_search_preview" {
			out = append(out, protocol.Tool{Name: typ, HostedWebSearch: true})
			continue
		}
		if typ != "function" {
			continue
		}
		fields := value
		var nested rec
		if json.Unmarshal(value["function"], &nested) == nil && nested != nil {
			fields = nested
		}
		name, _ := stringField(fields, "name")
		if name == "" {
			continue
		}
		description, _ := stringField(fields, "description")
		params := map[string]any{}
		_ = json.Unmarshal(fields["parameters"], &params)
		var strict *bool
		if value, ok := optionalBool(fields["strict"]); ok {
			strict = &value
		}
		out = append(out, protocol.Tool{Name: name, Description: description, Parameters: params, Strict: strict})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func buildOptions(root rec, tools []protocol.Tool) (protocol.RequestOptions, bool, error) {
	opts := protocol.RequestOptions{HideThinkingSummary: true}
	if v, ok := numberField(root, "max_completion_tokens"); ok {
		opts.MaxOutputTokens = &v
	} else if v, ok := numberField(root, "max_tokens"); ok {
		opts.MaxOutputTokens = &v
	}
	if v, ok := numberField(root, "temperature"); ok {
		opts.Temperature = &v
	}
	if v, ok := numberField(root, "top_p"); ok {
		opts.TopP = &v
	}
	if v, ok := numberField(root, "presence_penalty"); ok {
		opts.PresencePenalty = &v
	}
	if v, ok := numberField(root, "frequency_penalty"); ok {
		opts.FrequencyPenalty = &v
	}
	if v, ok := stringField(root, "service_tier"); ok {
		opts.ServiceTier = &v
	}
	if v, ok := stringField(root, "prompt_cache_key"); ok {
		opts.PromptCacheKey = &v
	}
	if v, ok := stringField(root, "user"); ok {
		opts.User = &v
	}
	if raw := bytes.TrimSpace(root["metadata"]); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		var metadata map[string]any
		if json.Unmarshal(raw, &metadata) != nil || metadata == nil {
			return protocol.RequestOptions{}, false, fmt.Errorf("metadata must be an object")
		}
		opts.Metadata = metadata
	}
	if v, ok := optionalBool(root["parallel_tool_calls"]); ok {
		opts.ParallelToolCalls = &v
	}
	if raw := bytes.TrimSpace(root["stop"]); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		var one string
		if json.Unmarshal(raw, &one) == nil {
			opts.StopSequences = []string{one}
		} else {
			var many []string
			if json.Unmarshal(raw, &many) != nil {
				return protocol.RequestOptions{}, false, fmt.Errorf("stop must be a string or string array")
			}
			opts.StopSequences = many
		}
	}
	choice, err := decodeToolChoice(root["tool_choice"], tools)
	if err != nil {
		return protocol.RequestOptions{}, false, err
	}
	opts.ToolChoice = choice

	effort := reasoningEffort(root)
	summary, summarySpecified := reasoningSummary(root)
	opts.Reasoning = effort
	if effort != "" || summarySpecified {
		opts.HideThinkingSummary = summary == "none"
	}

	format, structured, err := decodeTextFormat(root["response_format"])
	if err != nil {
		return protocol.RequestOptions{}, false, err
	}
	opts.TextFormat = format
	return opts, structured, nil
}

func decodeToolChoice(raw json.RawMessage, _ []protocol.Tool) (*protocol.ToolChoice, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var scalar string
	if json.Unmarshal(trimmed, &scalar) == nil {
		switch scalar {
		case "auto":
			return &protocol.ToolChoice{Kind: protocol.ToolChoiceAuto}, nil
		case "none":
			return &protocol.ToolChoice{Kind: protocol.ToolChoiceNone}, nil
		case "required":
			return &protocol.ToolChoice{Kind: protocol.ToolChoiceRequired}, nil
		default:
			return nil, nil
		}
	}
	var obj rec
	if json.Unmarshal(trimmed, &obj) != nil {
		return nil, nil
	}
	typ, _ := stringField(obj, "type")
	var fn rec
	_ = json.Unmarshal(obj["function"], &fn)
	name, _ := stringField(obj, "name")
	if name == "" {
		name, _ = stringField(fn, "name")
	}
	if typ == "function" || fn != nil {
		if name == "" {
			return nil, fmt.Errorf("tool_choice.function requires a name")
		}
		return &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: name}, nil
	}
	return nil, nil
}

var reasoningEfforts = map[string]struct{}{"minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {}, "ultra": {}}
var reasoningSummaries = map[string]struct{}{"auto": {}, "concise": {}, "detailed": {}, "none": {}}

func reasoningEffort(root rec) string {
	if value, ok := stringField(root, "reasoning_effort"); ok {
		if _, valid := reasoningEfforts[value]; valid {
			return value
		}
	}
	var reasoning rec
	if json.Unmarshal(root["reasoning"], &reasoning) == nil {
		if value, ok := stringField(reasoning, "effort"); ok {
			if _, valid := reasoningEfforts[value]; valid {
				return value
			}
		}
	}
	return ""
}

func reasoningSummary(root rec) (string, bool) {
	var reasoning rec
	if json.Unmarshal(root["reasoning"], &reasoning) == nil {
		if value, ok := stringField(reasoning, "summary"); ok {
			if _, valid := reasoningSummaries[value]; valid {
				return value, true
			}
		}
	}
	if value, ok := optionalBool(root["include_reasoning"]); ok {
		if value {
			return "auto", true
		}
		return "none", true
	}
	if reasoningEffort(root) != "" {
		return "auto", true
	}
	return "", false
}

func decodeTextFormat(raw json.RawMessage) (*protocol.TextFormat, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false, nil
	}
	var obj rec
	if json.Unmarshal(trimmed, &obj) != nil {
		return nil, false, fmt.Errorf("response_format must be an object")
	}
	typ, _ := stringField(obj, "type")
	switch typ {
	case "", "text":
		return nil, false, nil
	case "json_object":
		return &protocol.TextFormat{Type: "json_object"}, true, nil
	case "json_schema":
		var schema rec
		if json.Unmarshal(obj["json_schema"], &schema) != nil || schema == nil {
			return nil, false, fmt.Errorf("response_format.json_schema is required for type json_schema")
		}
		name, _ := stringField(schema, "name")
		if name == "" {
			name = "response"
		}
		description, _ := stringField(schema, "description")
		var schemaValue map[string]any
		_ = json.Unmarshal(schema["schema"], &schemaValue)
		format := &protocol.TextFormat{Type: "json_schema", Name: name, Description: description, Schema: schemaValue}
		if value, ok := optionalBool(schema["strict"]); ok {
			format.Strict = &value
		}
		return format, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported response_format.type: %s", typ)
	}
}

func stringField(fields rec, key string) (string, bool) {
	var value string
	if json.Unmarshal(fields[key], &value) != nil {
		return "", false
	}
	return value, true
}

func boolField(fields rec, key string) bool {
	var value bool
	return json.Unmarshal(fields[key], &value) == nil && value
}

func optionalBool(raw json.RawMessage) (bool, bool) {
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return false, false
	}
	return value, true
}

func numberField(fields rec, key string) (float64, bool) {
	var value float64
	if json.Unmarshal(fields[key], &value) != nil {
		return 0, false
	}
	return value, true
}

func randomID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
