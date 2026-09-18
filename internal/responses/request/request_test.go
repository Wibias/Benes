package request

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestDecodeStringInput(t *testing.T) {
	req, err := Decode(strings.NewReader(`{"model":"gpt-5","input":"hello","stream":true}`), 1<<20)
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if req.Model != "gpt-5" || req.Input.Text == nil || *req.Input.Text != "hello" || req.Stream == nil || !*req.Stream {
		t.Fatalf("decoded request = %#v", req)
	}
}

func TestDecodeStructuredInputPreservesUnknownCatchAllItem(t *testing.T) {
	req, err := Decode(strings.NewReader(`{"model":"gpt-5","input":[{"role":"user","content":[{"type":"input_text","text":"hi"}]},{"type":"future_item","vendor":{"x":1}}]}`), 1<<20)
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if len(req.Input.Items) != 2 {
		t.Fatalf("items=%d", len(req.Input.Items))
	}
	if req.Input.Items[1].Type != "future_item" {
		t.Fatalf("type=%q", req.Input.Items[1].Type)
	}
	if !bytes.Contains(req.Input.Items[1].Raw, []byte(`"vendor"`)) {
		t.Fatalf("raw=%s", req.Input.Items[1].Raw)
	}
}

func TestDecodeMatchesZodMinOneWithoutTrimmingModel(t *testing.T) {
	if _, err := Decode(strings.NewReader(`{"model":""}`), 1<<20); err == nil {
		t.Fatal("Decode accepted empty model")
	}
	if _, err := Decode(strings.NewReader(`{"model":" "}`), 1<<20); err != nil {
		t.Fatalf("Decode rejected whitespace model that current schema accepts: %v", err)
	}
}

func TestDecodeRejectsOversizedBodyWithoutPartialRequest(t *testing.T) {
	body := `{"model":"gpt-5","input":"` + strings.Repeat("x", 100) + `"}`
	req, err := Decode(strings.NewReader(body), 32)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err=%v, want ErrTooLarge", err)
	}
	if req != nil {
		t.Fatalf("request=%#v, want nil", req)
	}
}

func TestDecodeRejectsTrailingJSON(t *testing.T) {
	_, err := Decode(strings.NewReader(`{"model":"gpt-5"} {"extra":true}`), 1<<20)
	if err == nil {
		t.Fatal("Decode accepted trailing JSON")
	}
}

func TestDecodeRejectsNullInputButAllowsMissingInput(t *testing.T) {
	if _, err := Decode(strings.NewReader(`{"model":"gpt-5","input":null}`), 1<<20); err == nil {
		t.Fatal("Decode accepted null input")
	}
	if _, err := Decode(strings.NewReader(`{"model":"gpt-5"}`), 1<<20); err != nil {
		t.Fatalf("Decode missing input: %v", err)
	}
}

func TestDecodeInputImageMatchesCurrentSchema(t *testing.T) {
	_, err := Decode(strings.NewReader(`{"model":"gpt-5","input":[{"role":"user","content":[{"type":"input_image","detail":"ultra","file_id":"f"}]}]}`), 1<<20)
	if err == nil || !strings.Contains(err.Error(), "input_image") {
		t.Fatalf("err=%v", err)
	}
	if _, err := Decode(strings.NewReader(`{"model":"gpt-5","input":[{"role":"user","content":[{"type":"input_image","detail":"high","file_id":""}]}]}`), 1<<20); err != nil {
		t.Fatalf("Decode empty string file_id: %v", err)
	}
}

func TestDecodeAllowsOptionalMessageContent(t *testing.T) {
	if _, err := Decode(strings.NewReader(`{"model":"gpt-5","input":[{"role":"user"},{"role":"assistant"},{"role":"system"}]}`), 1<<20); err != nil {
		t.Fatalf("Decode optional content: %v", err)
	}
}

func TestDecodePreservesLooseTypedItemsIncludingMalformedKnownTypes(t *testing.T) {
	body := `{"model":"gpt-5","input":[{"type":"function_call","call_id":"","name":""},{"type":"custom_tool_call_output"}]}`
	req, err := Decode(strings.NewReader(body), 1<<20)
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if len(req.Input.Items) != 2 {
		t.Fatalf("items=%d", len(req.Input.Items))
	}
}

