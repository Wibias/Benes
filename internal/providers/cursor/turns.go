package cursor

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
)

const cursorToolProvider = "benes-responses"

func ConversationTurnBlobs(req RunRequest) [][]byte {
	turns, _ := encodeConversationTurns(req)
	return turns
}

func flattenConversationBlobs(req RunRequest) [][]byte {
	_, all := encodeConversationTurns(req)
	return all
}

func encodeConversationTurns(req RunRequest) (turns [][]byte, all [][]byte) {
	end := historyEnd(req.Turns)
	var current *pendingTurn
	var pending []RunTurn
	flush := func() {
		if current == nil {
			return
		}
		for _, call := range pending {
			step := encodeToolCallStep(call, RunTurn{})
			current.steps = append(current.steps, step)
			all = append(all, step)
		}
		pending = pending[:0]
		turn := encodeTurnStructure(current.user, current.steps)
		turns = append(turns, turn)
		all = append(all, current.user)
		all = append(all, current.steps...)
		all = append(all, turn)
		current = nil
	}
	for i, turn := range req.Turns[:end] {
		switch turn.Role {
		case "checkpoint":
			flush()
			if turn.Text == "" {
				continue
			}
			blob := []byte(turn.Text)
			turns = append(turns, blob)
			all = append(all, blob)
		case "user":
			flush()
			user := encodeTurnUserMessage(turn.Text, req.Digest, i)
			current = &pendingTurn{user: user}
		case "assistant":
			if current == nil || turn.Text == "" {
				continue
			}
			step := encodeAssistantStep(turn.Text)
			current.steps = append(current.steps, step)
		case "thinking":
			if current == nil || turn.Text == "" {
				continue
			}
			step := encodeThinkingStep(turn.Text)
			current.steps = append(current.steps, step)
		case "toolCall":
			if current == nil {
				continue
			}
			pending = append(pending, turn)
		case "tool":
			if current == nil {
				continue
			}
			if idx := matchPendingTool(pending, turn); idx >= 0 {
				step := encodeToolCallStep(pending[idx], turn)
				current.steps = append(current.steps, step)
				pending = append(pending[:idx], pending[idx+1:]...)
			} else {
				step := encodeAssistantStep(toolResultHistoryText(turn))
				current.steps = append(current.steps, step)
			}
		}
	}
	flush()
	return turns, all
}

func matchPendingTool(pending []RunTurn, result RunTurn) int {
	if result.ToolCallID == "" {
		return -1
	}
	found := -1
	for i, call := range pending {
		if !toolIdentityMatches(call, result) {
			continue
		}
		if found >= 0 {
			return -1
		}
		found = i
	}
	return found
}

func toolIdentityMatches(call, result RunTurn) bool {
	if call.ToolCallID == "" || call.ToolCallID != result.ToolCallID {
		return false
	}
	if result.ToolName != "" && result.ToolName != call.ToolName {
		return false
	}
	if len(result.Arguments) > 0 && !reflect.DeepEqual(call.Arguments, result.Arguments) {
		return false
	}
	return true
}

type pendingTurn struct {
	user  []byte
	steps [][]byte
}

func historyEnd(turns []RunTurn) int {
	if lastTurnIsTool(turns) {
		return len(turns)
	}
	idx := lastActionIndex(turns)
	if idx < 0 {
		return 0
	}
	return idx
}

func lastActionIndex(turns []RunTurn) int {
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Role == "user" {
			return i
		}
		if turns[i].Role == "tool" {
			continue
		}
	}
	return -1
}

func lastTurnIsTool(turns []RunTurn) bool {
	return len(turns) > 0 && turns[len(turns)-1].Role == "tool"
}

func encodeTurnUserMessage(text, digest string, index int) []byte {
	out := EncodeProtoString(1, text)
	return append(out, EncodeProtoString(2, messageID(text, digest, strconv.Itoa(index)))...)
}

func encodeAssistantStep(text string) []byte {
	return EncodeProtoMessage(1, EncodeProtoString(1, text))
}

func encodeThinkingStep(text string) []byte {
	return EncodeProtoMessage(3, EncodeProtoString(1, text))
}

func encodeToolCallStep(call, result RunTurn) []byte {
	name := call.ToolName
	if name == "" {
		name = "tool"
	}
	args := EncodeProtoString(1, name)
	keys := make([]string, 0, len(call.Arguments))
	for key := range call.Arguments {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		encoded, err := encodeProtoJSONValue(call.Arguments[key])
		if err != nil {
			continue
		}
		entry := append(EncodeProtoString(1, key), EncodeProtoBytes(2, encoded)...)
		args = append(args, EncodeProtoMessage(2, entry)...)
	}
	args = append(args, EncodeProtoString(3, call.ToolCallID)...)
	args = append(args, EncodeProtoString(4, cursorToolProvider)...)
	args = append(args, EncodeProtoString(5, name)...)
	mcp := EncodeProtoMessage(1, args)
	if result.Role == "tool" {
		item := EncodeProtoMessage(1, EncodeProtoMessage(1, EncodeProtoString(1, result.Text)))
		success := item
		if result.IsError {
			success = append(success, EncodeProtoVarint(2, 1)...)
		}
		mcp = append(mcp, EncodeProtoMessage(2, EncodeProtoMessage(1, success))...)
	}
	return EncodeProtoMessage(2, EncodeProtoMessage(15, mcp))
}

func encodeTurnStructure(user []byte, steps [][]byte) []byte {
	inner := EncodeProtoBytes(1, BlobID(user))
	for _, step := range steps {
		inner = append(inner, EncodeProtoBytes(2, BlobID(step))...)
	}
	return EncodeProtoMessage(1, inner)
}

func encodeProtoJSONValue(value any) ([]byte, error) {
	switch v := value.(type) {
	case nil:
		return EncodeProtoVarint(1, 0), nil
	case bool:
		var n uint64
		if v {
			n = 1
		}
		return EncodeProtoVarint(4, n), nil
	case string:
		return EncodeProtoString(3, v), nil
	case float64:
		return encodeProtoDouble(2, v), nil
	case float32:
		return encodeProtoDouble(2, float64(v)), nil
	case int:
		return encodeProtoDouble(2, float64(v)), nil
	case int64:
		return encodeProtoDouble(2, float64(v)), nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var fields []byte
		for _, key := range keys {
			encoded, err := encodeProtoJSONValue(v[key])
			if err != nil {
				return nil, err
			}
			entry := append(EncodeProtoString(1, key), EncodeProtoMessage(2, encoded)...)
			fields = append(fields, EncodeProtoMessage(1, entry)...)
		}
		return EncodeProtoMessage(5, fields), nil
	case []any:
		var items []byte
		for _, item := range v {
			encoded, err := encodeProtoJSONValue(item)
			if err != nil {
				return nil, err
			}
			items = append(items, EncodeProtoBytes(1, encoded)...)
		}
		return EncodeProtoMessage(6, items), nil
	default:
		return nil, fmt.Errorf("Cursor tool argument type %T is unsupported", value)
	}
}

func encodeProtoDouble(field int, value float64) []byte {
	out := appendVarint(nil, uint64(field<<3|1))
	bits := math.Float64bits(value)
	for i := 0; i < 8; i++ {
		out = append(out, byte(bits))
		bits >>= 8
	}
	return out
}
