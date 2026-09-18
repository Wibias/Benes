package request

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

func buildOptions(root rec, maxTokens float64) (protocol.RequestOptions, bool, error) {
	opts := protocol.RequestOptions{MaxOutputTokens: &maxTokens, HideThinkingSummary: true}
	if value, ok, err := optionalNumber(root, "temperature"); err != nil {
		return protocol.RequestOptions{}, false, err
	} else if ok {
		opts.Temperature = &value
	}
	if value, ok, err := optionalNumber(root, "top_p"); err != nil {
		return protocol.RequestOptions{}, false, err
	} else if ok {
		opts.TopP = &value
	}
	if stop, err := stringArrayField(root, "stop_sequences"); err != nil {
		return protocol.RequestOptions{}, false, err
	} else if stop != nil {
		opts.StopSequences = stop
	}
	choice, parallel, err := decodeToolChoice(root["tool_choice"])
	if err != nil {
		return protocol.RequestOptions{}, false, err
	}
	opts.ToolChoice = choice
	opts.ParallelToolCalls = parallel
	if err := applyThinkingOptions(&opts, root["thinking"], root["output_config"]); err != nil {
		return protocol.RequestOptions{}, false, err
	}
	applyMetadataOptions(&opts, root["metadata"])
	format := decodeTextFormat(root["output_config"])
	if format != nil {
		opts.TextFormat = format
		return opts, true, nil
	}
	return opts, false, nil
}

func decodeToolChoice(raw json.RawMessage) (*protocol.ToolChoice, *bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil, nil
	}
	var choice rec
	if err := json.Unmarshal(trimmed, &choice); err != nil || choice == nil {
		return nil, nil, fmt.Errorf("tool_choice must be an object")
	}
	typ, _ := stringField(choice, "type")
	var out *protocol.ToolChoice
	switch typ {
	case "auto":
		out = &protocol.ToolChoice{Kind: protocol.ToolChoiceAuto}
	case "any":
		out = &protocol.ToolChoice{Kind: protocol.ToolChoiceRequired}
	case "none":
		out = &protocol.ToolChoice{Kind: protocol.ToolChoiceNone}
	case "tool":
		name, ok := stringField(choice, "name")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, nil, fmt.Errorf("named tool_choice requires name")
		}
		out = &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: name}
	default:
		return nil, nil, fmt.Errorf("unsupported tool_choice type %q", typ)
	}
	var parallel *bool
	if rawDisable, exists := choice["disable_parallel_tool_use"]; exists {
		var disabled bool
		if err := json.Unmarshal(rawDisable, &disabled); err != nil {
			return nil, nil, fmt.Errorf("disable_parallel_tool_use must be a boolean")
		}
		value := !disabled
		parallel = &value
	}
	return out, parallel, nil
}

func applyThinkingOptions(opts *protocol.RequestOptions, thinkingRaw, outputRaw json.RawMessage) error {
	trimmed := bytes.TrimSpace(thinkingRaw)
	var thinking rec
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		if err := json.Unmarshal(trimmed, &thinking); err != nil || thinking == nil {
			return fmt.Errorf("thinking must be an object")
		}
	}
	thinkingType, hasThinkingType := stringField(thinking, "type")
	if thinking != nil && !hasThinkingType {
		return fmt.Errorf("thinking.type is required")
	}
	if thinkingType == "disabled" {
		opts.Reasoning = "none"
		opts.HideThinkingSummary = true
		return nil
	}
	if thinkingType != "" && thinkingType != "enabled" && thinkingType != "adaptive" {
		return fmt.Errorf("unsupported thinking.type %q", thinkingType)
	}
	if thinkingType == "enabled" || thinkingType == "adaptive" {
		opts.HideThinkingSummary = false
	}
	if thinkingType == "enabled" {
		budget, ok := positiveNumberField(thinking, "budget_tokens")
		if !ok {
			return fmt.Errorf("thinking.budget_tokens must be a positive number")
		}
		switch {
		case budget <= 4096:
			opts.Reasoning = "low"
		case budget <= 16384:
			opts.Reasoning = "medium"
		default:
			opts.Reasoning = "high"
		}
	}
	var output rec
	if len(bytes.TrimSpace(outputRaw)) > 0 && !bytes.Equal(bytes.TrimSpace(outputRaw), []byte("null")) {
		if err := json.Unmarshal(outputRaw, &output); err != nil || output == nil {
			return fmt.Errorf("output_config must be an object")
		}
	}
	if effort, ok := stringField(output, "effort"); ok {
		if _, known := anthropicEfforts[effort]; known {
			opts.Reasoning = effort
			opts.HideThinkingSummary = false
		}
	}
	return nil
}

func applyMetadataOptions(opts *protocol.RequestOptions, raw json.RawMessage) {
	var metadata rec
	if json.Unmarshal(raw, &metadata) != nil || metadata == nil {
		return
	}
	userID, ok := stringField(metadata, "user_id")
	if !ok || userID == "" {
		return
	}
	opts.User = &userID
	hash := sha256.Sum256([]byte(userID))
	cache := hex.EncodeToString(hash[:])[:32]
	opts.PromptCacheKey = &cache
}

func decodeTextFormat(raw json.RawMessage) *protocol.TextFormat {
	var output rec
	if json.Unmarshal(raw, &output) != nil || output == nil {
		return nil
	}
	var format rec
	if json.Unmarshal(output["format"], &format) != nil || format == nil {
		return nil
	}
	typ, _ := stringField(format, "type")
	if typ != "json_schema" {
		return nil
	}
	var schema map[string]any
	if json.Unmarshal(format["schema"], &schema) != nil || schema == nil || !supportedOutputSchema(schema) {
		return nil
	}
	return &protocol.TextFormat{Type: "json_schema", Name: "response", Schema: schema}
}

func supportedOutputSchema(schema map[string]any) bool {
	if typ, ok := schema["type"].(string); ok && typ == "object" {
		return true
	}
	ref, ok := schema["$ref"].(string)
	return ok && strings.TrimSpace(ref) != ""
}
