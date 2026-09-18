package resourcebudget

import (
	"context"
	"testing"
	"time"
)

func TestAnonymousRequestsDoNotShareSessionSerializationKey(t *testing.T) {
	m := NewManager(Limits{MaxActiveTurns: 2, MaxProcessBytes: 1024, MaxTurnBytes: 512})
	first, err := m.AcquireTurn(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	second, err := m.AcquireTurn(ctx, "")
	if err != nil {
		t.Fatalf("anonymous request was serialized behind another anonymous request: %v", err)
	}
	second.Close()
}
