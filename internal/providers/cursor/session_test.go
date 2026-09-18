package cursor

import (
	"errors"
	"testing"
)

func TestSessionForbidsReplayAfterDispatch(t *testing.T) {
	session := NewSession(HTTPVersion2)
	if err := session.MarkReachabilityFailure(); err != nil {
		t.Fatal(err)
	}
	session.CommitDispatch()
	if err := session.MarkReachabilityFailure(); !errors.Is(err, ErrRequestCommitted) {
		t.Fatalf("replay after commit=%v", err)
	}
}

func TestHTTP1AppendWaitsForRegistrationAndSequences(t *testing.T) {
	session := NewSession(HTTPVersion1Dot1)
	if err := session.Append("req-1", 1); !errors.Is(err, ErrAppendBeforeRegister) {
		t.Fatalf("early append=%v", err)
	}
	if err := session.Register("req-1"); err != nil {
		t.Fatal(err)
	}
	if err := session.Append("req-1", 1); err != nil {
		t.Fatal(err)
	}
	if err := session.Append("req-1", 1); !errors.Is(err, ErrSequenceGap) {
		t.Fatalf("dup seq=%v", err)
	}
	if err := session.Append("other", 2); err == nil {
		t.Fatal("identity mismatch")
	}
	if err := session.Append("req-1", 2); err != nil {
		t.Fatal(err)
	}
	if !session.Committed() {
		t.Fatal("append must commit")
	}
}
