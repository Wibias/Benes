package provideractivity

import "sync"

const maxEvents = 64

type Event struct {
	Provider  string `json:"provider"`
	Type      string `json:"type"`
	Detail    string `json:"detail,omitempty"`
	Severity  string `json:"severity"`
	Timestamp int64  `json:"timestamp"`
}

type Log struct {
	mu     sync.Mutex
	events []Event
}

func New() *Log {
	return &Log{events: make([]Event, 0, maxEvents)}
}

func (l *Log) Record(event Event) {
	if l == nil {
		return
	}
	if event.Severity == "" {
		event.Severity = "info"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.events) == maxEvents {
		copy(l.events, l.events[1:])
		l.events[maxEvents-1] = event
		return
	}
	l.events = append(l.events, event)
}

func (l *Log) Recent(n int) []Event {
	if l == nil {
		return nil
	}
	if n <= 0 {
		n = 20
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.events) == 0 {
		return nil
	}
	start := 0
	if len(l.events) > n {
		start = len(l.events) - n
	}
	out := make([]Event, len(l.events)-start)
	copy(out, l.events[start:])
	return out
}

func (l *Log) RecentFor(provider string, n int) []Event {
	all := l.Recent(maxEvents)
	if provider == "" {
		return trimEvents(all, n)
	}
	filtered := make([]Event, 0, len(all))
	for _, event := range all {
		if event.Provider == provider {
			filtered = append(filtered, event)
		}
	}
	return trimEvents(filtered, n)
}

func trimEvents(events []Event, n int) []Event {
	if n <= 0 || len(events) <= n {
		return events
	}
	return events[len(events)-n:]
}
