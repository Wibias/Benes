package cursor

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

var (
	errProtoTruncated = errors.New("Cursor protobuf payload is truncated")
	errProtoWire      = errors.New("Cursor protobuf wire type is unsupported")
)

func EncodeRunPayload(req RunRequest) []byte {
	return EncodeProtoMessage(1, encodeRunRequest(req))
}

func encodeRunRequest(req RunRequest) []byte {
	var out []byte
	out = append(out, EncodeProtoMessage(1, encodeConversationState(req))...)
	out = append(out, EncodeProtoMessage(2, encodeConversationAction(req))...)
	out = append(out, EncodeProtoMessage(3, EncodeProtoString(1, req.Model))...)
	return out
}

func encodeConversationAction(req RunRequest) []byte {
	if lastTurnIsTool(req.Turns) {
		return EncodeProtoMessage(2, nil)
	}
	return EncodeProtoMessage(1, encodeUserMessageAction(req))
}

func encodeUserMessageAction(req RunRequest) []byte {
	return EncodeProtoMessage(1, encodeUserMessage(req))
}

func encodeUserMessage(req RunRequest) []byte {
	text := activeUserText(req)
	out := EncodeProtoString(1, text)
	out = append(out, EncodeProtoString(2, messageID(text, req.Digest))...)
	if images := encodeSelectedContext(req.Images); len(images) > 0 {
		out = append(out, EncodeProtoMessage(3, images)...)
	}
	return out
}

func encodeSelectedContext(images []string) []byte {
	var out []byte
	for _, raw := range images {
		img, err := EncodeSelectedImage(raw)
		if err != nil {
			continue
		}
		out = append(out, EncodeProtoMessage(1, img)...)
	}
	return out
}

func EncodeSelectedImage(dataURL string) ([]byte, error) {
	mime, data, err := splitDataURL(dataURL)
	if err != nil {
		return nil, err
	}
	out := EncodeProtoString(2, messageID(dataURL, "image"))
	if mime != "" {
		out = append(out, EncodeProtoString(7, mime)...)
	}
	out = append(out, EncodeProtoBytes(8, data)...)
	return out, nil
}

func splitDataURL(raw string) (string, []byte, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "data:") {
		return "", nil, fmt.Errorf("Cursor vision accepts only data: image parts")
	}
	meta, rest, ok := strings.Cut(strings.TrimPrefix(raw, "data:"), ",")
	if !ok {
		return "", nil, fmt.Errorf("Cursor vision data URL is incomplete")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(rest)
	if err != nil {
		return "", nil, fmt.Errorf("Cursor vision payload is not strict base64")
	}
	mime, _, _ := strings.Cut(meta, ";")
	return mime, data, nil
}

func activeUserText(req RunRequest) string {
	for i := len(req.Turns) - 1; i >= 0; i-- {
		if req.Turns[i].Role == "user" {
			return req.Turns[i].Text
		}
	}
	return ""
}

func messageID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:8])
}

func DecodeServerFrame(raw []byte) (protocol.Event, bool) {
	root, err := decodeProtoFields(raw)
	if err != nil {
		return protocol.Event{}, false
	}
	if update := fieldBytes(root, 1); len(update) > 0 {
		inner, err := decodeProtoFields(update)
		if err != nil {
			return protocol.Event{}, false
		}
		if delta := fieldBytes(inner, 1); len(delta) > 0 {
			textFields, err := decodeProtoFields(delta)
			if err != nil {
				return protocol.Event{}, false
			}
			if text := fieldBytes(textFields, 1); len(text) > 0 {
				return protocol.Event{Type: protocol.EventTextDelta, Text: string(text)}, true
			}
		}
		if token := fieldBytes(inner, 8); len(token) > 0 {
			tokenFields, err := decodeProtoFields(token)
			if err != nil {
				return protocol.Event{}, false
			}
			if tokens, ok := fieldVarint(tokenFields, 1); ok {
				usage := NormalizeCursorUsage(0, 0, int64(tokens), 0)
				return protocol.Event{Type: protocol.EventDone, Usage: &usage}, true
			}
		}
	}
	return protocol.Event{}, false
}
