package resourcebudget

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestOneActiveTurnPerSessionWhileIndependentSessionsRun(t *testing.T) {
	m := NewManager(Limits{MaxActiveTurns: 2, MaxProcessBytes: 1024, MaxTurnBytes: 512})
	first, err := m.AcquireTurn(context.Background(), "session-a")
	if err != nil {
		t.Fatal(err)
	}
	other, err := m.AcquireTurn(context.Background(), "session-b")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := m.AcquireTurn(ctx, "session-a"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("same session should wait until the active turn releases, got %v", err)
	}
	first.Close()

	next, err := m.AcquireTurn(context.Background(), "session-a")
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
}

func TestGlobalTurnCapIsSeparateFromSessionSerialization(t *testing.T) {
	m := NewManager(Limits{MaxActiveTurns: 1, MaxProcessBytes: 1024, MaxTurnBytes: 512})
	first, err := m.AcquireTurn(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := m.AcquireTurn(ctx, "b"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("global cap should wait even for another session, got %v", err)
	}
	first.Close()
}

func TestByteReservationsEnforceClassTurnAndProcessBounds(t *testing.T) {
	m := NewManager(Limits{
		MaxActiveTurns:  2,
		MaxProcessBytes: 100,
		MaxTurnBytes:    70,
		ClassBytes: map[Class]int64{
			ClassStreamPending: 40,
			ClassToolArguments: 30,
			ClassContinuation:  60,
		},
	})
	a, _ := m.AcquireTurn(context.Background(), "a")
	b, _ := m.AcquireTurn(context.Background(), "b")
	defer a.Close()
	defer b.Close()

	stream, err := a.Reserve(ClassStreamPending, 40)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Reserve(ClassStreamPending, 1); !errors.Is(err, ErrClassBudgetExceeded) {
		t.Fatalf("class overflow = %v", err)
	}
	tool, err := a.Reserve(ClassToolArguments, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Reserve(ClassContinuation, 1); !errors.Is(err, ErrTurnBudgetExceeded) {
		t.Fatalf("turn overflow = %v", err)
	}
	if _, err := b.Reserve(ClassContinuation, 31); !errors.Is(err, ErrProcessBudgetExceeded) {
		t.Fatalf("process overflow = %v", err)
	}
	stream.Release()
	if _, err := b.Reserve(ClassContinuation, 31); err != nil {
		t.Fatalf("released process bytes not reusable: %v", err)
	}
	tool.Release()
}

func TestCloseIdempotentlyReleasesEveryOutstandingResource(t *testing.T) {
	m := NewManager(Limits{MaxActiveTurns: 1, MaxProcessBytes: 128, MaxTurnBytes: 128})
	turn, err := m.AcquireTurn(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := turn.Reserve(ClassTranslator, 64); err != nil {
		t.Fatal(err)
	}
	reader := turn.OpenReader()
	turn.SetPhase(PhaseUpstream)
	turn.Close()
	turn.Close()
	reader.Close()

	metrics := m.Metrics()
	if metrics.ActiveTurns != 0 || metrics.ActiveReaders != 0 || metrics.ProcessBytes != 0 || metrics.Bytes[ClassTranslator] != 0 {
		t.Fatalf("resources leaked after close: %#v", metrics)
	}
}

func TestReservationReleaseIsIdempotentAndCannotUnderflow(t *testing.T) {
	m := NewManager(Limits{MaxActiveTurns: 1, MaxProcessBytes: 64, MaxTurnBytes: 64})
	turn, _ := m.AcquireTurn(context.Background(), "s")
	lease, err := turn.Reserve(ClassRequestBody, 32)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	lease.Release()
	if got := m.Metrics().ProcessBytes; got != 0 {
		t.Fatalf("double release under/over-counted: %d", got)
	}
	turn.Close()
}

func TestRetryBoundaryForbidsOrdinaryPostCommitAttempts(t *testing.T) {
	m := NewManager(Limits{MaxActiveTurns: 1})
	turn, _ := m.AcquireTurn(context.Background(), "s")
	defer turn.Close()
	if err := turn.StartAttempt(); err != nil {
		t.Fatal(err)
	}
	turn.EndAttempt()
	turn.MarkCommitted()
	if err := turn.StartAttempt(); !errors.Is(err, ErrPostCommitRetry) {
		t.Fatalf("post-commit retry = %v", err)
	}
	metrics := m.Metrics()
	if metrics.PostCommitRetryAttempts != 0 {
		t.Fatalf("forbidden retries must not increment the invariant counter: %d", metrics.PostCommitRetryAttempts)
	}
}

func TestMetricsExposeOnlyBoundedCountsAndBytes(t *testing.T) {
	m := NewManager(Limits{MaxActiveTurns: 2, MaxProcessBytes: 100, MaxTurnBytes: 100})
	turn, _ := m.AcquireTurn(context.Background(), "secret-session-id")
	defer turn.Close()
	turn.SetPhase(PhaseTranslate)
	lease, _ := turn.Reserve(ClassTranslator, 20)
	defer lease.Release()
	reader := turn.OpenReader()
	defer reader.Close()

	got := m.Metrics()
	if got.ActiveTurns != 1 || got.ActiveReaders != 1 || got.ProcessBytes != 20 || got.Bytes[ClassTranslator] != 20 || got.ActiveByPhase[PhaseTranslate] != 1 {
		t.Fatalf("metrics = %#v", got)
	}
}

func TestConcurrentReservationAndCloseReturnsToIdle(t *testing.T) {
	m := NewManager(Limits{MaxActiveTurns: 8, MaxProcessBytes: 8 << 20, MaxTurnBytes: 1 << 20})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		turn, err := m.AcquireTurn(context.Background(), string(rune('a'+i)))
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(turn *Turn) {
			defer wg.Done()
			defer turn.Close()
			for j := 0; j < 100; j++ {
				lease, err := turn.Reserve(ClassStreamPending, 1024)
				if err != nil {
					t.Errorf("reserve: %v", err)
					return
				}
				lease.Release()
			}
		}(turn)
	}
	wg.Wait()
	got := m.Metrics()
	if got.ActiveTurns != 0 || got.ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", got)
	}
}

func TestSoak32Sessions10WavesReturnToIdle(t *testing.T) {
	m := NewManager(Limits{MaxActiveTurns: 32, MaxProcessBytes: 32 << 20, MaxTurnBytes: 1 << 20})
	for range 10 {
		var wg sync.WaitGroup
		for i := range 32 {
			turn, err := m.AcquireTurn(context.Background(), string(rune('a'+i%26)))
			if err != nil {
				t.Fatal(err)
			}
			wg.Add(1)
			go func(turn *Turn) {
				defer wg.Done()
				defer turn.Close()
				lease, err := turn.Reserve(ClassStreamPending, 64)
				if err != nil {
					t.Errorf("reserve: %v", err)
					return
				}
				lease.Release()
			}(turn)
		}
		wg.Wait()
	}
	got := m.Metrics()
	if got.ActiveTurns != 0 || got.ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", got)
	}
}


