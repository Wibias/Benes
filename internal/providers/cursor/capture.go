package cursor

import (
	"encoding/json"
	"time"

	"github.com/Wibias/Benes/internal/providers"
)

func CaptureCheckpoint(raw []byte, conversationID, identity, model, digest string) (Checkpoint, bool) {
	return CaptureSafeCheckpoint(raw, conversationID, identity, model, digest, true)
}

func CaptureSafeCheckpoint(raw []byte, conversationID, identity, model, digest string, allowed bool) (Checkpoint, bool) {
	if !allowed {
		return Checkpoint{}, false
	}
	var root struct {
		ConversationCheckpointUpdate json.RawMessage `json:"conversationCheckpointUpdate"`
	}
	if json.Unmarshal(raw, &root) != nil || len(root.ConversationCheckpointUpdate) == 0 {
		return Checkpoint{}, false
	}
	return Checkpoint{
		ConversationID: conversationID,
		Identity:       identity,
		Model:          model,
		PrefixDigest:   digest,
		Bytes:          append([]byte(nil), root.ConversationCheckpointUpdate...),
		StoredAt:       time.Now(),
	}, true
}

func checkpointCaptureAllowed(dispatch providers.DispatchRequest) bool {
	if dispatch.Parsed.Options.Store != nil && !*dispatch.Parsed.Options.Store {
		return false
	}
	if dispatch.ForwardHeaders.Get("x-openai-subagent") != "" {
		return false
	}
	if dispatch.ForwardHeaders.Get("x-codex-parent-thread-id") != "" {
		return false
	}
	return true
}
