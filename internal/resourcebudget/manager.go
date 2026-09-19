package resourcebudget

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrClassBudgetExceeded   = errors.New("resource class byte budget exceeded")
	ErrTurnBudgetExceeded    = errors.New("turn byte budget exceeded")
	ErrProcessBudgetExceeded = errors.New("process byte budget exceeded")
	ErrTurnClosed            = errors.New("turn is closed")
	ErrPostCommitRetry            = errors.New("ordinary retry is forbidden after downstream semantic commit")
	ErrInvalidReservation         = errors.New("reservation bytes must be non-negative")
	ErrPhysicalSendBudgetExceeded = errors.New("physical send budget exhausted")
	ErrPhysicalSendLeaseReleased  = errors.New("physical send reservation was released")
)

type Class string

const (
	ClassRequestBody     Class = "request_body"
	ClassStreamPending   Class = "stream_pending"
	ClassTranslator      Class = "translator"
	ClassToolArguments   Class = "tool_arguments"
	ClassOutput          Class = "output"
	ClassContinuation    Class = "continuation"
	ClassBlob            Class = "blob"
	ClassDownstreamQueue Class = "downstream_queue"
)

type Phase string

const (
	PhaseAdmission  Phase = "admission"
	PhaseUpstream   Phase = "upstream"
	PhaseTranslate  Phase = "translate"
	PhaseDownstream Phase = "downstream"
)

type Limits struct {
	MaxActiveTurns   int
	MaxProcessBytes  int64
	MaxTurnBytes     int64
	MaxPhysicalSends int
	ClassBytes       map[Class]int64
}

type Metrics struct {
	ActiveTurns             int
	QueuedTurns             int
	ActiveReaders           int
	ProcessBytes            int64
	Bytes                   map[Class]int64
	ActiveByPhase           map[Phase]int
	BudgetWaiters           int
	PostCommitRetryAttempts uint64
}

type Manager struct {
	mu sync.Mutex

	limits  Limits
	changed chan struct{}

	activeSessions          map[string]struct{}
	activeTurns             int
	queuedTurns             int
	activeReaders           int
	processBytes            int64
	bytes                   map[Class]int64
	activeByPhase           map[Phase]int
	budgetWaiters           int
	postCommitRetryAttempts uint64
}

type Turn struct {
	manager *Manager
	session string

	closed        bool
	committed     bool
	attemptActive bool
	phase         Phase
	readers                 int
	totalBytes              int64
	bytes                   map[Class]int64
	physicalSendReserved    int
	physicalSendCommitted   int
	nextPhysicalSendOrdinal int
	physicalSendRecords     []PhysicalSend
}

type Reservation struct {
	turn  *Turn
	class Class
	bytes int64
	once  sync.Once
}

type ReaderLease struct {
	turn *Turn
	once sync.Once
}

func NewManager(limits Limits) *Manager {
	if limits.MaxActiveTurns <= 0 {
		limits.MaxActiveTurns = 64
	}
	if limits.MaxProcessBytes <= 0 {
		limits.MaxProcessBytes = 512 << 20
	}
	if limits.MaxTurnBytes <= 0 {
		limits.MaxTurnBytes = 64 << 20
	}
	if limits.MaxPhysicalSends <= 0 {
		limits.MaxPhysicalSends = 8
	}
	classLimits := make(map[Class]int64, len(limits.ClassBytes))
	for class, value := range limits.ClassBytes {
		if value > 0 {
			classLimits[class] = value
		}
	}
	limits.ClassBytes = classLimits
	return &Manager{
		limits:         limits,
		changed:        make(chan struct{}),
		activeSessions: make(map[string]struct{}),
		bytes:          make(map[Class]int64),
		activeByPhase:  make(map[Phase]int),
	}
}

