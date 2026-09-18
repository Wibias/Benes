package antigravity

import (
	"context"
	"errors"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

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
		turn:    turn,
		pending: `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","id":"c1","args":{"q":"hello world"}}}]}}]}`,
	}
	_, err = s.Next()
	if !errors.Is(err, resourcebudget.ErrClassBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestFunctionCallWithinBudgetEmitsToolStart(t *testing.T) {
	mgr := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassToolArguments: 1 << 20},
	})
	turn, err := mgr.AcquireTurn(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()
	s := &stream{
		turn:    turn,
		pending: `data: {"candidates":[{"content":{"parts":[{"functionCall":{"name":"lookup","id":"c1","args":{"q":"x"}}}]}}]}`,
	}
	ev, err := s.Next()
	if err != nil || ev.Type != protocol.EventToolCallStart || ev.Name != "lookup" {
		t.Fatalf("event=%#v err=%v", ev, err)
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
	s := &stream{
		turn:    turn,
		pending: `data: {"candidates":[{"content":{"parts":[{"text":"hello world"}]}}]}`,
	}
	_, err = s.Next()
	if !errors.Is(err, resourcebudget.ErrClassBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
}
