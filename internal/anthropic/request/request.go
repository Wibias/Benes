package request

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
)

var (
	ErrTooLarge        = errors.New("anthropic messages request exceeds configured byte limit")
	ErrUnsupportedTool = errors.New("anthropic messages tool is not represented by the canonical tool contract")
)

type DecodeOptions struct {
	NowMillis int64
}

type rec map[string]json.RawMessage

var anthropicEfforts = map[string]struct{}{
	"minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {}, "ultra": {},
}

func Decode(reader io.Reader, maxBytes int64, options DecodeOptions) (protocol.ParsedRequest, error) {
	if reader == nil {
		return protocol.ParsedRequest{}, fmt.Errorf("anthropic request body is required")
	}
	if maxBytes <= 0 {
		maxBytes = 16 << 20
	}
	body, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return protocol.ParsedRequest{}, fmt.Errorf("read Anthropic Messages request: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return protocol.ParsedRequest{}, ErrTooLarge
	}

	root, err := decodeObject(body)
	if err != nil {
		return protocol.ParsedRequest{}, fmt.Errorf("decode Anthropic Messages request: %w", err)
	}
	model, ok := stringField(root, "model")
	if !ok || strings.TrimSpace(model) == "" {
		return protocol.ParsedRequest{}, fmt.Errorf("model is required")
	}
	maxTokens, ok := positiveIntegerField(root, "max_tokens")
	if !ok {
		return protocol.ParsedRequest{}, fmt.Errorf("max_tokens must be a positive integer")
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(root["messages"], &messages); err != nil || len(messages) == 0 {
		return protocol.ParsedRequest{}, fmt.Errorf("messages must be a non-empty array")
	}

	now := options.NowMillis
	if now == 0 {
		now = time.Now().UnixMilli()
	}
	ctx, err := buildContext(root["system"], messages, root["tools"], now)
	if err != nil {
		return protocol.ParsedRequest{}, err
	}
	if len(ctx.Messages) == 0 && len(ctx.SystemPrompt) == 0 {
		return protocol.ParsedRequest{}, fmt.Errorf("messages must include at least one supported turn")
	}

	opts, structured, err := buildOptions(root, maxTokens)
	if err != nil {
		return protocol.ParsedRequest{}, err
	}
	stream, _, err := optionalBoolField(root, "stream")
	if err != nil {
		return protocol.ParsedRequest{}, err
	}
	return protocol.ParsedRequest{
		Source:           protocol.RequestSourceAnthropicMessages,
		ModelID:          model,
		Context:          ctx,
		Stream:           stream,
		Options:          opts,
		Raw:              append(json.RawMessage(nil), body...),
		StructuredOutput: structured,
	}, nil
}

func decodeObject(body []byte) (rec, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var root rec
	if err := decoder.Decode(&root); err != nil || root == nil {
		if err == nil {
			err = fmt.Errorf("request body must be a JSON object")
		}
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return root, nil
}
