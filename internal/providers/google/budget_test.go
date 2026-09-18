package google

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestOpenReservesTranslatorBytesBeforeDispatch(t *testing.T) {
	mgr := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassTranslator: 4},
	})
	turn, err := mgr.AcquireTurn(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()
	client, err := New(context.Background(), Config{APIKey: "gk-test", Endpoint: "https://generativelanguage.googleapis.com"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(context.Background(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			ModelID: "gemini-3.7-flash",
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
	payload := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello world\"}]}}]}\n"
	s := &stream{body: io.NopCloser(strings.NewReader(payload)), turn: turn}
	defer s.Close()
	_, err = s.Next()
	if !errors.Is(err, resourcebudget.ErrClassBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestFunctionCallReservesToolArgumentBytes(t *testing.T) {
	mgr := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassToolArguments: 4},
	})
	turn, err := mgr.AcquireTurn(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()
	s := &stream{
		buf:  "data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"lookup\",\"id\":\"c1\",\"args\":{\"q\":\"hello world\"}}}]}}]}\n",
		turn: turn,
	}
	_, err = s.Next()
	if !errors.Is(err, resourcebudget.ErrClassBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
}
