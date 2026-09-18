package request

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrTooLarge = errors.New("responses request exceeds configured byte limit")

type Request struct {
	Raw                json.RawMessage   `json:"-"`
	Model              string            `json:"model"`
	Input              Input             `json:"-"`
	Instructions       json.RawMessage   `json:"instructions,omitempty"`
	Tools              []json.RawMessage `json:"tools,omitempty"`
	ToolChoice         json.RawMessage   `json:"tool_choice,omitempty"`
	MaxOutputTokens    *float64          `json:"max_output_tokens,omitempty"`
	Temperature        *float64          `json:"temperature,omitempty"`
	TopP               *float64          `json:"top_p,omitempty"`
	Stop               json.RawMessage   `json:"stop,omitempty"`
	Stream             *bool             `json:"stream,omitempty"`
	Reasoning          json.RawMessage   `json:"reasoning,omitempty"`
	Store              *bool             `json:"store,omitempty"`
	PreviousResponseID *string           `json:"previous_response_id,omitempty"`
	ParallelToolCalls  *bool             `json:"parallel_tool_calls,omitempty"`
	PromptCacheKey     *string           `json:"prompt_cache_key,omitempty"`
	Metadata           json.RawMessage   `json:"metadata,omitempty"`
	User               *string           `json:"user,omitempty"`
	ServiceTier        *string           `json:"service_tier,omitempty"`
	PresencePenalty    *float64          `json:"presence_penalty,omitempty"`
	FrequencyPenalty   *float64          `json:"frequency_penalty,omitempty"`
	Background         json.RawMessage   `json:"background,omitempty"`
	Include            json.RawMessage   `json:"include,omitempty"`
	Prompt             json.RawMessage   `json:"prompt,omitempty"`
	Text               json.RawMessage   `json:"text,omitempty"`
	Truncation         json.RawMessage   `json:"truncation,omitempty"`
}

type Input struct {
	Text  *string
	Items []Item
}

type Item struct {
	Type string
	Role string
	Raw  json.RawMessage
}

