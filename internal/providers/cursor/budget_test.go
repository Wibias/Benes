package cursor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

type rejectTripper struct{}

func (rejectTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("compiled Cursor run frame must reserve translator bytes before dispatch")
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
	client, err := NewHardened(context.Background(), Config{
		Endpoint:   DefaultAPI,
		APIKey:     "tok",
		HTTPClient: &http.Client{Transport: rejectTripper{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(context.Background(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			UpstreamModelID: "gpt-5.4",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hello world"}},
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
	frame, err := EncodeConnectFrame([]byte("hello world"), false)
	if err != nil {
		t.Fatal(err)
	}
	s := &stream{body: io.NopCloser(bytes.NewReader(frame)), turn: turn}
	_, err = s.Next()
	if !errors.Is(err, resourcebudget.ErrClassBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestPutConversationReservesBlobBytes(t *testing.T) {
	mgr := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassBlob: 4},
	})
	turn, err := mgr.AcquireTurn(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()
	store := NewBlobStore()
	err = store.PutConversationTurn(RunRequest{
		System: []string{"system prompt that exceeds four bytes"},
		Turns:  []RunTurn{{Role: "user", Text: "hi"}},
	}, turn)
	if !errors.Is(err, resourcebudget.ErrClassBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
}
