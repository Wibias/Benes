package resourcebudget

import "sync"

type Accumulator struct {
	mu sync.Mutex

	turn  *Turn
	class Class

	chunks [][]byte
	leases []*Reservation
	joined []byte
	dirty  bool
	closed bool
}

func NewAccumulator(turn *Turn, class Class) *Accumulator {
	return &Accumulator{turn: turn, class: class}
}

func (a *Accumulator) Append(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.turn == nil {
		return ErrTurnClosed
	}
	lease, err := a.turn.Reserve(a.class, int64(len(chunk)))
	if err != nil {
		return err
	}
	copyChunk := append([]byte(nil), chunk...)
	a.chunks = append(a.chunks, copyChunk)
	a.leases = append(a.leases, lease)
	a.joined = nil
	a.dirty = true
	return nil
}

func (a *Accumulator) Bytes() []byte {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dirty {
		total := 0
		for _, chunk := range a.chunks {
			total += len(chunk)
		}
		a.joined = make([]byte, 0, total)
		for _, chunk := range a.chunks {
			a.joined = append(a.joined, chunk...)
		}
		a.dirty = false
	}
	return append([]byte(nil), a.joined...)
}

func (a *Accumulator) Close() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	leases := a.leases
	a.leases = nil
	a.chunks = nil
	a.joined = nil
	a.dirty = false
	a.mu.Unlock()
	for _, lease := range leases {
		lease.Release()
	}
	return nil
}