func (m *Manager) AcquireTurn(ctx context.Context, session string) (*Turn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	queued := false
	for {
		m.mu.Lock()
		sessionBusy := false
		if session != "" {
			_, sessionBusy = m.activeSessions[session]
		}
		capacity := m.activeTurns < m.limits.MaxActiveTurns
		if !sessionBusy && capacity {
			if queued {
				m.queuedTurns--
				m.budgetWaiters--
			}
			m.activeTurns++
			if session != "" {
				m.activeSessions[session] = struct{}{}
			}
			turn := &Turn{manager: m, session: session, bytes: make(map[Class]int64)}
			m.mu.Unlock()
			return turn, nil
		}
		if !queued {
			queued = true
			m.queuedTurns++
			m.budgetWaiters++
		}
		changed := m.changed
		m.mu.Unlock()

		select {
		case <-ctx.Done():
			m.mu.Lock()
			if queued {
				m.queuedTurns--
				m.budgetWaiters--
			}
			m.mu.Unlock()
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

// TryAcquireTurn grants a turn immediately or returns false without waiting.
func (m *Manager) TryAcquireTurn(session string) (*Turn, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sessionBusy := false
	if session != "" {
		_, sessionBusy = m.activeSessions[session]
	}
	if sessionBusy || m.activeTurns >= m.limits.MaxActiveTurns {
		return nil, false
	}
	m.activeTurns++
	if session != "" {
		m.activeSessions[session] = struct{}{}
	}
	return &Turn{manager: m, session: session, bytes: make(map[Class]int64)}, true
}

func (t *Turn) Reserve(class Class, bytes int64) (*Reservation, error) {
	if bytes < 0 {
		return nil, ErrInvalidReservation
	}
	if bytes == 0 {
		return &Reservation{}, nil
	}
	if t == nil || t.manager == nil {
		return nil, ErrTurnClosed
	}
	m := t.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.closed {
		return nil, ErrTurnClosed
	}
	if limit := m.limits.ClassBytes[class]; limit > 0 && m.bytes[class]+bytes > limit {
		return nil, ErrClassBudgetExceeded
	}
	if m.limits.MaxTurnBytes > 0 && t.totalBytes+bytes > m.limits.MaxTurnBytes {
		return nil, ErrTurnBudgetExceeded
	}
	if m.limits.MaxProcessBytes > 0 && m.processBytes+bytes > m.limits.MaxProcessBytes {
		return nil, ErrProcessBudgetExceeded
	}
	t.totalBytes += bytes
	t.bytes[class] += bytes
	m.processBytes += bytes
	m.bytes[class] += bytes
	return &Reservation{turn: t, class: class, bytes: bytes}, nil
}

func (r *Reservation) Release() {
	if r == nil || r.turn == nil || r.bytes == 0 {
		return
	}
	r.once.Do(func() {
		t := r.turn
		m := t.manager
		m.mu.Lock()
		defer m.mu.Unlock()
		if t.closed {
			return
		}
		t.totalBytes -= r.bytes
		t.bytes[r.class] -= r.bytes
		m.processBytes -= r.bytes
		m.bytes[r.class] -= r.bytes
		if t.bytes[r.class] == 0 {
			delete(t.bytes, r.class)
		}
		if m.bytes[r.class] == 0 {
			delete(m.bytes, r.class)
		}
	})
}

func (t *Turn) OpenReader() *ReaderLease {
	if t == nil || t.manager == nil {
		return &ReaderLease{}
	}
	m := t.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.closed {
		return &ReaderLease{}
	}
	t.readers++
	m.activeReaders++
	return &ReaderLease{turn: t}
}

func (r *ReaderLease) Close() error {
	if r == nil || r.turn == nil {
		return nil
	}
	r.once.Do(func() {
		t := r.turn
		m := t.manager
		m.mu.Lock()
		defer m.mu.Unlock()
		if t.closed || t.readers == 0 {
			return
		}
		t.readers--
		m.activeReaders--
	})
	return nil
}

func (t *Turn) SetPhase(phase Phase) {
	if t == nil || t.manager == nil {
		return
	}
	m := t.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.closed || t.phase == phase {
		return
	}
	if t.phase != "" {
		m.activeByPhase[t.phase]--
		if m.activeByPhase[t.phase] == 0 {
			delete(m.activeByPhase, t.phase)
		}
	}
	t.phase = phase
	if phase != "" {
		m.activeByPhase[phase]++
	}
}

func (t *Turn) MarkCommitted() {
	if t == nil || t.manager == nil {
		return
	}
	t.manager.mu.Lock()
	defer t.manager.mu.Unlock()
	if !t.closed {
		t.committed = true
	}
}

func (t *Turn) StartAttempt() error {
	if t == nil || t.manager == nil {
		return ErrTurnClosed
	}
	m := t.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.closed {
		return ErrTurnClosed
	}
	if t.committed {
		return ErrPostCommitRetry
	}
	t.attemptActive = true
	return nil
}

func (t *Turn) EndAttempt() {
	if t == nil || t.manager == nil {
		return
	}
	t.manager.mu.Lock()
	defer t.manager.mu.Unlock()
	if !t.closed {
		t.attemptActive = false
	}
}

func (t *Turn) Close() error {
	if t == nil || t.manager == nil {
		return nil
	}
	m := t.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	if t.phase != "" {
		m.activeByPhase[t.phase]--
		if m.activeByPhase[t.phase] == 0 {
			delete(m.activeByPhase, t.phase)
		}
	}
	for class, bytes := range t.bytes {
		m.bytes[class] -= bytes
		if m.bytes[class] == 0 {
			delete(m.bytes, class)
		}
	}
	m.processBytes -= t.totalBytes
	m.activeReaders -= t.readers
	t.readers = 0
	t.totalBytes = 0
	t.bytes = nil
	if t.session != "" {
		delete(m.activeSessions, t.session)
	}
	m.activeTurns--
	m.signalLocked()
	return nil
}

func (m *Manager) Metrics() Metrics {
	if m == nil {
		return Metrics{Bytes: map[Class]int64{}, ActiveByPhase: map[Phase]int{}}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	bytes := make(map[Class]int64, len(m.bytes))
	for class, value := range m.bytes {
		bytes[class] = value
	}
	phases := make(map[Phase]int, len(m.activeByPhase))
	for phase, value := range m.activeByPhase {
		phases[phase] = value
	}
	return Metrics{
		ActiveTurns:             m.activeTurns,
		QueuedTurns:             m.queuedTurns,
		ActiveReaders:           m.activeReaders,
		ProcessBytes:            m.processBytes,
		Bytes:                   bytes,
		ActiveByPhase:           phases,
		BudgetWaiters:           m.budgetWaiters,
		PostCommitRetryAttempts: m.postCommitRetryAttempts,
	}
}

func (m *Manager) signalLocked() {
	close(m.changed)
	m.changed = make(chan struct{})
}