func TestDecodeToolsMatchBuiltinLooseFallback(t *testing.T) {
	if _, err := Decode(strings.NewReader(`{"model":"gpt-5","tools":[{"type":"function","name":""},{"type":"future_tool","x":1}]}`), 1<<20); err != nil {
		t.Fatalf("Decode loose tools: %v", err)
	}
	if _, err := Decode(strings.NewReader(`{"model":"gpt-5","tools":[{"name":"missing-type"}]}`), 1<<20); err == nil {
		t.Fatal("Decode accepted tool without type")
	}
}

func TestDecodeValidatesToolChoiceBecauseItHasNoLooseFallback(t *testing.T) {
	if _, err := Decode(strings.NewReader(`{"model":"gpt-5","tool_choice":{"type":"function","name":""}}`), 1<<20); err == nil {
		t.Fatal("Decode accepted empty named tool choice")
	}
	if _, err := Decode(strings.NewReader(`{"model":"gpt-5","tool_choice":{"type":"allowed_tools","mode":"sometimes","tools":[{"type":"function","name":"x"}]}}`), 1<<20); err == nil {
		t.Fatal("Decode accepted invalid allowed_tools mode")
	}
	if _, err := Decode(strings.NewReader(`{"model":"gpt-5","tool_choice":{"type":"allowed_tools","mode":"required","tools":[{"type":"function","name":"lookup"}]}}`), 1<<20); err != nil {
		t.Fatalf("Decode valid tool choice: %v", err)
	}
}

func TestDecodeValidatesInstructionsStopAndReasoning(t *testing.T) {
	invalid := []string{
		`{"model":"gpt-5","instructions":123}`,
		`{"model":"gpt-5","stop":12}`,
		`{"model":"gpt-5","reasoning":{"summary":"verbose"}}`,
		`{"model":"gpt-5","reasoning":{"effort":4}}`,
	}
	for _, body := range invalid {
		if _, err := Decode(strings.NewReader(body), 1<<20); err == nil {
			t.Fatalf("Decode accepted invalid body: %s", body)
		}
	}
	if _, err := Decode(strings.NewReader(`{"model":"gpt-5","instructions":null,"stop":null,"reasoning":null,"background":{"future":true},"max_output_tokens":12.5}`), 1<<20); err != nil {
		t.Fatalf("Decode valid flexible fields: %v", err)
	}
}

func TestDecodeRejectsNullForNonNullableOptionalFields(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-5","stream":null}`,
		`{"model":"gpt-5","tools":null}`,
		`{"model":"gpt-5","previous_response_id":null}`,
		`{"model":"gpt-5","temperature":null}`,
		`{"model":"gpt-5","tool_choice":null}`,
		`{"model":"gpt-5","reasoning":{"summary":null}}`,
		`{"model":"gpt-5","reasoning":{"effort":null}}`,
	} {
		if _, err := Decode(strings.NewReader(body), 1<<20); err == nil {
			t.Fatalf("Decode accepted non-nullable null: %s", body)
		}
	}
}

func TestDecodeLooseCatchAllAcceptsEmptyTypeString(t *testing.T) {
	if _, err := Decode(strings.NewReader(`{"model":"gpt-5","input":[{"type":"","anything":true}]}`), 1<<20); err != nil {
		t.Fatalf("Decode loose empty type: %v", err)
	}
}

func TestDecodeRejectsWrongOptionalImageAndFileFieldTypes(t *testing.T) {
	for _, body := range []string{
		`{"model":"gpt-5","input":[{"role":"user","content":[{"type":"input_image","file_id":"f","image_url":null}]}]}`,
		`{"model":"gpt-5","input":[{"role":"user","content":[{"type":"input_file","file_id":null}]}]}`,
	} {
		if _, err := Decode(strings.NewReader(body), 1<<20); err == nil {
			t.Fatalf("Decode accepted invalid block: %s", body)
		}
	}
}

func TestDecodeRetainsBoundedRawBodyForProviderReencoding(t *testing.T) {
	body := `{"model":"openai-apikey/gpt-5.6","input":"hello","future_field":{"x":1}}`
	req, err := Decode(strings.NewReader(body), 1<<20)
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if !bytes.Contains(req.Raw, []byte(`"future_field"`)) {
		t.Fatalf("Raw=%s", req.Raw)
	}
}
