package antigravity

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

const (
	EnvelopeUserAgent = "antigravity"
	RequestUserAgent  = "antigravity/ide/1.0.0 (aidev_client; os_type=windows; arch=amd64)"
	emptyPlaceholder  = "(empty)"
)

type Envelope struct {
	Model       string         `json:"model"`
	UserAgent   string         `json:"userAgent"`
	RequestType string         `json:"requestType"`
	Project     string         `json:"project"`
	RequestID   string         `json:"requestId"`
	Request     map[string]any `json:"request"`
}

func CompileDispatchEnvelope(parsed protocol.ParsedRequest, account Account, image bool) (Envelope, error) {
	model := strings.TrimSpace(parsed.UpstreamModelID)
	if model == "" {
		model = strings.TrimSpace(parsed.ModelID)
	}
	if strings.TrimSpace(account.ProjectID) == "" {
		return Envelope{}, fmt.Errorf("Cloud Code Assist project id is required")
	}
	claude := strings.Contains(strings.ToLower(model), "claude")
	turns := RepairClaudeTurns(parsed.Context.Messages)
	if ContinuationShape(turns) == "continue" {
		turns = append(turns, ClaudeTurn{Role: "user", Text: "continue"})
	}
	contents := turnsToContents(turns)
	request := map[string]any{
		"contents":  contents,
		"sessionId": sessionID(parsed),
	}
	if sys := systemInstruction(parsed.Context.SystemPrompt); sys != nil {
		request["systemInstruction"] = sys
	}
	if UseReplacementPreamble(claude) {
		request["preamble"] = true
	}
	if claude {
		if parsed.Options.ToolChoice != nil && parsed.Options.ToolChoice.Kind == protocol.ToolChoiceNone {
			delete(request, "tools")
			delete(request, "toolConfig")
		} else {
			request["toolConfig"] = map[string]any{
				"functionCallingConfig": map[string]any{"mode": "VALIDATED"},
			}
		}
		request["beta"] = InterleavedThinkingBeta
	}
	if image {
		request["generationConfig"] = map[string]any{
			"responseModalities": []string{"TEXT", "IMAGE"},
		}
	}
	if tools := geminiTools(parsed); len(tools) > 0 {
		request["tools"] = tools
	}
	return Envelope{
		Model:       model,
		UserAgent:   EnvelopeUserAgent,
		RequestType: "agent",
		Project:     account.ProjectID,
		RequestID:   "agent-" + newRequestID(),
		Request:     request,
	}, nil
}

func turnsToContents(turns []ClaudeTurn) []map[string]any {
	out := make([]map[string]any, 0, len(turns))
	for _, turn := range turns {
		role := "user"
		if turn.Role == "assistant" {
			role = "model"
		}
		parts := make([]map[string]any, 0, 2)
		if text := strings.TrimSpace(turn.Text); text != "" {
			parts = append(parts, map[string]any{"text": text})
		}
		if turn.HasCall {
			call := map[string]any{"name": turn.Tool, "args": map[string]any{}}
			if turn.CallID != "" {
				call["id"] = turn.CallID
			}
			item := map[string]any{"functionCall": call}
			if sig := strings.TrimSpace(turn.Signature); sig != "" {
				item["thoughtSignature"] = sig
			}
			parts = append(parts, item)
		}
		if len(parts) > 0 {
			out = append(out, map[string]any{"role": role, "parts": parts})
		}
		if turn.HasOut {
			result := turn.Result
			if result == "" {
				result = "(empty tool output)"
			}
			resp := map[string]any{"name": turn.Tool, "response": map[string]any{"result": result}}
			if turn.CallID != "" {
				resp["id"] = turn.CallID
			}
			out = append(out, map[string]any{
				"role":  "user",
				"parts": []map[string]any{{"functionResponse": resp}},
			})
			continue
		}
		if len(parts) == 0 {
			out = append(out, map[string]any{"role": role, "parts": []map[string]any{{"text": emptyPlaceholder}}})
		}
	}
	return out
}

func systemInstruction(lines []string) map[string]any {
	var b strings.Builder
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(line)
	}
	if b.Len() == 0 {
		return nil
	}
	return map[string]any{"parts": []map[string]any{{"text": b.String()}}}
}

func geminiTools(parsed protocol.ParsedRequest) []map[string]any {
	if len(parsed.Context.Tools) == 0 {
		return nil
	}
	if parsed.Options.ToolChoice != nil && parsed.Options.ToolChoice.Kind == protocol.ToolChoiceNone {
		return nil
	}
	decls := make([]map[string]any, 0, len(parsed.Context.Tools))
	for _, tool := range parsed.Context.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		decls = append(decls, map[string]any{
			"name":        name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
		})
	}
	if len(decls) == 0 {
		return nil
	}
	return []map[string]any{{"functionDeclarations": decls}}
}

func sessionID(parsed protocol.ParsedRequest) string {
	for _, message := range parsed.Context.Messages {
		if message.Role != protocol.RoleUser {
			continue
		}
		text := joinText(message.Content)
		if text == "" {
			continue
		}
		sum := sha256.Sum256([]byte(text))
		masked := binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff
		return fmt.Sprintf("-%d", masked)
	}
	return "-" + newRequestID()
}

func newRequestID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "0"
	}
	return hex.EncodeToString(raw)
}

func encodeEnvelope(env Envelope) ([]byte, error) {
	return json.Marshal(env)
}
