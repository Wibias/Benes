package outbound

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestWriteSSEReservesDownstreamQueueOnTheTurn(t *testing.T) {
	mgr := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassDownstreamQueue: 8},
	})
	turn, err := mgr.AcquireTurn(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()
	stream := &fakeStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello world"},
		{Type: protocol.EventDone},
	}}
	err = WriteSSE(context.Background(), httptest.NewRecorder(), stream, "m", Options{
		IDGenerator: func() string { return "id" },
		CreatedUnix: 1,
		Turn:        turn,
	})
	if !errors.Is(err, resourcebudget.ErrClassBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
}
