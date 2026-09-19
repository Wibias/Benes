package vision

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/sidecar"
)

type fakeStream struct {
	events []protocol.Event
	i      int
}

func (s *fakeStream) Next() (protocol.Event, error) {
	if s.i >= len(s.events) {
		return protocol.Event{}, io.EOF
	}
	ev := s.events[s.i]
	s.i++
	return ev, nil
}

func (s *fakeStream) Close() error { return nil }

type fakeProvider struct {
	last providers.DispatchRequest
	err  error
	evs  []protocol.Event
}

func (p *fakeProvider) Open(_ context.Context, req providers.DispatchRequest) (providers.EventStream, error) {
	p.last = req
	if p.err != nil {
		return nil, p.err
	}
	return &fakeStream{events: p.evs}, nil
}

func pngDataURL() string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nxxxx"))
}

func proven() sidecar.Candidate {
	return sidecar.Candidate{Modality: sidecar.ModalityVisionDescribe, Class: sidecar.ClassVisionDescribe, ProviderID: "openai-apikey", ModelID: "gpt-4o", ImageInput: true, Proven: true}
}

func TestDescribeGenericVisionProvider(t *testing.T) {
	p := &fakeProvider{evs: []protocol.Event{{Type: protocol.EventTextDelta, Text: "A red square."}, {Type: protocol.EventDone}}}
	got, err := New(p, 0, 0, 0).Describe(context.Background(), proven(), Request{Images: []string{pngDataURL()}})
	if err != nil || got.Description != "A red square." {
		t.Fatalf("%v %#v", err, got)
	}
	if p.last.Parsed.Context.Messages[0].Content[1].Type != protocol.ContentImage {
		t.Fatalf("missing image part")
	}
	if got.Description == pngDataURL() {
		t.Fatal("raw image returned as description")
	}
}

func TestDescribeRejectsTextOnlyAndUnknown(t *testing.T) {
	p := &fakeProvider{evs: []protocol.Event{{Type: protocol.EventTextDelta, Text: "nope"}}}
	blind := proven()
	blind.ImageInput = false
	if _, err := New(p, 0, 0, 0).Describe(context.Background(), blind, Request{Images: []string{pngDataURL()}}); err != sidecar.ErrUnprovenCapability {
		t.Fatalf("blind=%v", err)
	}
	unknown := proven()
	unknown.Proven = false
	if _, err := New(p, 0, 0, 0).Describe(context.Background(), unknown, Request{Images: []string{pngDataURL()}}); err != sidecar.ErrUnprovenCapability {
		t.Fatalf("unknown=%v", err)
	}
}

func TestDescribeBoundsTimeoutCancelAndQuota(t *testing.T) {
	huge := "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, DefaultMaxImageBytes+1))
	p := &fakeProvider{evs: []protocol.Event{{Type: protocol.EventError, HTTPStatus: 429, Message: "quota"}}}
	client := New(p, 1, 0, 8)
	if _, err := client.Describe(context.Background(), proven(), Request{Images: []string{pngDataURL(), pngDataURL()}}); err != ErrTooManyImages {
		t.Fatalf("count=%v", err)
	}
	if _, err := client.Describe(context.Background(), proven(), Request{Images: []string{huge}}); err != ErrImageTooLarge {
		t.Fatalf("bytes=%v", err)
	}
	if _, err := client.Describe(context.Background(), proven(), Request{Images: []string{pngDataURL()}}); err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("quota=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Describe(ctx, proven(), Request{Images: []string{pngDataURL()}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	deadline, stop := context.WithTimeout(context.Background(), time.Nanosecond)
	defer stop()
	<-deadline.Done()
	if _, err := New(&fakeProvider{evs: []protocol.Event{{Type: protocol.EventTextDelta, Text: "late"}}}, 1, 0, 8).Describe(deadline, proven(), Request{Images: []string{pngDataURL()}}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout=%v", err)
	}
}

func TestDescribeFailsClosedOnEOFBeforeDone(t *testing.T) {
	p := &fakeProvider{evs: []protocol.Event{{Type: protocol.EventTextDelta, Text: "partial description"}}}
	got, err := New(p, 0, 0, 0).Describe(context.Background(), proven(), Request{Images: []string{pngDataURL()}})
	if err == nil {
		t.Fatalf("unexpected success: %#v", got)
	}
	if got.Description != "" {
		t.Fatalf("partial description escaped: %#v", got)
	}
}

func TestDescribeFailsClosedWhenDescriptionLimitExceeded(t *testing.T) {
	p := &fakeProvider{evs: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "12345"},
		{Type: protocol.EventDone},
	}}
	got, err := New(p, 0, 0, 4).Describe(context.Background(), proven(), Request{Images: []string{pngDataURL()}})
	if err == nil {
		t.Fatalf("unexpected clipped success: %#v", got)
	}
	if got.Description != "" {
		t.Fatalf("clipped description escaped: %#v", got)
	}
}

func TestDescribeAllowsExactDescriptionLimitAfterDone(t *testing.T) {
	p := &fakeProvider{evs: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "12345"},
		{Type: protocol.EventDone},
	}}
	got, err := New(p, 0, 0, 5).Describe(context.Background(), proven(), Request{Images: []string{pngDataURL()}})
	if err != nil || got.Description != "12345" {
		t.Fatalf("err=%v got=%#v", err, got)
	}
}
