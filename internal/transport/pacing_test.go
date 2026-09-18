package transport

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []clockWaiter
}

type clockWaiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, clockWaiter{at: c.now.Add(d), ch: ch})
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	var ready []clockWaiter
	var pending []clockWaiter
	for _, waiter := range c.waiters {
		if !waiter.at.After(now) {
			ready = append(ready, waiter)
		} else {
			pending = append(pending, waiter)
		}
	}
	c.waiters = pending
	c.mu.Unlock()
	for _, waiter := range ready {
		waiter.ch <- now
	}
}

func TestPacerAnchorsIntervalToGrantedTransportStart(t *testing.T) {
	clock := newFakeClock(time.Unix(1_700_000_000, 0))
	pacer := NewPacer(100*time.Millisecond, clock)
	starts := make(chan time.Time, 3)

	acquire := func() {
		if err := pacer.Acquire(context.Background(), "openai/gpt"); err != nil {
			t.Errorf("acquire: %v", err)
			return
		}
		starts <- clock.Now()
	}

	go acquire()
	waitForStarts(t, starts, 1)
	go acquire()
	go acquire()
	clock.waitUntilWaiters(t, 2)
	clock.Advance(100 * time.Millisecond)
	second := waitForStarts(t, starts, 1)[0]
	clock.waitUntilWaiters(t, 1)
	clock.Advance(100 * time.Millisecond)
	third := waitForStarts(t, starts, 1)[0]
	if second.Sub(time.Unix(1_700_000_000, 0)) != 100*time.Millisecond {
		t.Fatalf("second start = %s", second)
	}
	if third.Sub(second) != 100*time.Millisecond {
		t.Fatalf("third-second = %s", third.Sub(second))
	}
}

func TestPacerCancelFailsClosedWithoutGrantingStart(t *testing.T) {
	clock := newFakeClock(time.Unix(1_700_000_000, 0))
	pacer := NewPacer(time.Second, clock)
	if err := pacer.Acquire(context.Background(), "k"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- pacer.Acquire(ctx, "k") }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled wait granted a start")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled wait did not return")
	}
}

func (c *fakeClock) waitUntilWaiters(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		count := len(c.waiters)
		c.mu.Unlock()
		if count >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("clock waiters never reached %d", n)
}

func waitForStarts(t *testing.T, starts <-chan time.Time, n int) []time.Time {
	t.Helper()
	out := make([]time.Time, 0, n)
	deadline := time.After(2 * time.Second)
	for len(out) < n {
		select {
		case got := <-starts:
			out = append(out, got)
		case <-deadline:
			t.Fatalf("waiting for %d starts, got %d", n, len(out))
		}
	}
	return out
}
