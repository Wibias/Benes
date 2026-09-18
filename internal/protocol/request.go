package protocol

import "encoding/json"

type RequestSource string

const (
	RequestSourceResponses         RequestSource = "responses"
	RequestSourceChatCompletions   RequestSource = "chat_completions"
	RequestSourceAnthropicMessages RequestSource = "anthropic_messages"
)

type ToolChoiceKind string

const (
	ToolChoiceAuto     ToolChoiceKind = "auto"
	ToolChoiceNone     ToolChoiceKind = "none"
	ToolChoiceRequired ToolChoiceKind = "required"
	ToolChoiceNamed    ToolChoiceKind = "named"
	ToolChoiceAllowed  ToolChoiceKind = "allowed"
)

type ToolChoiceMode string

const (
	ToolChoiceModeAuto     ToolChoiceMode = "auto"
	ToolChoiceModeRequired ToolChoiceMode = "required"
)

type ToolChoice struct {
	Kind         ToolChoiceKind
	Name         string
	AllowedTools []string
	AllowedMode  ToolChoiceMode
}

type HostedWebSearchKind string

const (
	HostedWebSearchWebSearch HostedWebSearchKind = "web_search"
	HostedWebSearchPreview   HostedWebSearchKind = "web_search_preview"
)

type HostedWebSearchLocation struct {
	Type     string
	Country  string
	City     string
	Region   string
	Timezone string
}

type HostedWebSearchFilters struct {
	AllowedDomains []string
}

type HostedWebSearchTool struct {
	Type               HostedWebSearchKind
	SearchContextSize  string
	SearchContentTypes []string
	IndexedWebAccess   *bool
	ExternalWebAccess  *bool
	UserLocation       *HostedWebSearchLocation
	Filters            *HostedWebSearchFilters
}

type TextFormat struct {
	Type        string
	Name        string
	Description string
	Schema      map[string]any
	Strict      *bool
}

type RequestOptions struct {
	MaxOutputTokens     *float64
	Temperature         *float64
	TopP                *float64
	StopSequences       []string
	ToolChoice          *ToolChoice
	ParallelToolCalls   *bool
	Reasoning           string
	HideThinkingSummary bool
	ServiceTier         *string
	PresencePenalty     *float64
	FrequencyPenalty    *float64
	PromptCacheKey      *string
	TextFormat          *TextFormat
	User                *string
	Metadata            map[string]any
	Store               *bool
}

type ParsedRequest struct {
	Source               RequestSource
	ModelID              string
	UpstreamModelID      string
	PreviousResponseID   string
	ReplayPrefixLength   int
	Context              Context
	HostedWebSearchTools []HostedWebSearchTool
	Stream               bool
	Options              RequestOptions
	Raw                  json.RawMessage
	StructuredOutput     bool
	CompactionRequest    bool
}
