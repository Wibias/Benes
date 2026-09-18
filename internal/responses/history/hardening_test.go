package history

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/responses/request"
)

func TestBuildNilRequestFails(t *testing.T) {
	if _, err := Build(nil, 1); err == nil {
		t.Fatal("expected error")
	}
}

func TestUnsupportedTypedItemFailsClosed(t *testing.T) {
	_, err := Build(&request.Request{Model: "m", Input: request.Input{Items: []request.Item{
		item(t, `{"type":"computer_call","id":"w"}`),
	}}}, 1)
	if err == nil || !errors.Is(err, ErrUnsupportedItem) {
		t.Fatalf("err=%v", err)
	}
}

func TestProviderOpaqueMalformedMetadataIsIgnored(t *testing.T) {
	for _, extra := range []string{
		`"not-an-object"`,
		`{"google":"not-an-object"}`,
		`{"google":{"thought_signature":7}}`,
		`{"google":{"thought_signature":""}}`,
	} {
		raw := `{"type":"function_call","call_id":"c","name":"tool","arguments":"{}","extra_content":` + extra + `}`
		ctx := parse(t, request.Input{Items: []request.Item{item(t, raw)}})
		if ctx.Messages[0].Content[0].ProviderMetadata != nil {
			t.Fatalf("extra=%s metadata=%+v", extra, ctx.Messages[0].Content[0].ProviderMetadata)
		}
	}
}

func TestProviderOpaqueThoughtSignatureByteLimit(t *testing.T) {
	within := strings.Repeat("é", maxOpaqueSignatureBytes/2)
	withinJSON, _ := json.Marshal(within)
	ctx := parse(t, request.Input{Items: []request.Item{item(t, `{"type":"function_call","call_id":"c","name":"tool","arguments":"{}","extra_content":{"google":{"thought_signature":`+string(withinJSON)+`}}}`)}})
	if ctx.Messages[0].Content[0].ProviderMetadata == nil {
		t.Fatal("signature at byte limit was dropped")
	}

	over := within + "x"
	overJSON, _ := json.Marshal(over)
	ctx = parse(t, request.Input{Items: []request.Item{item(t, `{"type":"function_call","call_id":"c","name":"tool","arguments":"{}","extra_content":{"google":{"thought_signature":`+string(overJSON)+`}}}`)}})
	if ctx.Messages[0].Content[0].ProviderMetadata != nil {
		t.Fatal("signature above byte limit was preserved")
	}
}
