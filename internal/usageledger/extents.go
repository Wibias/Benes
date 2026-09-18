package usageledger

import "fmt"

// Extents describes the absolute committed ledger address space.
// Offsets never renumber when a retained prefix is deleted.
//
// LogicalSize() returns EndOffset. RetainedBytes is the readable
// committed span EndOffset-RetainedFromOffset after durable retention.
type Extents struct {
	RetainedFromOffset int64 `json:"retainedFromOffset"`
	EndOffset          int64 `json:"endOffset"`
	RetainedBytes      int64 `json:"retainedBytes"`
}

func (l *Ledger) Extents() (Extents, error) {
	if l == nil {
		return Extents{}, fmt.Errorf("ledger is nil")
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.extentsLocked()
}

func (l *Ledger) extentsLocked() (Extents, error) {
	state, err := l.loadRetentionStateLocked()
	if err != nil {
		return Extents{}, err
	}
	opened, err := l.openSourcesRetained(state)
	if err != nil {
		return Extents{}, err
	}
	defer closeOpened(opened)
	var bytes int64
	for _, src := range opened {
		bytes += src.size
	}
	from := state.RetainedFromOffset
	if from < 0 {
		from = 0
	}
	return Extents{
		RetainedFromOffset: from,
		EndOffset:          from + bytes,
		RetainedBytes:      bytes,
	}, nil
}
