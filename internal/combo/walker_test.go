package combo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/timeline"
)

type recordingProvider struct {
	id        string
	err       error
	streamErr error
	events    []protocol.Event
	mu        sync.Mutex
	models    []string
	opens     int
	closed    int
	attempts  []int
}

func (p *recordingProvider) Open(ctx context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	p.mu.Lock()
	p.opens++
	p.models = append(p.models, dispatch.Parsed.UpstreamModelID)
	if tr := timeline.FromContext(ctx); tr != nil {
		p.attempts = append(p.attempts, tr.Attempt())
	}
	p.mu.Unlock()
	if p.err != nil {
		return nil, p.err
	}
	return &recordingStream{
		parent:  p,
		events:  append([]protocol.Event(nil), p.events...),
		nextErr: p.streamErr,
	}, nil
}

type recordingStream struct {
	parent  *recordingProvider
	events  []protocol.Event
	nextErr error
}

func (s *recordingStream) Next() (protocol.Event, error) {
	if len(s.events) == 0 {
		if s.nextErr != nil {
			return protocol.Event{}, s.nextErr
		}
		return protocol.Event{}, io.EOF
	}
	event := s.events[0]
	s.events = s.events[1:]
	return event, nil
}

func (s *recordingStream) Close() error {
	s.parent.mu.Lock()
	s.parent.closed++
	s.parent.mu.Unlock()
	return nil
}

func TestWalkerOpensNextMemberOnHopAndRewritesUpstreamModel(t *testing.T) {
	first := &recordingProvider{id: "g", err: fmt.Errorf("Google GenerateContent returned HTTP 503")}
	second := &recordingProvider{id: "o", events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "ok"}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "g", Protocol: "google"}, Model: "gemini-flash", Provider: first},
		{Member: Member{ID: "o", Protocol: "openai-chat"}, Model: "gpt-5.4", Provider: second},
	}}
	stream, err := walker.Open(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{ModelID: "combo/fast", UpstreamModelID: "fast"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if first.opens != 1 || second.opens != 1 {
		t.Fatalf("opens first=%d second=%d", first.opens, second.opens)
	}
	if len(first.models) != 1 || first.models[0] != "gemini-flash" {
		t.Fatalf("first model=%v", first.models)
	}
	if len(second.models) != 1 || second.models[0] != "gpt-5.4" {
		t.Fatalf("second model=%v", second.models)
	}
	event, err := stream.Next()
	if err != nil || event.Text != "ok" {
		t.Fatalf("event=%#v err=%v", event, err)
	}
	_ = stream.Close()
	committed, err := walker.OpenCommitted(context.Background(), providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{UpstreamModelID: "fast"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer committed.Close()
	if first.opens != 1 || second.opens != 2 {
		t.Fatalf("committed opens first=%d second=%d", first.opens, second.opens)
	}
}

func TestWalkerStopsOnInvalidRequestAndDoesNotOpenLaterMembers(t *testing.T) {
	first := &recordingProvider{err: fmt.Errorf("OpenAI Responses upstream returned HTTP 400")}
	second := &recordingProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "nope"}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "a", Protocol: "openai-chat"}, Model: "a", Provider: first},
		{Member: Member{ID: "b", Protocol: "openai-chat"}, Model: "b", Provider: second},
	}}
	_, err := walker.Open(context.Background(), providers.DispatchRequest{})
	if err == nil || second.opens != 0 {
		t.Fatalf("err=%v second.opens=%d", err, second.opens)
	}
}

func TestWalkerStampsPerMemberAttemptOnTheRequestTrace(t *testing.T) {
	tr := timeline.New("req-walk", 8)
	ctx := timeline.WithTrace(context.Background(), tr)
	first := &recordingProvider{err: fmt.Errorf("upstream returned HTTP 429")}
	second := &recordingProvider{events: []protocol.Event{{Type: protocol.EventDone}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "a", Protocol: "cursor"}, Model: "a", Provider: first},
		{Member: Member{ID: "b", Protocol: "openai-chat"}, Model: "b", Provider: second},
	}}
	stream, err := walker.Open(ctx, providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if len(first.attempts) != 1 || first.attempts[0] != 1 {
		t.Fatalf("first attempts=%v", first.attempts)
	}
	if len(second.attempts) != 1 || second.attempts[0] != 2 {
		t.Fatalf("second attempts=%v", second.attempts)
	}
}

