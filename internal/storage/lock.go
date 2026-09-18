package storage

import "sync"

type Coordinator struct {
	mu     sync.Mutex
	holder string
}

func NewCoordinator() *Coordinator {
	return &Coordinator{}
}

func (c *Coordinator) TryAcquire(op string) bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.holder != "" {
		return false
	}
	c.holder = op
	return true
}

func (c *Coordinator) Release() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.holder = ""
	c.mu.Unlock()
}

func (c *Coordinator) Holder() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.holder
}