func TestAccumulatorCountsChunksAndJoinsOnce(t *testing.T) {
	m := NewManager(Limits{MaxActiveTurns: 1, MaxProcessBytes: 64, MaxTurnBytes: 64, ClassBytes: map[Class]int64{ClassToolArguments: 8}})
	turn, _ := m.AcquireTurn(context.Background(), "s")
	defer turn.Close()
	acc := NewAccumulator(turn, ClassToolArguments)
	if err := acc.Append([]byte(`{"a"`)); err != nil {
		t.Fatal(err)
	}
	if err := acc.Append([]byte(`:1}`)); err != nil {
		t.Fatal(err)
	}
	if got := string(acc.Bytes()); got != `{"a":1}` {
		t.Fatalf("joined = %q", got)
	}
	acc.Close()
	if got := m.Metrics().Bytes[ClassToolArguments]; got != 0 {
		t.Fatalf("accumulator bytes leaked: %d", got)
	}
}

func TestAccumulatorOverflowFailsWithoutRetainingRejectedChunk(t *testing.T) {
	m := NewManager(Limits{MaxActiveTurns: 1, MaxProcessBytes: 64, MaxTurnBytes: 64, ClassBytes: map[Class]int64{ClassToolArguments: 4}})
	turn, _ := m.AcquireTurn(context.Background(), "s")
	defer turn.Close()
	acc := NewAccumulator(turn, ClassToolArguments)
	if err := acc.Append([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	if err := acc.Append([]byte("5")); !errors.Is(err, ErrClassBudgetExceeded) {
		t.Fatalf("overflow = %v", err)
	}
	if got := string(acc.Bytes()); got != "1234" {
		t.Fatalf("rejected chunk retained: %q", got)
	}
	acc.Close()
}
