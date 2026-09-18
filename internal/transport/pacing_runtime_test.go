package transport

import (
	"context"
	"testing"
	"time"
)

func TestPacerAcquireWithUsesPerRequestIntervalAndTelemetry(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	clock := newFakeClock(start)
	pacer := NewPacer(time.Second, clock)
	if err := pacer.AcquireWith(context.Background(), "openai-apikey", 2*time.Second, "gpt-5"); err != nil {
		t.Fatal(err)
	}
	snap := pacer.Snapshot()
	if snap.LastLabel != "gpt-5" || snap.UntilNextSlot != 2*time.Second || snap.Queue != 0 {
		t.Fatalf("snapshot=%#v", snap)
	}

	done := make(chan error, 1)
	go func() {
		done <- pacer.AcquireWith(context.Background(), "openai-apikey", time.Second, "gpt-5-mini")
	}()
	clock.waitUntilWaiters(t, 1)
	if queued := pacer.Snapshot().Queue; queued != 1 {
		t.Fatalf("queue=%d", queued)
	}
	clock.Advance(time.Second)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	snap = pacer.Snapshot()
	if snap.LastLabel != "gpt-5-mini" || snap.Queue != 0 || snap.UntilNextSlot != time.Second {
		t.Fatalf("snapshot=%#v", snap)
	}
}

func TestPacingLabelRoundTripsThroughContext(t *testing.T) {
	ctx := WithPacingLabel(context.Background(), "gpt-5")
	if got := PacingLabel(ctx); got != "gpt-5" {
		t.Fatalf("label=%q", got)
	}
}
