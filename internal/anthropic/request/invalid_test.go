package request

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeRecordsHostedWebSearchTools(t *testing.T) {
	got, err := Decode(strings.NewReader(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`), 1<<20, DecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Context.Tools) != 1 || !got.Context.Tools[0].HostedWebSearch {
		t.Fatalf("tools=%#v", got.Context.Tools)
	}
}

func TestDecodeBoundsAndRejectsTrailingOrMalformedRequiredShape(t *testing.T) {
	body := `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	if _, err := Decode(strings.NewReader(body), 8, DecodeOptions{}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limit err=%v", err)
	}
	for _, raw := range []string{
		body + ` {}`,
		`[]`,
		`{"max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"m","max_tokens":10,"messages":[]}`,
		`{"model":"m","max_tokens":0,"messages":[{"role":"user","content":"hi"}]}`,
	} {
		if _, err := Decode(strings.NewReader(raw), 1<<20, DecodeOptions{}); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestDecodeRejectsMalformedToolIdentityPairingImagesSchemasAndNumbers(t *testing.T) {
	cases := []string{
		`{"model":"m","max_tokens":10,"messages":[{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{}}]}]}`,
		`{"model":"m","max_tokens":10,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"t1","input":{}}]}]}`,
		`{"model":"m","max_tokens":10,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":[]}]}]}`,
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"tool_result","content":"x"}]}]}`,
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":""}}]}]}`,
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":""}}]}]}`,
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Read","input_schema":[]}]}`,
		`{"model":"m","max_tokens":"10","messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"m","max_tokens":10.5,"messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"m","max_tokens":-1,"messages":[{"role":"user","content":"hi"}]}`,
	}
	for _, raw := range cases {
		if _, err := Decode(strings.NewReader(raw), 1<<20, DecodeOptions{}); err == nil {
			t.Fatalf("accepted malformed request: %s", raw)
		}
	}
}

func TestDecodeRejectsMalformedKnownOptionTypes(t *testing.T) {
	cases := []string{
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"temperature":"0.7"}`,
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"top_p":true}`,
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"stop_sequences":"STOP"}`,
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"tool_choice":{"type":"auto","disable_parallel_tool_use":"true"}}`,
	}
	for _, raw := range cases {
		if _, err := Decode(strings.NewReader(raw), 1<<20, DecodeOptions{}); err == nil {
			t.Fatalf("accepted malformed option: %s", raw)
		}
	}
}

func TestDecodeRejectsMalformedBooleanAndThinkingShapes(t *testing.T) {
	cases := []string{
		`{"model":"m","max_tokens":10,"stream":"true","messages":[{"role":"user","content":"hi"}]}`,
		`{"model":"m","max_tokens":10,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","is_error":"true","content":"boom"}]}]}`,
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"thinking":"enabled"}`,
		`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":"8192"}}`,
	}
	for _, raw := range cases {
		if _, err := Decode(strings.NewReader(raw), 1<<20, DecodeOptions{}); err == nil {
			t.Fatalf("accepted malformed typed field: %s", raw)
		}
	}
}
