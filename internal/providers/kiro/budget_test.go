package kiro

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

type rejectTripper struct {
	t *testing.T
}

func (r rejectTripper) RoundTrip(*http.Request) (*http.Response, error) {
	r.t.Fatal("compiled Kiro body must reserve translator bytes before dispatch")
	return nil, errors.New("unreachable")
}

func TestOpenReservesTranslatorBytesBeforeDispatch(t *testing.T) {
	mgr := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassTranslator: 4},
	})
	turn, err := mgr.AcquireTurn(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()
	client, err := NewHardened(context.Background(), Config{
		Account: AccountSnapshot{
			AccessToken: "tok",
			ProfileARN:  "arn:aws:codewhisperer:us-east-1:123456789012:profile/abc",
			APIRegion:   "us-east-1",
		},
		HTTPClient: &http.Client{Transport: rejectTripper{t: t}},
		Endpoint:   "https://runtime.us-east-1.kiro.dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Open(context.Background(), providers.DispatchRequest{
		Turn: turn,
		Parsed: protocol.ParsedRequest{
			UpstreamModelID: "claude-sonnet-4",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hello world"}},
			}}},
		},
	})
	if !errors.Is(err, resourcebudget.ErrClassBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestNextReservesStreamPendingBytes(t *testing.T) {
	mgr := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassStreamPending: 4},
	})
	turn, err := mgr.AcquireTurn(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()
	frame := EncodeEventStreamMessage(map[string]string{":event-type": "assistantResponseEvent"}, []byte(`{"content":"hello world"}`))
	s := &stream{body: io.NopCloser(bytes.NewReader(frame)), turn: turn, open: map[string]struct{}{}}
	defer s.Close()
	_, err = s.Next()
	if !errors.Is(err, resourcebudget.ErrClassBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestToolUseInputReservesToolArgumentBytes(t *testing.T) {
	mgr := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassToolArguments: 4},
	})
	turn, err := mgr.AcquireTurn(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer turn.Close()
	start := EncodeEventStreamMessage(map[string]string{":event-type": "toolUseEvent"}, []byte(`{"name":"lookup","toolUseId":"c1"}`))
	delta := EncodeEventStreamMessage(map[string]string{":event-type": "toolUseEvent"}, []byte(`{"name":"lookup","toolUseId":"c1","input":"{\"q\":\"hello world\"}"}`))
	s := &stream{buf: append(start, delta...), turn: turn, open: map[string]struct{}{}}
	ev, err := s.Next()
	if err != nil || ev.Type != protocol.EventToolCallStart {
		t.Fatalf("start=%#v err=%v", ev, err)
	}
	_, err = s.Next()
	if !errors.Is(err, resourcebudget.ErrClassBudgetExceeded) {
		t.Fatalf("err=%v", err)
	}
}