func TestWalkerHonorsCancelBeforeOpening(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	first := &recordingProvider{events: []protocol.Event{{Type: protocol.EventDone}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "a", Protocol: "google"}, Model: "a", Provider: first},
	}}
	_, err := walker.Open(ctx, providers.DispatchRequest{})
	if !errors.Is(err, context.Canceled) || first.opens != 0 {
		t.Fatalf("err=%v opens=%d", err, first.opens)
	}
}

func TestWalkerClassifiesCanceledOpenAsStop(t *testing.T) {
	first := &recordingProvider{err: context.Canceled}
	second := &recordingProvider{events: []protocol.Event{{Type: protocol.EventDone}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "a", Protocol: "openai-chat"}, Model: "a", Provider: first},
		{Member: Member{ID: "b", Protocol: "openai-chat"}, Model: "b", Provider: second},
	}}
	_, err := walker.Open(context.Background(), providers.DispatchRequest{})
	if !errors.Is(err, context.Canceled) || second.opens != 0 {
		t.Fatalf("err=%v second.opens=%d", err, second.opens)
	}
}

func TestWalkerHopsWhenOpenedStreamFailsBeforeVisibleEvent(t *testing.T) {
	first := &recordingProvider{
		id:        "a",
		streamErr: fmt.Errorf("OpenAI Responses forward upstream returned HTTP 503"),
	}
	second := &recordingProvider{id: "b", events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "ok"}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "a", Protocol: "openai-chat"}, Model: "a", Provider: first},
		{Member: Member{ID: "b", Protocol: "openai-chat"}, Model: "b", Provider: second},
	}}
	stream, err := walker.Open(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if first.opens != 1 || second.opens != 1 {
		t.Fatalf("opens first=%d second=%d", first.opens, second.opens)
	}
	if first.closed != 1 {
		t.Fatalf("failed stream was not closed before hop: closed=%d", first.closed)
	}
	event, err := stream.Next()
	if err != nil || event.Type != protocol.EventTextDelta || event.Text != "ok" {
		t.Fatalf("event=%#v err=%v", event, err)
	}
}

func TestWalkerDoesNotHopAfterVisibleOutput(t *testing.T) {
	first := &recordingProvider{
		id: "a",
		events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "partial"},
		},
		streamErr: fmt.Errorf("OpenAI Responses forward upstream returned HTTP 503"),
	}
	second := &recordingProvider{id: "b", events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "other"}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "a", Protocol: "openai-chat"}, Model: "a", Provider: first},
		{Member: Member{ID: "b", Protocol: "openai-chat"}, Model: "b", Provider: second},
	}}
	stream, err := walker.Open(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if first.opens != 1 || second.opens != 0 {
		t.Fatalf("opens first=%d second=%d", first.opens, second.opens)
	}
	event, err := stream.Next()
	if err != nil || event.Text != "partial" {
		t.Fatalf("event=%#v err=%v", event, err)
	}
	if _, err := stream.Next(); err == nil {
		t.Fatal("expected committed member failure after visible output")
	}
}

func TestWalkerHopsOnHTTP410OpenError(t *testing.T) {
	first := &recordingProvider{err: fmt.Errorf("upstream returned HTTP 410")}
	second := &recordingProvider{events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "ok"}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "a", Protocol: "openai-chat"}, Model: "a", Provider: first},
		{Member: Member{ID: "b", Protocol: "openai-chat"}, Model: "b", Provider: second},
	}}
	stream, err := walker.Open(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if first.opens != 1 || second.opens != 1 {
		t.Fatalf("opens first=%d second=%d", first.opens, second.opens)
	}
	event, err := stream.Next()
	if err != nil || event.Text != "ok" {
		t.Fatalf("event=%#v err=%v", event, err)
	}
}

