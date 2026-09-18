package cursor

import (
	"crypto/sha256"
	"encoding/json"
)

func RootPromptBlobs(req RunRequest) [][]byte {
	out := make([][]byte, 0, len(req.System)+len(req.Turns))
	system := req.System
	if len(system) == 0 {
		system = []string{"You are a helpful assistant."}
	}
	for _, prompt := range system {
		out = append(out, mustJSON(map[string]any{"role": "system", "content": prompt}))
	}
	lastUser := -1
	for i := len(req.Turns) - 1; i >= 0; i-- {
		if req.Turns[i].Role == "user" {
			lastUser = i
			break
		}
	}
	for i, turn := range req.Turns {
		if i == lastUser {
			continue
		}
		switch turn.Role {
		case "user":
			out = append(out, mustJSON(map[string]any{
				"role":    "user",
				"content": []map[string]any{{"type": "text", "text": turn.Text}},
			}))
		case "assistant":
			out = append(out, mustJSON(map[string]any{
				"role":    "assistant",
				"content": []map[string]any{{"type": "text", "text": turn.Text}},
			}))
		case "tool":
			out = append(out, mustJSON(map[string]any{
				"role":    "user",
				"content": []map[string]any{{"type": "text", "text": toolResultHistoryText(turn)}},
			}))
		case "thinking":
			if turn.Text == "" {
				continue
			}
			out = append(out, mustJSON(map[string]any{
				"role":    "assistant",
				"content": []map[string]any{{"type": "text", "text": turn.Text}},
			}))
		}
	}
	return out
}

func BlobID(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

func encodeConversationState(req RunRequest) []byte {
	var out []byte
	for _, blob := range RootPromptBlobs(req) {
		out = append(out, EncodeProtoBytes(1, BlobID(blob))...)
	}
	for _, blob := range ConversationTurnBlobs(req) {
		out = append(out, EncodeProtoBytes(8, BlobID(blob))...)
	}
	return out
}

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return raw
}
