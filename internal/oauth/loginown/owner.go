package loginown

import (
	"context"
	"sync"

	"github.com/Wibias/Benes/internal/authpublic"
)

type Token struct {
	Provider string
	Gen      uint64
}

type slot struct {
	gen      uint64
	active   bool
	settling int
	waiters  chan struct{}
}

var (
	mu    sync.Mutex
	slots = map[string]*slot{}
)

func slotFor(provider string) *slot {
	s, ok := slots[provider]
	if !ok {
		s = &slot{}
		slots[provider] = s
	}
	return s
}

func TryClaim(provider string) (Token, error) {
	mu.Lock()
	defer mu.Unlock()
	s := slotFor(provider)
	if s.active || s.settling > 0 {
		return Token{}, authpublic.LoginBusyError{Provider: provider}
	}
	s.gen++
	s.active = true
	return Token{Provider: provider, Gen: s.gen}, nil
}

func Claim(provider string) Token {
	mu.Lock()
	defer mu.Unlock()
	s := slotFor(provider)
	s.gen++
	s.active = true
	return Token{Provider: provider, Gen: s.gen}
}

func Current(provider string) (Token, bool) {
	mu.Lock()
	defer mu.Unlock()
	s := slotFor(provider)
	if !s.active || s.gen == 0 {
		return Token{}, false
	}
	return Token{Provider: provider, Gen: s.gen}, true
}

func Assert(token Token) error {
	if token.Provider == "" || token.Gen == 0 {
		return authpublic.LoginSupersededError{}
	}
	mu.Lock()
	defer mu.Unlock()
	s := slotFor(token.Provider)
	if !s.active || s.gen != token.Gen {
		return authpublic.LoginSupersededError{}
	}
	return nil
}

func Release(token Token) {
	mu.Lock()
	defer mu.Unlock()
	s := slotFor(token.Provider)
	if s.gen == token.Gen {
		s.active = false
	}
}

func Cancel(provider string) bool {
	mu.Lock()
	defer mu.Unlock()
	s := slotFor(provider)
	if !s.active {
		return false
	}
	s.gen++
	s.active = false
	return true
}

func BeginSettle(provider string) {
	mu.Lock()
	defer mu.Unlock()
	s := slotFor(provider)
	s.settling++
	if s.waiters == nil {
		s.waiters = make(chan struct{})
	}
}

func EndSettle(provider string) {
	mu.Lock()
	defer mu.Unlock()
	s := slotFor(provider)
	if s.settling > 0 {
		s.settling--
	}
	if s.settling == 0 && s.waiters != nil {
		close(s.waiters)
		s.waiters = nil
	}
}

func WaitNotSettling(ctx context.Context, provider string) error {
	for {
		mu.Lock()
		s := slotFor(provider)
		if s.settling == 0 {
			mu.Unlock()
			return nil
		}
		wait := s.waiters
		mu.Unlock()
		if wait == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wait:
		}
	}
}

func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	slots = map[string]*slot{}
}
