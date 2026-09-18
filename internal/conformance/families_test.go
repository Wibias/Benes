package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	anthropicout "github.com/Wibias/Benes/internal/anthropic/outbound"
	anthropicreq "github.com/Wibias/Benes/internal/anthropic/request"
	chatreq "github.com/Wibias/Benes/internal/chat/request"
	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/providers/google"
	"github.com/Wibias/Benes/internal/providers/kiro"
	"github.com/Wibias/Benes/internal/providers/openaichat"
	"github.com/Wibias/Benes/internal/providers/openairesponses"
	"github.com/Wibias/Benes/internal/responses/parsed"
	requestwire "github.com/Wibias/Benes/internal/responses/request"
)

const maxFixtureBytes = 1 << 20

func parseResponses(t *testing.T, inbound string, upstream string) protocol.ParsedRequest {
	t.Helper()
	wire, err := requestwire.Decode(strings.NewReader(inbound), maxFixtureBytes)
	if err != nil {
		t.Fatalf("responses Decode: %v", err)
	}
	req, err := parsed.Build(wire, 1)
	if err != nil {
		t.Fatalf("responses Build: %v", err)
	}
	req.UpstreamModelID = upstream
	return req
}

func parseChat(t *testing.T, inbound string, upstream string) protocol.ParsedRequest {
	t.Helper()
	req, err := chatreq.Decode(strings.NewReader(inbound), maxFixtureBytes, chatreq.DecodeOptions{NowMillis: 1, IDGenerator: func() string { return "call_1" }})
	if err != nil {
		t.Fatalf("chat Decode: %v", err)
	}
	req.UpstreamModelID = upstream
	return req
}

func parseAnthropic(t *testing.T, inbound string, upstream string) protocol.ParsedRequest {
	t.Helper()
	req, err := anthropicreq.Decode(strings.NewReader(inbound), maxFixtureBytes, anthropicreq.DecodeOptions{NowMillis: 1})
	if err != nil {
		t.Fatalf("anthropic Decode: %v", err)
	}
	req.UpstreamModelID = upstream
	return req
}

func openResponses(t *testing.T, parsed protocol.ParsedRequest, stream []byte) (capturedRequest, []protocol.Event, error) {
	t.Helper()
	trip, client := newCapture("text/event-stream", stream)
	adapter, err := openairesponses.New(openairesponses.Config{
		Endpoint:   "https://api.openai.com/v1/responses",
		APIKey:     "conformance-key",
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("openairesponses.New: %v", err)
	}
	streamOut, err := adapter.Open(context.Background(), providers.DispatchRequest{Parsed: parsed})
	if err != nil {
		return trip.captured(), nil, err
	}
	events, err := collectEvents(streamOut)
	return trip.captured(), events, err
}

func openChat(t *testing.T, parsed protocol.ParsedRequest, stream []byte) (capturedRequest, []protocol.Event, error) {
	t.Helper()
	trip, client := newCapture("text/event-stream", stream)
	adapter, err := openaichat.New(openaichat.Config{
		Endpoint:   "https://api.openai.com/v1/chat/completions",
		APIKey:     "conformance-key",
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("openaichat.New: %v", err)
	}
	streamOut, err := adapter.Open(context.Background(), providers.DispatchRequest{Parsed: parsed})
	if err != nil {
		return trip.captured(), nil, err
	}
	events, err := collectEvents(streamOut)
	return trip.captured(), events, err
}

func openGoogle(t *testing.T, parsed protocol.ParsedRequest, stream []byte) (capturedRequest, []protocol.Event, error) {
	t.Helper()
	trip, client := newCapture("text/event-stream", stream)
	adapter, err := google.New(context.Background(), google.Config{
		APIKey:     "conformance-key",
		HTTPClient: client,
		Endpoint:   "https://generativelanguage.googleapis.com",
	})
	if err != nil {
		t.Fatalf("google.New: %v", err)
	}
	streamOut, err := adapter.Open(context.Background(), providers.DispatchRequest{Parsed: parsed})
	if err != nil {
		return trip.captured(), nil, err
	}
	events, err := collectEvents(streamOut)
	return trip.captured(), events, err
}

func openKiro(t *testing.T, parsed protocol.ParsedRequest, stream []byte) (capturedRequest, []protocol.Event, error) {
	t.Helper()
	trip, client := newCapture("application/vnd.amazon.eventstream", stream)
	adapter, err := kiro.NewHardened(context.Background(), kiro.Config{
		Account: kiro.AccountSnapshot{
			AccessToken: "conformance-token",
			ProfileARN:  "arn:aws:codewhisperer:us-east-1:123456789012:profile/abc",
			APIRegion:   "us-east-1",
		},
		HTTPClient: client,
		Endpoint:   "https://runtime.us-east-1.kiro.dev",
	})
	if err != nil {
		t.Fatalf("kiro.NewHardened: %v", err)
	}
	streamOut, err := adapter.Open(context.Background(), providers.DispatchRequest{Parsed: parsed})
	if err != nil {
		return trip.captured(), nil, err
	}
	events, err := collectEvents(streamOut)
	return trip.captured(), events, err
}

func encodeAnthropic(t *testing.T, model string, events []protocol.Event) []anthropicout.Frame {
	t.Helper()
	enc, err := anthropicout.New(model, anthropicout.Options{
		IDGenerator:        func() string { return "msg_conformance" },
		SignatureGenerator: func() string { return "sig_conformance" },
	})
	if err != nil {
		t.Fatalf("anthropic outbound New: %v", err)
	}
	var frames []anthropicout.Frame
	for _, ev := range events {
		got, err := enc.Handle(ev)
		if err != nil {
			t.Fatalf("anthropic outbound Handle(%s): %v", ev.Type, err)
		}
		frames = append(frames, got...)
	}
	return frames
}

func googleParsed(user string, tools []protocol.Tool, extra ...protocol.Message) protocol.ParsedRequest {
	msgs := []protocol.Message{{
		Role:    protocol.RoleUser,
		Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: user}},
	}}
	msgs = append(msgs, extra...)
	return protocol.ParsedRequest{
		ModelID:         "gemini-3.7-flash",
		UpstreamModelID: "gemini-3.7-flash",
		Context:         protocol.Context{Messages: msgs, Tools: tools},
	}
}

func kiroParsed(user string, tools []protocol.Tool, extra ...protocol.Message) protocol.ParsedRequest {
	msgs := []protocol.Message{{
		Role:    protocol.RoleUser,
		Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: user}},
	}}
	msgs = append(msgs, extra...)
	return protocol.ParsedRequest{
		ModelID:         "claude-sonnet-4",
		UpstreamModelID: "claude-sonnet-4",
		Context:         protocol.Context{Messages: msgs, Tools: tools},
	}
}

func compileResponses(t *testing.T, parsed protocol.ParsedRequest) []byte {
	t.Helper()
	body, err := openairesponses.Compile(parsed)
	if err != nil {
		t.Fatalf("openairesponses.Compile: %v", err)
	}
	return body
}

func mutateDropKey(raw []byte, key string) []byte {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return raw
	}
	delete(body, key)
	out, err := json.Marshal(body)
	if err != nil {
		return raw
	}
	return out
}

func mutateReplaceText(raw []byte, old, next string) []byte {
	return bytes.Replace(raw, []byte(old), []byte(next), 1)
}
