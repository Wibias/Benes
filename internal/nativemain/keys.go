package nativemain

import (
	"crypto/rand"
	"crypto/subtle"
	"sync"
)

type MemoryKeyProvider struct {
	mu   sync.Mutex
	keys map[string][]byte
}

func NewMemoryKeyProvider() *MemoryKeyProvider {
	return &MemoryKeyProvider{keys: map[string][]byte{}}
}

func (p *MemoryKeyProvider) Get(homeID string) (*Key, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	raw, ok := p.keys[homeID]
	if !ok {
		return nil, nil
	}
	cp := append([]byte(nil), raw...)
	return &Key{Ref: keyringService + ":" + homeID, Raw: cp}, nil
}

func (p *MemoryKeyProvider) Create(homeID string) (*Key, error) {
	if existing, err := p.Get(homeID); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fail("KEYRING_UNAVAILABLE", "The native OS credential store could not save the profile key.", 503)
	}
	p.mu.Lock()
	p.keys[homeID] = append([]byte(nil), raw...)
	p.mu.Unlock()
	return &Key{Ref: keyringService + ":" + homeID, Raw: raw}, nil
}

func equalKey(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
