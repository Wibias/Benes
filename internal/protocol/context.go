package protocol

type MessageRole string

const (
	RoleUser       MessageRole = "user"
	RoleDeveloper  MessageRole = "developer"
	RoleAssistant  MessageRole = "assistant"
	RoleToolResult MessageRole = "toolResult"
)

type ContentType string

const (
	ContentText     ContentType = "text"
	ContentImage    ContentType = "image"
	ContentThinking ContentType = "thinking"
	ContentToolCall ContentType = "toolCall"
)

type GoogleOpaqueMetadata struct {
	ThoughtSignature string
}

type MiniMaxReasoningDetail struct {
	Type   string `json:"type,omitempty"`
	ID     string `json:"id,omitempty"`
	Format string `json:"format,omitempty"`
	Index  *int   `json:"index,omitempty"`
	Text   string `json:"text,omitempty"`
}

type MiniMaxOpaqueMetadata struct {
	ReasoningDetails []MiniMaxReasoningDetail
}

type ProviderOpaqueMetadata struct {
	Google  *GoogleOpaqueMetadata
	MiniMax *MiniMaxOpaqueMetadata
}

type ContentPart struct {
	Type             ContentType
	Text             string
	ImageURL         string
	Detail           string
	Thinking         string
	Signature        string
	ItemID           string
	Redacted         []string
	ToolCallID       string
	ToolName         string
	ToolNamespace    string
	Arguments        map[string]any
	CustomWireName   string
	ThoughtSignature string
	ProviderMetadata *ProviderOpaqueMetadata
}

type Message struct {
	Role                     MessageRole
	Content                  []ContentPart
	Phase                    *MessagePhase
	Model                    string
	Timestamp                int64
	KiroRedactedReasoning    string
	ToolCallID               string
	ToolName                 string
	ToolNamespace            string
	ContainsEncryptedContent bool
	IsError                  bool
}

type Tool struct {
	Name                  string
	Description           string
	Parameters            map[string]any
	Strict                *bool
	Namespace             string
	Freeform              bool
	ToolSearch            bool
	LoadedFromToolSearch  bool
	HostedWebSearch       bool
	SidecarWebSearch      bool
	SidecarWebSearchKinds []HostedWebSearchKind
}

type Context struct {
	SystemPrompt []string
	Messages     []Message
	Tools        []Tool
}
