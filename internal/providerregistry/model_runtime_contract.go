package providerregistry

const (
	APITypeChatCompletions   = "chat_completions"
	APITypeResponses         = "responses"
	APITypeAnthropicMessages = "anthropic_messages"
)

type RuntimeContract struct {
	APITypes          []string
	SupportsToolUse   *bool
	SupportsStreaming *bool
}

func ModelRuntimeContract(protocol Protocol) (RuntimeContract, bool) {
	apiTypes := []string{APITypeChatCompletions, APITypeResponses, APITypeAnthropicMessages}
	switch protocol {
	case ProtocolOpenAIResponses,
		ProtocolOpenAIChat,
		ProtocolAnthropicMessages,
		ProtocolGoogleAntigravity,
		ProtocolKiro,
		ProtocolGoogle,
		ProtocolGoogleVertex:
		// These adapters can translate all three canonical inbound shapes, but
		// adapter representability alone does not prove that every model behind
		// an arbitrary configured destination supports tools or streaming.
		return RuntimeContract{APITypes: append([]string(nil), apiTypes...)}, true
	case ProtocolCursor:
		// Cursor's current canonical adapter preserves tool-call/result history,
		// but it does not serialize the current turn's client tool declarations.
		// This is negative runtime evidence and therefore safe to advertise.
		toolUse := false
		return RuntimeContract{
			APITypes:        append([]string(nil), apiTypes...),
			SupportsToolUse: &toolUse,
		}, true
	default:
		return RuntimeContract{}, false
	}
}
