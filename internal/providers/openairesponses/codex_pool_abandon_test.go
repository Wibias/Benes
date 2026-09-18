package openairesponses

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/protocol"
)

type poolAbandonStream struct{ closed bool }

func (*poolAbandonStream) Next() (protocol.Event, error) { return protocol.Event{}, io.EOF }
func (s *poolAbandonStream) Close() error { s.closed = true; return nil }

func TestCodexPoolAttemptObserverAbandonIsNeutralLeaseCleanup(t *testing.T) {
	outcomes := &fakePoolOutcomeRecorder{}
	lease := &codexauth.ProbeLease{AccountID: "acct", LeaseID: "lease", CooldownGeneration: 2}
	observer := newCodexPoolAttemptObserver(outcomes, codexauth.PoolCredential{
		AccountID: "acct", WriterGeneration: 4, ProbeLease: lease,
	})
	abandoner, ok := any(observer).(ForwardOutcomeAbandoner)
	if !ok {
		t.Fatal("Pool observer does not support neutral abandonment")
	}
	abandoner.Abandon()
	if len(outcomes.calls) != 1 || outcomes.calls[0].outcome.Kind != codexauth.OutcomeConnectNeutral || outcomes.calls[0].meta.ProbeLease != lease {
		t.Fatalf("calls=%#v", outcomes.calls)
	}
}

func TestObservedForwardStreamCallsPoolAbandonOnceOnClose(t *testing.T) {
	outcomes := &fakePoolOutcomeRecorder{}
	observer := newCodexPoolAttemptObserver(outcomes, codexauth.PoolCredential{AccountID: "acct"})
	inner := &poolAbandonStream{}
	stream := newObservedForwardStream(context.Background(), inner, observer)
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if !inner.closed {
		t.Fatal("inner stream was not closed")
	}
	if len(outcomes.calls) != 1 || outcomes.calls[0].outcome.Kind != codexauth.OutcomeConnectNeutral {
		t.Fatalf("calls=%#v", outcomes.calls)
	}
	if _, err := stream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next err=%v", err)
	}
	if len(outcomes.calls) != 1 {
		t.Fatalf("abandon duplicated: %#v", outcomes.calls)
	}
}