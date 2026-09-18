package antigravity

import (
	"bytes"
	"encoding/json"
	"strings"
)

type parsedFrame struct {
	kind      FailoverClass
	text      string
	thought   string
	callID    string
	tool      string
	args      json.RawMessage
	signature string
	errKind   FailureKind
	errText   string
	done      bool
}

func parseCCAFrame(payload string) parsedFrame {
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "{}" {
		return parsedFrame{kind: FailoverEmpty}
	}
	if payload == "[DONE]" {
		return parsedFrame{done: true}
	}
	var root map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &root) != nil {
		return parsedFrame{}
	}
	if raw, ok := root["error"]; ok && len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		kind := ClassifyStatus(0, string(raw), false)
		class := FailoverClass("")
		lower := strings.ToLower(string(raw))
		switch {
		case strings.Contains(lower, "unavailable") || strings.Contains(lower, `"code":14`):
			class = FailoverUnavailable
		case strings.Contains(lower, "not_found") || strings.Contains(lower, `"code":5`):
			class = Failover404
		}
		return parsedFrame{kind: class, errKind: kind, errText: string(raw)}
	}
	if wrapped, ok := root["response"]; ok && len(bytes.TrimSpace(wrapped)) > 0 {
		var inner map[string]json.RawMessage
		if json.Unmarshal(wrapped, &inner) == nil {
			root = inner
		}
	}
	if raw, ok := root["error"]; ok && len(bytes.TrimSpace(raw)) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return parsedFrame{errKind: ClassifyStatus(0, string(raw), false), errText: string(raw)}
	}
	if text := firstText(root); text != "" {
		return parsedFrame{text: text}
	}
	var candidates []struct {
		Content struct {
			Parts []struct {
				Text             string          `json:"text"`
				Thought          bool            `json:"thought"`
				ThoughtSignature string          `json:"thoughtSignature"`
				FunctionCall     json.RawMessage `json:"functionCall"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	}
	if raw, ok := root["candidates"]; ok {
		if json.Unmarshal(raw, &candidates) != nil {
			return parsedFrame{}
		}
	}
	if len(candidates) == 0 {
		if _, ok := root["usageMetadata"]; ok {
			return parsedFrame{done: true}
		}
		return parsedFrame{}
	}
	frame := parsedFrame{}
	for _, part := range candidates[0].Content.Parts {
		if len(part.FunctionCall) > 0 && !bytes.Equal(bytes.TrimSpace(part.FunctionCall), []byte("null")) {
			var call struct {
				ID   string          `json:"id"`
				Name string          `json:"name"`
				Args json.RawMessage `json:"args"`
			}
			_ = json.Unmarshal(part.FunctionCall, &call)
			frame.callID = call.ID
			frame.tool = call.Name
			frame.args = call.Args
			if strings.TrimSpace(part.ThoughtSignature) != "" {
				frame.signature = part.ThoughtSignature
			}
		}
		if part.Text == "" {
			continue
		}
		if part.Thought {
			frame.thought += part.Text
			continue
		}
		frame.text += part.Text
	}
	if candidates[0].FinishReason != "" && frame.text == "" && frame.thought == "" && frame.tool == "" {
		frame.done = true
	}
	return frame
}
