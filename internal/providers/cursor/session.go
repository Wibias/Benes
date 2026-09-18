package cursor

import (
	"errors"
	"fmt"
	"sync"
)

var (
	ErrRequestCommitted     = errors.New("Cursor request is committed and is not replayable")
	ErrAppendBeforeRegister = errors.New("Cursor HTTP/1.1 append arrived before RunSSE registration")
	ErrSequenceGap          = errors.New("Cursor HTTP/1.1 append sequence is not monotonic")
)

type Session struct {
	mu          sync.Mutex
	committed   bool
	requestID   string
	nextSeq     uint64
	httpVersion HTTPVersion
}

func NewSession(version HTTPVersion) *Session {
	if version == "" {
		version = HTTPVersion2
	}
	return &Session{httpVersion: version}
}

func (s *Session) MarkReachabilityFailure() error {
	if s == nil {
		return fmt.Errorf("cursor session is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.committed {
		return ErrRequestCommitted
	}
	return nil
}

func (s *Session) CommitDispatch() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.committed = true
	s.mu.Unlock()
}

func (s *Session) Committed() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.committed
}

func (s *Session) Register(requestID string) error {
	if s == nil {
		return fmt.Errorf("cursor session is required")
	}
	if requestID == "" {
		return fmt.Errorf("Cursor RunSSE request id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requestID = requestID
	s.nextSeq = 1
	return nil
}

func (s *Session) Append(requestID string, seq uint64) error {
	if s == nil {
		return fmt.Errorf("cursor session is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.httpVersion == HTTPVersion1Dot1 && s.requestID == "" {
		return ErrAppendBeforeRegister
	}
	if s.requestID != "" && requestID != s.requestID {
		return fmt.Errorf("Cursor append request id does not match the registered session")
	}
	if seq != s.nextSeq {
		return ErrSequenceGap
	}
	s.nextSeq++
	s.committed = true
	return nil
}

func (s *Session) AllocateAppend(requestID string) (uint64, error) {
	if s == nil {
		return 0, fmt.Errorf("cursor session is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.httpVersion == HTTPVersion1Dot1 && s.requestID == "" {
		return 0, ErrAppendBeforeRegister
	}
	if s.requestID != "" && requestID != s.requestID {
		return 0, fmt.Errorf("Cursor append request id does not match the registered session")
	}
	seq := s.nextSeq
	if seq == 0 {
		seq = 1
	}
	s.nextSeq = seq + 1
	s.committed = true
	return seq, nil
}