func TestWalkerRequiresAtLeastOneTarget(t *testing.T) {
	_, err := (&Walker{}).Open(context.Background(), providers.DispatchRequest{})
	if err == nil {
		t.Fatal("expected empty walker to fail closed")
	}
}

func TestWalkerHandsEachChildAnImmutableParentSnapshot(t *testing.T) {
	first := &mutatingProvider{err: fmt.Errorf("Google GenerateContent returned HTTP 503")}
	second := &mutatingProvider{}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "g", Protocol: "google"}, Model: "gemini-flash", Provider: first},
		{Member: Member{ID: "o", Protocol: "openai-chat"}, Model: "gpt-5.4", Provider: second},
	}}
	parent := providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{
			ModelID:            "combo/fast",
			PreviousResponseID: "resp_parent",
			Context: protocol.Context{Messages: []protocol.Message{{
				Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hello"}},
			}}},
		},
	}
	stream, err := walker.Open(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if parent.Parsed.PreviousResponseID != "resp_parent" || len(parent.Parsed.Context.Messages) != 1 {
		t.Fatalf("parent mutated: %#v", parent.Parsed)
	}
	if first.previousID != "resp_parent" || second.previousID != "resp_parent" {
		t.Fatalf("children previous=%q %q", first.previousID, second.previousID)
	}
	if first.userText != "hello" || second.userText != "hello" {
		t.Fatalf("children text=%q %q", first.userText, second.userText)
	}
	if first.model != "gemini-flash" || second.model != "gpt-5.4" {
		t.Fatalf("models=%q %q", first.model, second.model)
	}
}

func TestWalkerRecordsAttemptsWithoutSplittingTheRequest(t *testing.T) {
	first := &recordingProvider{id: "g", err: fmt.Errorf("Google GenerateContent returned HTTP 503")}
	second := &recordingProvider{id: "o", events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "ok"}}}
	walker := &Walker{Targets: []Target{
		{Member: Member{ID: "g", Protocol: "google"}, Model: "gemini-flash", Provider: first},
		{Member: Member{ID: "o", Protocol: "openai-chat"}, Model: "gpt-5.4", Provider: second},
	}}
	stream, err := walker.Open(context.Background(), providers.DispatchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if walker.CommittedID() != "o" {
		t.Fatalf("committed=%q", walker.CommittedID())
	}
	attempts := walker.Attempts()
	if len(attempts) != 2 || attempts[0].Decision != DecisionHop || attempts[1].Decision != DecisionCommitted {
		t.Fatalf("attempts=%#v", attempts)
	}
}

type mutatingProvider struct {
	err        error
	previousID string
	userText   string
	model      string
}

func (p *mutatingProvider) Open(_ context.Context, dispatch providers.DispatchRequest) (providers.EventStream, error) {
	p.previousID = dispatch.Parsed.PreviousResponseID
	p.model = dispatch.Parsed.UpstreamModelID
	if len(dispatch.Parsed.Context.Messages) > 0 && len(dispatch.Parsed.Context.Messages[0].Content) > 0 {
		p.userText = dispatch.Parsed.Context.Messages[0].Content[0].Text
	}
	dispatch.Parsed.PreviousResponseID = "resp_child"
	dispatch.Parsed.Context.Messages = append(dispatch.Parsed.Context.Messages, protocol.Message{
		Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "mutated"}},
	})
	if len(dispatch.Parsed.Context.Messages) > 0 && len(dispatch.Parsed.Context.Messages[0].Content) > 0 {
		dispatch.Parsed.Context.Messages[0].Content[0].Text = "mutated-user"
		dispatch.Parsed.Context.Messages[0].Content[0].ThoughtSignature = "leaked"
	}
	if p.err != nil {
		return nil, p.err
	}
	return &recordingStream{parent: &recordingProvider{}, events: []protocol.Event{{Type: protocol.EventTextDelta, Text: "ok"}}}, nil
}