func Decode(reader io.Reader, maxBytes int64) (*Request, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("maxBytes must be positive")
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read responses request: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrTooLarge
	}

	var wire struct {
		Model              string            `json:"model"`
		Input              json.RawMessage   `json:"input"`
		Instructions       json.RawMessage   `json:"instructions"`
		Tools              []json.RawMessage `json:"tools"`
		ToolChoice         json.RawMessage   `json:"tool_choice"`
		MaxOutputTokens    *float64          `json:"max_output_tokens"`
		Temperature        *float64          `json:"temperature"`
		TopP               *float64          `json:"top_p"`
		Stop               json.RawMessage   `json:"stop"`
		Stream             *bool             `json:"stream"`
		Reasoning          json.RawMessage   `json:"reasoning"`
		Store              *bool             `json:"store"`
		PreviousResponseID *string           `json:"previous_response_id"`
		ParallelToolCalls  *bool             `json:"parallel_tool_calls"`
		PromptCacheKey     *string           `json:"prompt_cache_key"`
		Metadata           json.RawMessage   `json:"metadata"`
		User               *string           `json:"user"`
		ServiceTier        *string           `json:"service_tier"`
		PresencePenalty    *float64          `json:"presence_penalty"`
		FrequencyPenalty   *float64          `json:"frequency_penalty"`
		Background         json.RawMessage   `json:"background"`
		Include            json.RawMessage   `json:"include"`
		Prompt             json.RawMessage   `json:"prompt"`
		Text               json.RawMessage   `json:"text"`
		Truncation         json.RawMessage   `json:"truncation"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode responses request: %w", err)
	}
	if wire.Model == "" {
		return nil, fmt.Errorf("model must contain at least one character")
	}

	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(body, &rawFields); err != nil {
		return nil, fmt.Errorf("decode responses request fields: %w", err)
	}
	if err := validateTopLevelFields(rawFields); err != nil {
		return nil, err
	}

	input, err := decodeInput(wire.Input)
	if err != nil {
		return nil, err
	}
	if err := validateTools(wire.Tools); err != nil {
		return nil, err
	}
	if err := validateToolChoice(wire.ToolChoice); err != nil {
		return nil, err
	}
	if err := validateReasoning(wire.Reasoning); err != nil {
		return nil, err
	}

	return &Request{
		Raw:   append(json.RawMessage(nil), body...),
		Model: wire.Model, Input: input, Instructions: wire.Instructions, Tools: wire.Tools,
		ToolChoice: wire.ToolChoice, MaxOutputTokens: wire.MaxOutputTokens, Temperature: wire.Temperature,
		TopP: wire.TopP, Stop: wire.Stop, Stream: wire.Stream, Reasoning: wire.Reasoning, Store: wire.Store,
		PreviousResponseID: wire.PreviousResponseID, ParallelToolCalls: wire.ParallelToolCalls,
		PromptCacheKey: wire.PromptCacheKey, Metadata: wire.Metadata, User: wire.User, ServiceTier: wire.ServiceTier,
		PresencePenalty: wire.PresencePenalty, FrequencyPenalty: wire.FrequencyPenalty, Background: wire.Background,
		Include: wire.Include, Prompt: wire.Prompt, Text: wire.Text, Truncation: wire.Truncation,
	}, nil
}

func validateTopLevelFields(fields map[string]json.RawMessage) error {
	if raw, ok := fields["instructions"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("instructions must be string or null")
		}
	}
	if raw, ok := fields["tools"]; ok {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("tools must be an array")
		}
		var tools []json.RawMessage
		if err := json.Unmarshal(raw, &tools); err != nil {
			return fmt.Errorf("tools must be an array")
		}
	}
	for _, key := range []string{"max_output_tokens", "temperature", "top_p", "presence_penalty", "frequency_penalty"} {
		if raw, ok := fields[key]; ok {
			if isNull(raw) {
				return fmt.Errorf("%s must be a number", key)
			}
			var value float64
			if err := json.Unmarshal(raw, &value); err != nil {
				return fmt.Errorf("%s must be a number", key)
			}
		}
	}
	for _, key := range []string{"stream", "store", "parallel_tool_calls"} {
		if raw, ok := fields[key]; ok {
			if isNull(raw) {
				return fmt.Errorf("%s must be a boolean", key)
			}
			var value bool
			if err := json.Unmarshal(raw, &value); err != nil {
				return fmt.Errorf("%s must be a boolean", key)
			}
		}
	}
	for _, key := range []string{"previous_response_id", "prompt_cache_key", "user", "service_tier"} {
		if raw, ok := fields[key]; ok {
			if isNull(raw) {
				return fmt.Errorf("%s must be a string", key)
			}
			var value string
			if err := json.Unmarshal(raw, &value); err != nil {
				return fmt.Errorf("%s must be a string", key)
			}
		}
	}
	if raw, ok := fields["stop"]; ok {
		trimmed := bytes.TrimSpace(raw)
		if !bytes.Equal(trimmed, []byte("null")) {
			if len(trimmed) == 0 {
				return fmt.Errorf("stop has invalid value")
			}
			if trimmed[0] == '"' {
				var value string
				if err := json.Unmarshal(raw, &value); err != nil {
					return fmt.Errorf("stop must be string, string array, or null")
				}
			} else {
				var values []string
				if err := json.Unmarshal(raw, &values); err != nil {
					return fmt.Errorf("stop must be string, string array, or null")
				}
			}
		}
	}
	return nil
}

func decodeInput(raw json.RawMessage) (Input, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return Input{}, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return Input{}, fmt.Errorf("input must be a string or array")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return Input{}, fmt.Errorf("input: %w", err)
		}
		return Input{Text: &text}, nil
	}
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return Input{}, fmt.Errorf("input must be a string or array")
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(raw, &raws); err != nil {
		return Input{}, fmt.Errorf("input: %w", err)
	}
	items := make([]Item, 0, len(raws))
	for index, itemRaw := range raws {
		item, err := decodeItem(itemRaw)
		if err != nil {
			return Input{}, fmt.Errorf("input[%d]: %w", index, err)
		}
		items = append(items, item)
	}
	return Input{Items: items}, nil
}

func decodeItem(raw json.RawMessage) (Item, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Item{}, fmt.Errorf("item must be an object: %w", err)
	}
	copyRaw := append(json.RawMessage(nil), raw...)
	if typeRaw, ok := fields["type"]; ok {
		var typeName string
		if isNull(typeRaw) || json.Unmarshal(typeRaw, &typeName) != nil {
			return Item{}, fmt.Errorf("item type must be string")
		}
		var role string
		if roleRaw, ok := fields["role"]; ok && !isNull(roleRaw) {
			_ = json.Unmarshal(roleRaw, &role)
		}
		// The current TypeScript union ends in a loose {type:string} branch.
		// Any typed object can therefore fall through to that branch. Preserve
		// this externally observable behavior and defer semantic validation.
		return Item{Type: typeName, Role: role, Raw: copyRaw}, nil
	}

	var envelope struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
		Phase   json.RawMessage `json:"phase"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return Item{}, fmt.Errorf("message item: %w", err)
	}
	if envelope.Role == "" {
		return Item{}, fmt.Errorf("item requires type or role")
	}
	switch envelope.Role {
	case "user", "developer", "system", "assistant":
	default:
		return Item{}, fmt.Errorf("unsupported message role %q", envelope.Role)
	}
	if err := validateMessageContent(envelope.Role, envelope.Content); err != nil {
		return Item{}, err
	}
	if envelope.Role == "assistant" && len(bytes.TrimSpace(envelope.Phase)) > 0 {
		var phase string
		if isNull(envelope.Phase) || json.Unmarshal(envelope.Phase, &phase) != nil || (phase != "commentary" && phase != "final_answer") {
			return Item{}, fmt.Errorf("assistant phase must be commentary or final_answer")
		}
	}
	return Item{Role: envelope.Role, Raw: copyRaw}, nil
}

