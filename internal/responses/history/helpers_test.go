package history

import (
	"encoding/json"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/responses/reasoning"
	"github.com/Wibias/Benes/internal/responses/request"
)

func strptr(s string) *string { return &s }
func item(t *testing.T, raw string) request.Item {
	t.Helper()
	var h struct{ Type, Role string }
	if err := json.Unmarshal([]byte(raw), &h); err != nil {
		t.Fatal(err)
	}
	return request.Item{Type: h.Type, Role: h.Role, Raw: json.RawMessage(raw)}
}
func parse(t *testing.T, in request.Input) protocol.Context {
	t.Helper()
	got, err := Build(&request.Request{Model: "model-x", Input: in}, 1234)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
func enc(t *testing.T, e reasoning.Envelope) string {
	t.Helper()
	s, err := reasoning.Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func j(t *testing.T, s string) string { t.Helper(); b, _ := json.Marshal(s); return string(b) }
