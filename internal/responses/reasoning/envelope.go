package reasoning

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"

	"github.com/Wibias/Benes/internal/protocol"
)

const Prefix = "benesr1:"

type Envelope struct {
	Signature    string   `json:"sig,omitempty"`
	Redacted     []string `json:"red,omitempty"`
	Text         string   `json:"txt,omitempty"`
	KiroRedacted string                       `json:"krc,omitempty"`
	KiroKind     protocol.KiroReasoningMember `json:"krk,omitempty"`
}

func Encode(envelope Envelope) (string, error) {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return "", err
	}
	return Prefix + base64.StdEncoding.EncodeToString(payload), nil
}

func Decode(encryptedContent string) (Envelope, bool) {
	if !strings.HasPrefix(encryptedContent, Prefix) {
		return Envelope{}, false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encryptedContent, Prefix))
	if err != nil {
		return Envelope{}, false
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return Envelope{}, false
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return Envelope{}, false
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return Envelope{}, false
	}

	envelope := Envelope{}
	if sig, ok := obj["sig"].(string); ok {
		envelope.Signature = sig
	}
	if red, ok := obj["red"].([]any); ok {
		for _, entry := range red {
			if text, ok := entry.(string); ok {
				envelope.Redacted = append(envelope.Redacted, text)
			}
		}
	}
	if text, ok := obj["txt"].(string); ok && text != "" {
		envelope.Text = text
	}
	if krcRaw, exists := obj["krc"]; exists {
		krc, ok := krcRaw.(string)
		if !ok || krc == "" {
			return Envelope{}, false
		}
		envelope.KiroRedacted = krc
	}
	if kindRaw, exists := obj["krk"]; exists {
		kind, ok := kindRaw.(string)
		if !ok || kind == "" {
			return Envelope{}, false
		}
		envelope.KiroKind = protocol.KiroReasoningMember(kind)
	}
	if envelope.KiroRedacted != "" {
		if envelope.KiroKind == "" {
			envelope.KiroKind = protocol.KiroReasoningRedactedContent
		}
		if !envelope.KiroKind.Valid() {
			return Envelope{}, false
		}
	} else if envelope.KiroKind != "" {
		return Envelope{}, false
	}

	if envelope.Signature == "" && len(envelope.Redacted) == 0 && envelope.Text == "" && envelope.KiroRedacted == "" {
		return Envelope{}, false
	}
	return envelope, true
}