func validateMessageContent(role string, raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("%s message content cannot be null", role)
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return fmt.Errorf("message content: %w", err)
		}
		return nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return fmt.Errorf("message content must be string or array: %w", err)
	}
	for index, block := range blocks {
		if err := validateContentBlock(role, block); err != nil {
			return fmt.Errorf("content[%d]: %w", index, err)
		}
	}
	return nil
}

func validateContentBlock(role string, raw json.RawMessage) error {
	var block map[string]json.RawMessage
	if err := json.Unmarshal(raw, &block); err != nil {
		return fmt.Errorf("content block must be object: %w", err)
	}
	typeName, err := requiredString(block, "type")
	if err != nil {
		return err
	}
	if role == "assistant" {
		switch typeName {
		case "output_text", "text":
			_, err = requiredString(block, "text")
			return err
		case "refusal":
			_, err = requiredString(block, "refusal")
			return err
		default:
			return fmt.Errorf("unsupported assistant content type %q", typeName)
		}
	}
	switch typeName {
	case "input_text", "text":
		_, err = requiredString(block, "text")
		return err
	case "input_image":
		for _, key := range []string{"image_url", "file_id"} {
			if value, ok := block[key]; ok {
				var text string
				if isNull(value) || json.Unmarshal(value, &text) != nil {
					return fmt.Errorf("input_image %s must be string", key)
				}
			}
		}
		var detail string
		if value, ok := block["detail"]; ok {
			if isNull(value) || json.Unmarshal(value, &detail) != nil {
				return fmt.Errorf("input_image detail must be string")
			}
			if detail != "auto" && detail != "low" && detail != "high" && detail != "original" {
				return fmt.Errorf("input_image has invalid detail %q", detail)
			}
		}
		if !hasString(block, "image_url") && !hasString(block, "file_id") {
			return fmt.Errorf("input_image requires image_url or file_id")
		}
		return nil
	case "input_file":
		for _, key := range []string{"file_id", "filename", "file_data"} {
			if value, ok := block[key]; ok {
				var text string
				if isNull(value) || json.Unmarshal(value, &text) != nil {
					return fmt.Errorf("input_file %s must be string", key)
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported input content type %q", typeName)
	}
}

func validateTools(tools []json.RawMessage) error {
	for index, raw := range tools {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return fmt.Errorf("tools[%d] must be object: %w", index, err)
		}
		if _, err := requiredString(fields, "type"); err != nil {
			return fmt.Errorf("tools[%d]: %w", index, err)
		}
	}
	return nil
}

func validateToolChoice(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("tool_choice cannot be null")
	}
	if trimmed[0] == '"' {
		var choice string
		if err := json.Unmarshal(raw, &choice); err != nil {
			return fmt.Errorf("tool_choice: %w", err)
		}
		if choice != "auto" && choice != "none" && choice != "required" {
			return fmt.Errorf("invalid tool_choice %q", choice)
		}
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("tool_choice must be string or object: %w", err)
	}
	typeName, err := requiredString(fields, "type")
	if err != nil {
		return fmt.Errorf("tool_choice: %w", err)
	}
	switch typeName {
	case "function", "custom":
		_, err := requiredMinOneString(fields, "name")
		return err
	case "web_search", "web_search_preview", "file_search", "computer_use_preview", "code_interpreter", "image_generation", "mcp":
		return nil
	case "allowed_tools":
		mode, err := requiredString(fields, "mode")
		if err != nil {
			return err
		}
		if mode != "auto" && mode != "required" {
			return fmt.Errorf("allowed_tools mode must be auto or required")
		}
		var tools []map[string]json.RawMessage
		if err := json.Unmarshal(fields["tools"], &tools); err != nil {
			return fmt.Errorf("allowed_tools tools must be array")
		}
		for index, tool := range tools {
			if _, err := requiredString(tool, "type"); err != nil {
				return fmt.Errorf("allowed_tools.tools[%d]: %w", index, err)
			}
			if rawName, ok := tool["name"]; ok {
				var name string
				if isNull(rawName) || json.Unmarshal(rawName, &name) != nil {
					return fmt.Errorf("allowed_tools.tools[%d].name must be string", index)
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported tool_choice type %q", typeName)
	}
}

func validateReasoning(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("reasoning must be object or null")
	}
	if summaryRaw, ok := fields["summary"]; ok {
		if isNull(summaryRaw) {
			return fmt.Errorf("reasoning.summary must be string")
		}
		var summary string
		if err := json.Unmarshal(summaryRaw, &summary); err != nil {
			return fmt.Errorf("reasoning.summary must be string")
		}
		if summary != "auto" && summary != "concise" && summary != "detailed" && summary != "none" {
			return fmt.Errorf("invalid reasoning.summary %q", summary)
		}
	}
	if effortRaw, ok := fields["effort"]; ok {
		if isNull(effortRaw) {
			return fmt.Errorf("reasoning.effort must be string")
		}
		var effort string
		if err := json.Unmarshal(effortRaw, &effort); err != nil {
			return fmt.Errorf("reasoning.effort must be string")
		}
	}
	return nil
}

func isNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func requiredString(fields map[string]json.RawMessage, key string) (string, error) {
	raw, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("requires %s", key)
	}
	if isNull(raw) {
		return "", fmt.Errorf("%s must be string", key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be string", key)
	}
	return value, nil
}

func requiredMinOneString(fields map[string]json.RawMessage, key string) (string, error) {
	value, err := requiredString(fields, key)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("%s must contain at least one character", key)
	}
	return value, nil
}

func hasString(fields map[string]json.RawMessage, key string) bool {
	raw, ok := fields[key]
	if !ok {
		return false
	}
	if isNull(raw) {
		return false
	}
	var value string
	return json.Unmarshal(raw, &value) == nil
}
