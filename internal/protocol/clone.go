package protocol

func CloneParsedRequest(in ParsedRequest) ParsedRequest {
	out := in
	out.Raw = cloneSlice(in.Raw)
	out.Context = cloneContext(in.Context)
	out.Options = cloneOptions(in.Options)
	return out
}

func cloneContext(in Context) Context {
	out := Context{
		SystemPrompt: cloneSlice(in.SystemPrompt),
	}
	if in.Messages != nil {
		out.Messages = make([]Message, len(in.Messages))
		for i, message := range in.Messages {
			out.Messages[i] = cloneMessage(message)
		}
	}
	if in.Tools != nil {
		out.Tools = make([]Tool, len(in.Tools))
		for i, tool := range in.Tools {
			cloned := tool
			cloned.Parameters = cloneAnyMap(tool.Parameters)
			if tool.Strict != nil {
				value := *tool.Strict
				cloned.Strict = &value
			}
			out.Tools[i] = cloned
		}
	}
	return out
}

func cloneMessage(in Message) Message {
	out := in
	if in.Phase != nil {
		phase := *in.Phase
		out.Phase = &phase
	}
	if in.Content != nil {
		out.Content = make([]ContentPart, len(in.Content))
		for i, part := range in.Content {
			out.Content[i] = cloneContentPart(part)
		}
	}
	return out
}

func cloneContentPart(in ContentPart) ContentPart {
	out := in
	out.Redacted = cloneSlice(in.Redacted)
	out.Arguments = cloneAnyMap(in.Arguments)
	out.ProviderMetadata = CloneProviderOpaqueMetadata(in.ProviderMetadata)
	return out
}

func CloneProviderOpaqueMetadata(in *ProviderOpaqueMetadata) *ProviderOpaqueMetadata {
	if in == nil {
		return nil
	}
	out := *in
	if in.Google != nil {
		google := *in.Google
		out.Google = &google
	}
	if in.MiniMax != nil {
		minimax := *in.MiniMax
		if minimax.ReasoningDetails != nil {
			details := make([]MiniMaxReasoningDetail, len(minimax.ReasoningDetails))
			copy(details, minimax.ReasoningDetails)
			for i := range details {
				if details[i].Index != nil {
					index := *details[i].Index
					details[i].Index = &index
				}
			}
			minimax.ReasoningDetails = details
		}
		out.MiniMax = &minimax
	}
	return &out
}

func cloneOptions(in RequestOptions) RequestOptions {
	out := in
	out.StopSequences = cloneSlice(in.StopSequences)
	out.MaxOutputTokens = cloneFloat(in.MaxOutputTokens)
	out.Temperature = cloneFloat(in.Temperature)
	out.TopP = cloneFloat(in.TopP)
	out.PresencePenalty = cloneFloat(in.PresencePenalty)
	out.FrequencyPenalty = cloneFloat(in.FrequencyPenalty)
	out.ParallelToolCalls = cloneBool(in.ParallelToolCalls)
	out.ServiceTier = cloneString(in.ServiceTier)
	out.PromptCacheKey = cloneString(in.PromptCacheKey)
	out.User = cloneString(in.User)
	out.Store = cloneBool(in.Store)
	if in.ToolChoice != nil {
		choice := *in.ToolChoice
		choice.AllowedTools = cloneSlice(in.ToolChoice.AllowedTools)
		out.ToolChoice = &choice
	}
	if in.TextFormat != nil {
		format := *in.TextFormat
		if in.TextFormat.Strict != nil {
			value := *in.TextFormat.Strict
			format.Strict = &value
		}
		format.Schema = cloneAnyMap(in.TextFormat.Schema)
		out.TextFormat = &format
	}
	out.Metadata = cloneAnyMap(in.Metadata)
	return out
}

func cloneFloat(in *float64) *float64 {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func cloneBool(in *bool) *bool {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func cloneString(in *string) *string {
	if in == nil {
		return nil
	}
	value := *in
	return &value
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		if typed == nil {
			return []any(nil)
		}
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneAny(item)
		}
		return out
	case []string:
		return cloneSlice(typed)
	default:
		return value
	}
}

func cloneSlice[T any](in []T) []T {
	if in == nil {
		return nil
	}
	out := make([]T, len(in))
	copy(out, in)
	return out
}
