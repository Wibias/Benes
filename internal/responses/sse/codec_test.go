package sse

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestDecoderParsesWHATWGFraming(t *testing.T) {
	input := "\ufeff: comment\r\nevent: custom\r\nid: 7\r\ndata: first\r\ndata:second\r\nretry: 1500\r\n\r\n"
	dec := NewDecoder(strings.NewReader(input), Limits{MaxLineBytes: 1024, MaxEventBytes: 4096})
	event, err := dec.Next()
	if err != nil {
		t.Fatalf("Next(): %v", err)
	}
	if event.Type != "custom" || event.ID != "7" || event.Data != "first\nsecond" || event.RetryMilliseconds != 1500 {
		t.Fatalf("event=%#v", event)
	}
	if _, err := dec.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("second Next err=%v", err)
	}
}

func TestDecoderAcceptsLoneCRLineEndings(t *testing.T) {
	dec := NewDecoder(strings.NewReader("data: a\rdata: b\r\r"), Limits{})
	event, err := dec.Next()
	if err != nil {
		t.Fatalf("Next(): %v", err)
	}
	if event.Data != "a\nb" {
		t.Fatalf("data=%q", event.Data)
	}
}

func TestDecoderDiscardsIncompleteEventAtEOF(t *testing.T) {
	dec := NewDecoder(strings.NewReader("data: never-dispatch"), Limits{})
	if _, err := dec.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("err=%v, want EOF", err)
	}
}

func TestDecoderIgnoresIDContainingNULAndInvalidRetry(t *testing.T) {
	dec := NewDecoder(strings.NewReader("id: good\n\ndata: first\n\nid: bad\x00id\nretry: 1x\ndata: second\n\n"), Limits{})
	first, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "good" {
		t.Fatalf("first ID=%q", first.ID)
	}
	second, err := dec.Next()
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != "good" {
		t.Fatalf("second ID=%q, want inherited good", second.ID)
	}
	if second.RetryMilliseconds != 0 {
		t.Fatalf("retry=%d", second.RetryMilliseconds)
	}
}

func TestDecoderSkipsBlocksWithoutDataButRetainsID(t *testing.T) {
	dec := NewDecoder(strings.NewReader("id: 42\n\ndata: payload\n\n"), Limits{})
	event, err := dec.Next()
	if err != nil {
		t.Fatalf("Next(): %v", err)
	}
	if event.ID != "42" || event.Data != "payload" {
		t.Fatalf("event=%#v", event)
	}
}

func TestDecoderRejectsOversizedLineAndEvent(t *testing.T) {
	dec := NewDecoder(strings.NewReader("data: 123456\n\n"), Limits{MaxLineBytes: 8, MaxEventBytes: 100})
	if _, err := dec.Next(); !errors.Is(err, ErrLineTooLarge) {
		t.Fatalf("err=%v", err)
	}
	dec = NewDecoder(strings.NewReader("data: 1234\ndata: 5678\n\n"), Limits{MaxLineBytes: 100, MaxEventBytes: 7})
	if _, err := dec.Next(); !errors.Is(err, ErrEventTooLarge) {
		t.Fatalf("err=%v", err)
	}
}

func TestEncoderWritesJSONAsSingleDataEvent(t *testing.T) {
	var out bytes.Buffer
	if err := WriteJSON(&out, map[string]any{"type": "response.output_text.delta", "delta": "a\nb"}); err != nil {
		t.Fatalf("WriteJSON(): %v", err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "data: {") || !strings.HasSuffix(got, "\n\n") {
		t.Fatalf("wire=%q", got)
	}
	if strings.Contains(got, "a\nb") {
		t.Fatalf("JSON newline was emitted literally: %q", got)
	}
	if !strings.Contains(got, `a\nb`) {
		t.Fatalf("wire=%q missing escaped newline", got)
	}
}

func TestDecoderResetsEventTypeOnBlankBlockWithoutData(t *testing.T) {
	dec := NewDecoder(strings.NewReader("event: custom\n\ndata: payload\n\n"), Limits{})
	event, err := dec.Next()
	if err != nil {
		t.Fatalf("Next(): %v", err)
	}
	if event.Type != "message" {
		t.Fatalf("type=%q, want message", event.Type)
	}
}

func TestDecoderCarriesRetryStateAcrossDataLessBlock(t *testing.T) {
	dec := NewDecoder(strings.NewReader("retry: 2500\n\ndata: payload\n\n"), Limits{})
	event, err := dec.Next()
	if err != nil {
		t.Fatalf("Next(): %v", err)
	}
	if event.RetryMilliseconds != 2500 {
		t.Fatalf("retry=%d, want 2500", event.RetryMilliseconds)
	}
}
