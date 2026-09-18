package parsed

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/history"
	"github.com/Wibias/Benes/internal/responses/hostedwebsearch"
	requestwire "github.com/Wibias/Benes/internal/responses/request"
)

var reasoningEfforts = map[string]struct{}{
	"none": {}, "minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {},
}

func Build(req *requestwire.Request, now int64) (protocol.ParsedRequest, error) {
	if req == nil {
		return protocol.ParsedRequest{}, fmt.Errorf("request is nil")
	}
	ctx, err := history.Build(req, now)
	if err != nil {
		return protocol.ParsedRequest{}, err
	}
	hostedTools, err := hostedwebsearch.ParseDeclared(req.Tools)
	if err != nil {
		return protocol.ParsedRequest{}, err
	}
	options, structuredOutput, err := buildOptions(req)
	if err != nil {
		return protocol.ParsedRequest{}, err
	}
	out := protocol.ParsedRequest{
		Source:               protocol.RequestSourceResponses,
		ModelID:              req.Model,
		Context:              ctx,
		HostedWebSearchTools: hostedTools,
		Options:              options,
		Raw:                  append(json.RawMessage(nil), req.Raw...),
		StructuredOutput:     structuredOutput,
	}
	if req.Stream != nil {
		out.Stream = *req.Stream
	}
	if req.PreviousResponseID != nil && *req.PreviousResponseID != "" {
		out.PreviousResponseID = *req.PreviousResponseID
	}
	out.CompactionRequest = compactionRequested(req)
	return out, nil
}

func compactionRequested(req *requestwire.Request) bool {
	for _, item := range req.Input.Items {
		if item.Type == "compaction_trigger" {
			return true
		}
	}
	return false
}

func buildOptions(req *requestwire.Request) (protocol.RequestOptions, bool, error) {
	o := protocol.RequestOptions{HideThinkingSummary: true}
	o.MaxOutputTokens = copyFloat(req.MaxOutputTokens)
	o.Temperature = copyFloat(req.Temperature)
	o.TopP = copyFloat(req.TopP)
	o.ParallelToolCalls = copyBool(req.ParallelToolCalls)
	o.ServiceTier = copyString(req.ServiceTier)
	o.PresencePenalty = copyFloat(req.PresencePenalty)
	o.FrequencyPenalty = copyFloat(req.FrequencyPenalty)
	o.PromptCacheKey = copyString(req.PromptCacheKey)
	o.User = copyString(req.User)
	o.Store = copyBool(req.Store)
	metadata, err := parseMetadata(req.Metadata)
	if err != nil {
		return protocol.RequestOptions{}, false, fmt.Errorf("metadata: %w", err)
	}
	o.Metadata = metadata

	stops, err := parseStop(req.Stop)
	if err != nil {
		return protocol.RequestOptions{}, false, fmt.Errorf("stop: %w", err)
	}
	o.StopSequences = stops

	choice, err := parseToolChoice(req.ToolChoice)
	if err != nil {
		return protocol.RequestOptions{}, false, fmt.Errorf("tool_choice: %w", err)
	}
	o.ToolChoice = choice

	reasoning, hide, err := parseReasoning(req.Reasoning)
	if err != nil {
		return protocol.RequestOptions{}, false, fmt.Errorf("reasoning: %w", err)
	}
	o.Reasoning = reasoning
	o.HideThinkingSummary = hide

	format := parseTextFormat(req.Text)
	o.TextFormat = format
	return o, format != nil, nil
}

func parseStop(raw json.RawMessage) ([]string, error) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		return nil, nil
	}
	if t[0] == '"' {
		var value string
		if err := json.Unmarshal(t, &value); err != nil {
			return nil, err
		}
		return []string{value}, nil
	}
	var values []string
	if err := json.Unmarshal(t, &values); err != nil {
		return nil, fmt.Errorf("must be string, string array, or null")
	}
	return values, nil
}

func parseReasoning(raw json.RawMessage) (string, bool, error) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		return "", true, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(t, &fields); err != nil {
		return "", true, fmt.Errorf("must be object or null")
	}
	effort := ""
	if rawEffort, ok := fields["effort"]; ok {
		var value string
		if err := json.Unmarshal(rawEffort, &value); err != nil {
			return "", true, fmt.Errorf("effort must be string")
		}
		if value == "ultra" {
			value = "max"
		}
		if _, ok := reasoningEfforts[value]; ok {
			effort = value
		}
	}
	hide := true
	if rawSummary, ok := fields["summary"]; ok {
		var summary string
		if err := json.Unmarshal(rawSummary, &summary); err != nil {
			return "", true, fmt.Errorf("summary must be string")
		}
		hide = summary == "" || summary == "none"
	}
	return effort, hide, nil
}

