package timeline

import (
	"sync"
	"time"
)

type Side string

const (
	SideUpstream   Side = "upstream"
	SideRelay      Side = "relay"
	SideDownstream Side = "downstream"
	SideClient     Side = "client"
	SideLocal      Side = "local"
)

type Stage string

const (
	StagePreDispatch         Stage = "pre_dispatch"
	StageUpstreamWaitHeaders Stage = "upstream_wait_headers"
	StageUpstreamRead        Stage = "upstream_read"
	StageRelayTransform      Stage = "relay_transform"
	StageDownstreamWrite     Stage = "downstream_write"
	StageClientCancel        Stage = "client_cancel"
	StageTerminalDelivery    Stage = "terminal_delivery"
)

type Milestone string

const (
	MilestoneDispatch        Milestone = "dispatch"
	MilestoneHeaders         Milestone = "headers"
	MilestoneFirstByte       Milestone = "first_byte"
	MilestoneTTFT            Milestone = "ttft"
	MilestoneFirstDownstream Milestone = "first_downstream"
	MilestoneUpstreamEnd     Milestone = "upstream_end"
	MilestoneDownstreamEnd   Milestone = "downstream_end"
)

type Event struct {
	Stage     Stage
	Side      Side
	Milestone Milestone
	At        time.Time
	Elapsed   time.Duration
	Attempt   int
	OK        bool
	Cause     string
}

type Attribution struct {
	Side  Side
	Stage Stage
	Cause string
}

// Route records the user-requested provider identity and the physical
// connection that actually owns dispatch. Model remains the upstream model id,
// without the provider namespace.
type Route struct {
	RequestedProvider  string
	ProviderConnection string
	Model              string
}

type Trace struct {
	mu      sync.Mutex
	id      string
	start   time.Time
	events  []Event
	limit   int
	attempt int
	route   Route
}

func New(id string, limit int) *Trace {
	if limit <= 0 {
		limit = 32
	}
	return &Trace{id: id, start: time.Now().UTC(), limit: limit}
}

func (t *Trace) ID() string {
	if t == nil {
		return ""
	}
	return t.id
}

func (t *Trace) SetRoute(route Route) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.route = route
}

func (t *Trace) Route() Route {
	if t == nil {
		return Route{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.route
}

func (t *Trace) SetAttempt(n int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if n < 0 {
		n = 0
	}
	t.attempt = n
}

func (t *Trace) Attempt() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.attempt
}

func (t *Trace) Mark(stage Stage, side Side, milestone Milestone, ok bool, cause string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.events) >= t.limit {
		return
	}
	now := time.Now().UTC()
	t.events = append(t.events, Event{
		Stage:     stage,
		Side:      side,
		Milestone: milestone,
		At:        now,
		Elapsed:   now.Sub(t.start),
		Attempt:   t.attempt,
		OK:        ok,
		Cause:     cause,
	})
}

func (t *Trace) Events() []Event {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Event, len(t.events))
	copy(out, t.events)
	return out
}

func (t *Trace) Classify() Attribution {
	if t == nil {
		return Attribution{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := len(t.events) - 1; i >= 0; i-- {
		if !t.events[i].OK {
			return Attribution{Side: t.events[i].Side, Stage: t.events[i].Stage, Cause: t.events[i].Cause}
		}
	}
	return Attribution{}
}
