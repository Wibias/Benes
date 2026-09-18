package anthropicmessages

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/sse"
)

type rejectTripper struct{}

func (rejectTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("Anthropic request must reserve translator bytes before dispatch")
}

func TestOpenReservesTranslatorBytesBeforeDispatch(t *testing.T) {
	mgr := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassTranslator: 4},
	})
	turn, err := mgr.AcquireTurn(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()

	client, err := New(Config{
		Endpoint:   "https://api.anthropic.com/v1/messages",
		APIKey:     "test-key",
		HTTPClient: &http.Client{Transport: rejectTripper{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(t.Context(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			UpstreamModelID: "m",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hello world"}},
			}}},
		},
	})
	if !errors.Is(err, resourcebudget.ErrClassBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestNextReservesStreamPendingBytes(t *testing.T) {
	mgr := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassStreamPending: 4},
	})
	turn, err := mgr.AcquireTurn(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()

	wire := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"role\":\"assistant\",\"content\":[],\"model\":\"m\"}}\n\n"
	stream := NewStream(strings.NewReader(wire), sse.Limits{})
	stream.turn = turn
	_, err = stream.Next()
	if !errors.Is(err, resourcebudget.ErrClassBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
}
