package bootstrap

import (
	"context"
	"sync"
)

type codexQuotaPrimer interface {
	Prime(context.Context)
}

type codexQuotaPrimeScheduler struct {
	ctx     context.Context
	managed codexQuotaPrimer
	main    codexQuotaPrimer

	mu       sync.Mutex
	inFlight bool
}

func newCodexQuotaPrimeScheduler(ctx context.Context, managed, main codexQuotaPrimer) *codexQuotaPrimeScheduler {
	if managed == nil && main == nil {
		return nil
	}
	return &codexQuotaPrimeScheduler{ctx: ctx, managed: managed, main: main}
}

func (s *codexQuotaPrimeScheduler) Trigger() {
	if s == nil || s.ctx == nil || s.ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	if s.inFlight {
		s.mu.Unlock()
		return
	}
	s.inFlight = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			s.inFlight = false
			s.mu.Unlock()
		}()
		runCodexQuotaPrimers(s.ctx, s.managed, s.main)
	}()
}

func (s *codexQuotaPrimeScheduler) Running() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inFlight
}

func runCodexQuotaPrimers(ctx context.Context, managed, main codexQuotaPrimer) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	var wait sync.WaitGroup
	if managed != nil {
		wait.Add(1)
		go func() {
			defer wait.Done()
			managed.Prime(ctx)
		}()
	}
	if main != nil {
		wait.Add(1)
		go func() {
			defer wait.Done()
			main.Prime(ctx)
		}()
	}
	wait.Wait()
}
