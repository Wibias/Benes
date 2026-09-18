package openairesponses

import (
	"context"
	"sync"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

type ForwardOutcomeKind string

const (
	ForwardOutcomeHTTP           ForwardOutcomeKind = "http"
	ForwardOutcomeTransportError ForwardOutcomeKind = "transport_error"
	ForwardOutcomeCompleted      ForwardOutcomeKind = "completed"
	ForwardOutcomeFailed         ForwardOutcomeKind = "failed"
	ForwardOutcomeIncomplete     ForwardOutcomeKind = "incomplete"
)

type ForwardOutcome struct {
	Kind       ForwardOutcomeKind
	StatusCode int
	RetryAfter string
	ResetAt    []string
	Denial     ForwardDenial
	TimedOut   bool
}

type ForwardQuotaHeaders struct {
	PrimaryUsedPercent     string
	SecondaryUsedPercent   string
	TertiaryUsedPercent    string
	PrimaryResetAt         string
	SecondaryResetAt       string
	TertiaryResetAt        string
	PrimaryWindowMinutes   string
	SecondaryWindowMinutes string
}

type ForwardOutcomeObserver interface {
	Observe(ForwardOutcome)
}

type ForwardQuotaObserver interface {
	ObserveQuota(ForwardQuotaHeaders)
}

type ForwardOutcomeAbandoner interface {
	Abandon()
}

type ForwardOutcomeFunc func(ForwardOutcome)

func (f ForwardOutcomeFunc) Observe(outcome ForwardOutcome) {
	f(outcome)
}

type ForwardQuotaRetryFunc func(context.Context) (ForwardAttempt, bool, error)
type ForwardModel400RetryFunc func(context.Context) (ForwardAttempt, bool, error)

type ForwardAttempt struct {
	Credential       ForwardCredential
	Observer         ForwardOutcomeObserver
	RetryQuota       ForwardQuotaRetryFunc
	RetryModel400    ForwardModel400RetryFunc
	RetryAuth        ForwardQuotaRetryFunc
	CommitQuotaRetry func()
}

type ForwardAttemptAuthority interface {
	ResolveAttempt(context.Context, providers.DispatchRequest) (ForwardAttempt, error)
}

func resolveForwardAttempt(
	ctx context.Context,
	authority ForwardCredentialAuthority,
	dispatch providers.DispatchRequest,
) (ForwardAttempt, error) {
	if attemptAuthority, ok := authority.(ForwardAttemptAuthority); ok {
		return attemptAuthority.ResolveAttempt(ctx, dispatch)
	}
	credential, err := authority.Resolve(ctx, dispatch)
	if err != nil {
		return ForwardAttempt{}, err
	}
	return ForwardAttempt{Credential: credential}, nil
}

func abandonForwardObserver(observer ForwardOutcomeObserver) {
	if abandoner, ok := observer.(ForwardOutcomeAbandoner); ok {
		abandoner.Abandon()
	}
}

type observedForwardStream struct {
	ctx      context.Context
	inner    providers.EventStream
	observer ForwardOutcomeObserver

	mu         sync.Mutex
	settled    bool
	closed     bool
	done       chan struct{}
	doneClosed bool
}

func newObservedForwardStream(
	ctx context.Context,
	inner providers.EventStream,
	observer ForwardOutcomeObserver,
) providers.EventStream {
	if observer == nil {
		return inner
	}
	stream := &observedForwardStream{ctx: ctx, inner: inner, observer: observer}
	if _, ok := observer.(ForwardOutcomeAbandoner); ok {
		stream.done = make(chan struct{})
		go stream.watchContext()
	}
	return stream
}

func (s *observedForwardStream) watchContext() {
	select {
	case <-s.ctx.Done():
		s.abandon()
	case <-s.done:
	}
}

func (s *observedForwardStream) Next() (protocol.Event, error) {
	event, err := s.inner.Next()
	if err != nil {
		if s.ctx.Err() != nil {
			s.abandon()
		} else {
			s.report(ForwardOutcome{Kind: ForwardOutcomeIncomplete})
		}
		return event, err
	}
	switch event.Type {
	case protocol.EventDone:
		s.report(ForwardOutcome{Kind: ForwardOutcomeCompleted})
	case protocol.EventError:
		s.report(ForwardOutcome{Kind: ForwardOutcomeFailed})
	case protocol.EventIncomplete:
		s.report(ForwardOutcome{Kind: ForwardOutcomeIncomplete})
	}
	return event, nil
}

func (s *observedForwardStream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	abandoner := s.settleAbandonLocked()
	s.mu.Unlock()
	if abandoner != nil {
		abandoner.Abandon()
	}
	return s.inner.Close()
}

// PhysicalOwnership forwards the inner stream pin. Observation is identity-transparent.
func (s *observedForwardStream) PhysicalOwnership() *providers.PhysicalPin {
	if s == nil || s.inner == nil {
		return nil
	}
	if reporter, ok := s.inner.(interface{ PhysicalOwnership() *providers.PhysicalPin }); ok {
		return reporter.PhysicalOwnership()
	}
	return nil
}

func (s *observedForwardStream) report(outcome ForwardOutcome) {
	s.mu.Lock()
	if s.settled {
		s.mu.Unlock()
		return
	}
	s.settled = true
	s.closeDoneLocked()
	observer := s.observer
	s.mu.Unlock()
	observer.Observe(outcome)
}

func (s *observedForwardStream) abandon() {
	s.mu.Lock()
	abandoner := s.settleAbandonLocked()
	s.mu.Unlock()
	if abandoner != nil {
		abandoner.Abandon()
	}
}

func (s *observedForwardStream) settleAbandonLocked() ForwardOutcomeAbandoner {
	if s.settled {
		return nil
	}
	s.settled = true
	s.closeDoneLocked()
	abandoner, _ := s.observer.(ForwardOutcomeAbandoner)
	return abandoner
}

func (s *observedForwardStream) closeDoneLocked() {
	if s.done != nil && !s.doneClosed {
		close(s.done)
		s.doneClosed = true
	}
}

var _ providers.EventStream = (*observedForwardStream)(nil)
