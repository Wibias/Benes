package openairesponses

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestPrepareCanonicalBodyChatSourceIgnoresChatRawSemantics(t *testing.T) {
	req := protocol.ParsedRequest{
		Source:          protocol.RequestSourceChatCompletions,
		UpstreamModelID: "gpt-real",
		Stream:          false,
		Raw:             json.RawMessage(`{"model":"raw-chat","messages":[{"role":"user","content":"raw"}],"temperature":99}`),
		Context:         protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "canonical"}}}}},
		Options:         protocol.RequestOptions{Temperature: f64(0.25)},
	}
	body, err := prepareCanonicalBody(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "gpt-real" || got["stream"] != true || got["temperature"] != 0.25 {
		t.Fatalf("body=%#v", got)
	}
	input := got["input"].([]any)
	content := input[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["text"] != "canonical" {
		t.Fatalf("input=%#v", input)
	}
}

func TestPrepareCanonicalBodyResponsesSourceKeepsPreviousResponseID(t *testing.T) {
	req := protocol.ParsedRequest{
		Source:             protocol.RequestSourceResponses,
		UpstreamModelID:    "gpt",
		PreviousResponseID: "resp_1",
		Raw:                json.RawMessage(`{"model":"p/gpt","previous_response_id":"resp_1","input":"hi"}`),
		Context:            protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}}}},
	}
	body, err := prepareCanonicalBody(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["previous_response_id"] != "resp_1" {
		t.Fatalf("previous_response_id=%#v", got["previous_response_id"])
	}
}

func TestPrepareCanonicalBodyResponsesUsesCanonicalWireAfterGuard(t *testing.T) {
	req := protocol.ParsedRequest{
		Source:          protocol.RequestSourceResponses,
		UpstreamModelID: "gpt-canonical",
		Raw:             json.RawMessage(`{"model":"p/raw","store":false,"input":"RAW","future_field":{"x":1}}`),
		Context:         protocol.Context{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "CANONICAL"}}}}},
	}
	body, err := prepareCanonicalBody(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(body, &got)
	if got["model"] != "gpt-canonical" {
		t.Fatalf("model=%#v", got["model"])
	}
	if _, ok := got["future_field"]; ok {
		t.Fatalf("raw future field leaked: %#v", got)
	}
}

func TestPrepareCanonicalBodyRejectsUnknownSource(t *testing.T) {
	req := protocol.ParsedRequest{UpstreamModelID: "gpt"}
	if _, err := prepareCanonicalBody(req); !errors.Is(err, ErrUnsupportedRequestShape) {
		t.Fatalf("err=%v", err)
	}
}

func f64(v float64) *float64 { return &v }
