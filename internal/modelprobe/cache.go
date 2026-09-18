package modelprobe

import (
	"strings"
	"sync"
	"time"
)

const DefaultTTL = 5 * time.Minute

type Cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]Result
}

func NewCache(ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Cache{ttl: ttl, entries: map[string]Result{}}
}

func CacheKey(provider, model, host, slot string) string {
	return strings.Join([]string{
		strings.TrimSpace(provider),
		strings.TrimSpace(model),
		strings.ToLower(strings.TrimSpace(host)),
		strings.TrimSpace(slot),
	}, "\x1f")
}

func (c *Cache) Lookup(provider, model, host, slot string) Result {
	if c == nil {
		return UnknownResult(provider, model)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	got, ok := c.entries[CacheKey(provider, model, host, slot)]
	if !ok {
		return UnknownResult(provider, model)
	}
	if c.ttl > 0 && time.Since(got.TestedAt) > c.ttl {
		out := UnknownResult(provider, model)
		out.DestinationHost = got.DestinationHost
		out.CredentialSlot = got.CredentialSlot
		out.Reason = "stale"
		return out
	}
	return got
}

func (c *Cache) Store(result Result) {
	if c == nil {
		return
	}
	result = result.Safe()
	if result.State == "" || result.TestedAt.IsZero() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]Result{}
	}
	c.entries[CacheKey(result.Provider, result.Model, result.DestinationHost, result.CredentialSlot)] = result
}