func parseToolChoice(raw json.RawMessage) (*protocol.ToolChoice, error) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return nil, nil
	}
	if bytes.Equal(t, []byte("null")) {
		return nil, fmt.Errorf("cannot be null")
	}
	if t[0] == '"' {
		var value string
		if err := json.Unmarshal(t, &value); err != nil {
			return nil, err
		}
		switch value {
		case "auto":
			return &protocol.ToolChoice{Kind: protocol.ToolChoiceAuto}, nil
		case "none":
			return &protocol.ToolChoice{Kind: protocol.ToolChoiceNone}, nil
		case "required":
			return &protocol.ToolChoice{Kind: protocol.ToolChoiceRequired}, nil
		default:
			return nil, fmt.Errorf("unsupported string %q", value)
		}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(t, &fields); err != nil {
		return nil, fmt.Errorf("must be string or object")
	}
	typeName, ok := rawString(fields["type"])
	if !ok {
		return nil, fmt.Errorf("object requires type")
	}
	switch typeName {
	case "function", "custom":
		name, ok := rawString(fields["name"])
		if !ok || name == "" {
			return nil, fmt.Errorf("%s choice requires name", typeName)
		}
		return &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: name}, nil
	case "web_search", "web_search_preview":
		return &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: typeName}, nil
	case "image_generation", "image_gen":
		return &protocol.ToolChoice{Kind: protocol.ToolChoiceNamed, Name: "image_gen"}, nil
	case "allowed_tools":
		return parseAllowedTools(fields)
	default:
		return &protocol.ToolChoice{Kind: protocol.ToolChoiceAuto}, nil
	}
}

func parseAllowedTools(fields map[string]json.RawMessage) (*protocol.ToolChoice, error) {
	mode, ok := rawString(fields["mode"])
	if !ok || (mode != "auto" && mode != "required") {
		return nil, fmt.Errorf("allowed_tools mode must be auto or required")
	}
	var tools []map[string]json.RawMessage
	if err := json.Unmarshal(fields["tools"], &tools); err != nil {
		return nil, fmt.Errorf("allowed_tools tools must be array")
	}
	seen := map[string]struct{}{}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		name := allowedToolName(tool)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return &protocol.ToolChoice{Kind: protocol.ToolChoiceNone}, nil
	}
	return &protocol.ToolChoice{
		Kind:         protocol.ToolChoiceAllowed,
		AllowedTools: names,
		AllowedMode:  protocol.ToolChoiceMode(mode),
	}, nil
}

func allowedToolName(tool map[string]json.RawMessage) string {
	if name, ok := rawString(tool["name"]); ok && name != "" {
		return name
	}
	typeName, _ := rawString(tool["type"])
	switch typeName {
	case "web_search", "web_search_preview":
		return typeName
	case "image_generation", "image_gen":
		return "image_gen"
	case "tool_search":
		return "tool_search"
	default:
		return ""
	}
}

func parseTextFormat(raw json.RawMessage) *protocol.TextFormat {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		return nil
	}
	var text map[string]json.RawMessage
	if json.Unmarshal(t, &text) != nil {
		return nil
	}
	var format map[string]json.RawMessage
	if json.Unmarshal(text["format"], &format) != nil {
		return nil
	}
	typeName, ok := rawString(format["type"])
	if !ok {
		return nil
	}
	if typeName == "json_object" {
		return &protocol.TextFormat{Type: "json_object"}
	}
	if typeName != "json_schema" {
		return nil
	}
	out := &protocol.TextFormat{Type: "json_schema"}
	out.Name, _ = rawString(format["name"])
	out.Description, _ = rawString(format["description"])
	if schemaRaw, ok := format["schema"]; ok {
		var schema map[string]any
		if json.Unmarshal(schemaRaw, &schema) == nil && schema != nil {
			out.Schema = schema
		}
	}
	if strictRaw, ok := format["strict"]; ok {
		var strict bool
		if json.Unmarshal(strictRaw, &strict) == nil {
			out.Strict = &strict
		}
	}
	return out
}

func parseMetadata(raw json.RawMessage) (map[string]any, error) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || bytes.Equal(t, []byte("null")) {
		return nil, nil
	}
	var value map[string]any
	if err := json.Unmarshal(t, &value); err != nil || value == nil {
		if err == nil {
			err = fmt.Errorf("must be object")
		}
		return nil, err
	}
	return value, nil
}

func rawString(raw json.RawMessage) (string, bool) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func copyFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func copyBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func copyString(value *string) *string {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}
