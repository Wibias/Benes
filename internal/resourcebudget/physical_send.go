package resourcebudget

type PhysicalSend struct {
	Ordinal int
	Reason  string
}

type PhysicalSendLease struct {
	turn      *Turn
	ordinal   int
	reason    string
	done      bool
	committed bool
}

func (t *Turn) ReservePhysicalSend(reason string) (*PhysicalSendLease, error) {
	if t == nil || t.manager == nil {
		return &PhysicalSendLease{}, nil
	}
	m := t.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.closed {
		return nil, ErrTurnClosed
	}
	if limit := m.limits.MaxPhysicalSends; limit > 0 && t.physicalSendCommitted+t.physicalSendReserved >= limit {
		return nil, ErrPhysicalSendBudgetExceeded
	}
	t.nextPhysicalSendOrdinal++
	t.physicalSendReserved++
	return &PhysicalSendLease{
		turn:    t,
		ordinal: t.nextPhysicalSendOrdinal,
		reason:  reason,
	}, nil
}

func (l *PhysicalSendLease) Commit() error {
	if l == nil || l.turn == nil || l.turn.manager == nil {
		return nil
	}
	t := l.turn
	m := t.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.done {
		if l.committed {
			return nil
		}
		return ErrPhysicalSendLeaseReleased
	}
	if t.closed {
		if t.physicalSendReserved > 0 {
			t.physicalSendReserved--
		}
		l.done = true
		return ErrTurnClosed
	}
	if t.physicalSendReserved > 0 {
		t.physicalSendReserved--
	}
	t.physicalSendCommitted++
	t.physicalSendRecords = append(t.physicalSendRecords, PhysicalSend{
		Ordinal: l.ordinal,
		Reason:  l.reason,
	})
	l.done = true
	l.committed = true
	return nil
}

func (l *PhysicalSendLease) Release() {
	if l == nil || l.turn == nil || l.turn.manager == nil {
		return
	}
	t := l.turn
	m := t.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.done {
		return
	}
	if !t.closed && t.physicalSendReserved > 0 {
		t.physicalSendReserved--
	}
	l.done = true
}

func (t *Turn) PhysicalSends() []PhysicalSend {
	if t == nil || t.manager == nil {
		return nil
	}
	m := t.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]PhysicalSend, len(t.physicalSendRecords))
	copy(out, t.physicalSendRecords)
	return out
}
