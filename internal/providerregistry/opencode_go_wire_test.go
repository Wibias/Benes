package providerregistry

import "testing"

func TestOpenCodeGoProtocolUsesCurrentDocumentedWireMatrix(t *testing.T) {
	tests := []struct {
		model string
		want  Protocol
	}{
		// Responses.
		{model: "grok-4.6", want: ProtocolOpenAIResponses},
		{model: "gpt-5.6-luna", want: ProtocolOpenAIResponses},
		{model: "muse-spark-1.3-contributor", want: ProtocolOpenAIResponses},
		{model: "muse-spark-1.2-contributor", want: ProtocolOpenAIResponses},

		// Chat Completions.
		{model: "glm-5.3-flash", want: ProtocolOpenAIChat},
		{model: "glm-5.3", want: ProtocolOpenAIChat},
		{model: "glm-5.2", want: ProtocolOpenAIChat},
		{model: "glm-5.1", want: ProtocolOpenAIChat},
		{model: "kimi-k3", want: ProtocolOpenAIChat},
		{model: "kimi-k2.7-code", want: ProtocolOpenAIChat},
		{model: "kimi-k2.6", want: ProtocolOpenAIChat},
		{model: "longcat-2.0", want: ProtocolOpenAIChat},
		{model: "deepseek-v4-pro", want: ProtocolOpenAIChat},
		{model: "deepseek-v4-flash", want: ProtocolOpenAIChat},
		{model: "deepseek-v4-flash-vision-exp", want: ProtocolOpenAIChat},
		{model: "mimo-v2.5", want: ProtocolOpenAIChat},
		{model: "mimo-v2.5-pro", want: ProtocolOpenAIChat},
		{model: "hy4-preview", want: ProtocolOpenAIChat},
		{model: "hy3", want: ProtocolOpenAIChat},
		{model: "omen-alpha", want: ProtocolOpenAIChat},

		// Anthropic Messages.
		{model: "minimax-m3", want: ProtocolAnthropicMessages},
		{model: "minimax-m2.7", want: ProtocolAnthropicMessages},
		{model: "minimax-m2.5", want: ProtocolAnthropicMessages},
		{model: "qwen3.8-max", want: ProtocolAnthropicMessages},
		{model: "qwen3.8-flash", want: ProtocolAnthropicMessages},
		{model: "qwen3.7-max", want: ProtocolAnthropicMessages},
		{model: "qwen3.7-plus", want: ProtocolAnthropicMessages},
		{model: "qwen3.6-plus", want: ProtocolAnthropicMessages},

		// Explicit deterministic controls.
		{model: "grok-4.5", want: ProtocolOpenAIChat},
		{model: " grok-4.6 ", want: ProtocolOpenAIResponses},
		{model: " future-model ", want: ProtocolOpenAIChat},
		{model: "Grok-4.6", want: ProtocolOpenAIChat},
		{model: "", want: ProtocolOpenAIChat},
	}
	for _, test := range tests {
		t.Run(test.model, func(t *testing.T) {
			if got := openCodeGoProtocol(test.model); got != test.want {
				t.Fatalf("openCodeGoProtocol(%q)=%q want %q", test.model, got, test.want)
			}
		})
	}
}
