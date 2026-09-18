package providerregistry

import "strings"

var openCodeGoModelProtocols = map[string]Protocol{
	// Responses.
	"grok-4.6":                   ProtocolOpenAIResponses,
	"gpt-5.6-luna":               ProtocolOpenAIResponses,
	"muse-spark-1.3-contributor": ProtocolOpenAIResponses,
	"muse-spark-1.2-contributor": ProtocolOpenAIResponses,

	// Chat Completions.
	"glm-5.3-flash":                ProtocolOpenAIChat,
	"glm-5.3":                      ProtocolOpenAIChat,
	"glm-5.2":                      ProtocolOpenAIChat,
	"glm-5.1":                      ProtocolOpenAIChat,
	"kimi-k3":                      ProtocolOpenAIChat,
	"kimi-k2.7-code":               ProtocolOpenAIChat,
	"kimi-k2.6":                    ProtocolOpenAIChat,
	"longcat-2.0":                  ProtocolOpenAIChat,
	"deepseek-v4-pro":              ProtocolOpenAIChat,
	"deepseek-v4-flash":            ProtocolOpenAIChat,
	"deepseek-v4-flash-vision-exp": ProtocolOpenAIChat,
	"mimo-v2.5":                    ProtocolOpenAIChat,
	"mimo-v2.5-pro":                ProtocolOpenAIChat,
	"hy4-preview":                  ProtocolOpenAIChat,
	"hy3":                          ProtocolOpenAIChat,
	"omen-alpha":                   ProtocolOpenAIChat,

	// Anthropic Messages.
	"minimax-m3":    ProtocolAnthropicMessages,
	"minimax-m2.7":  ProtocolAnthropicMessages,
	"minimax-m2.5":  ProtocolAnthropicMessages,
	"qwen3.8-max":   ProtocolAnthropicMessages,
	"qwen3.8-flash": ProtocolAnthropicMessages,
	"qwen3.7-max":   ProtocolAnthropicMessages,
	"qwen3.7-plus":  ProtocolAnthropicMessages,
	"qwen3.6-plus":  ProtocolAnthropicMessages,
}

func openCodeGoProtocol(model string) Protocol {
	if protocol, ok := openCodeGoModelProtocols[strings.TrimSpace(model)]; ok {
		return protocol
	}
	return ProtocolOpenAIChat
}
