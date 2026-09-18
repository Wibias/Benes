package transport

import (
	"context"
	"strings"
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

type pacingLabelContextKey struct{}

// WithPacingLabel attaches a privacy-safe request label (normally the resolved
// upstream model id) so a dynamic pacer can select a model-specific rule and
// expose useful runtime telemetry without inspecting request bodies.
func WithPacingLabel(ctx context.Context, label string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, pacingLabelContextKey{}, strings.TrimSpace(label))
}

func PacingLabel(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	label, _ := ctx.Value(pacingLabelContextKey{}).(string)
	return strings.TrimSpace(label)
}

type pacerSlot struct {
	mu   sync.Mutex
	last time.Time
}

type PacerSnapshot struct {
	Queue         int
	UntilNextSlot time.Duration
	LastLabel     string
}

type Pacer struct {
	interval time.Duration
	clock    Clock

	mu           sync.Mutex
	slots        map[string]*pacerSlot
	queue        int
	lastGranted  time.Time
	lastInterval time.Duration
	lastLabel    string
}

func NewPacer(interval time.Duration, clock Clock) *Pacer {
	if clock == nil {
		clock = realClock{}
	}
	if interval < 0 {
		interval = 0
	}
	return &Pacer{interval: interval, clock: clock, slots: map[string]*pacerSlot{}}
}

// Acquire waits using the pacer's default interval.
func (p *Pacer) Acquire(ctx context.Context, key string) error {
	if p == nil {
		return nil
	}
	return p.AcquireWith(ctx, key, p.interval, "")
}

// AcquireWith waits using an interval resolved for this request. This keeps one
// provider-wide ordering slot while allowing model-specific pacing rules. The
// label is observational only and must not contain secrets.
func (p *Pacer) AcquireWith(ctx context.Context, key string, interval time.Duration, label string) error {
	if p == nil {
		return nil
	}
	if interval < 0 {
		interval = 0
	}
	if ctx == nil {
		ctx = context.Background()
	}
	label = strings.TrimSpace(label)
	if interval == 0 {
		p.recordGrant(p.clock.Now(), 0, label)
		return nil
	}

	p.adjustQueue(1)
	defer p.adjustQueue(-1)
	slot := p.slot(key)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := p.clock.Now()
		if slot.last.IsZero() || now.Sub(slot.last) >= interval {
			slot.last = now
			p.recordGrant(now, interval, label)
			return nil
		}
		wait := interval - now.Sub(slot.last)
		slot.mu.Unlock()
		timer := p.clock.After(wait)
		var err error
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-timer:
		}
		slot.mu.Lock()
		if err != nil {
			return err
		}
	}
}

func (p *Pacer) Snapshot() PacerSnapshot {
	if p == nil {
		return PacerSnapshot{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	until := time.Duration(0)
	if !p.lastGranted.IsZero() && p.lastInterval > 0 {
		until = p.lastGranted.Add(p.lastInterval).Sub(p.clock.Now())
		if until < 0 {
			until = 0
		}
	}
	return PacerSnapshot{Queue: p.queue, UntilNextSlot: until, LastLabel: p.lastLabel}
}

func (p *Pacer) adjustQueue(delta int) {
	p.mu.Lock()
	p.queue += delta
	if p.queue < 0 {
		p.queue = 0
	}
	p.mu.Unlock()
}

func (p *Pacer) recordGrant(now time.Time, interval time.Duration, label string) {
	p.mu.Lock()
	p.lastGranted = now
	p.lastInterval = interval
	if label != "" {
		p.lastLabel = label
	}
	p.mu.Unlock()
}

func (p *Pacer) slot(key string) *pacerSlot {
	p.mu.Lock()
	defer p.mu.Unlock()
	slot := p.slots[key]
	if slot == nil {
		slot = &pacerSlot{}
		p.slots[key] = slot
	}
	return slot
}
