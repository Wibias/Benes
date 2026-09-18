package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestCopyBoundedStopsAtByteLimit(t *testing.T) {
	var destination bytes.Buffer
	copied, err := CopyBounded(context.Background(), &destination, io.NopCloser(bytes.NewBufferString("abcdefgh")), StreamLimits{
		MaxBytes: 5,
	})
	if copied != 5 {
		t.Fatalf("copied = %d, want 5", copied)
	}
	if destination.String() != "abcde" {
		t.Fatalf("destination = %q, want abcde", destination.String())
	}
	var streamErr *StreamError
	if !errors.As(err, &streamErr) || streamErr.Kind != StreamErrorByteLimit {
		t.Fatalf("error = %v, want byte-limit StreamError", err)
	}
}

func TestCopyBoundedClassifiesInactivityTimeout(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()

	started := time.Now()
	_, err := CopyBounded(context.Background(), io.Discard, reader, StreamLimits{
		InactivityTimeout: 40 * time.Millisecond,
	})
	if time.Since(started) > time.Second {
		t.Fatal("inactivity timeout did not stop the blocked read promptly")
	}
	var streamErr *StreamError
	if !errors.As(err, &streamErr) || streamErr.Kind != StreamErrorInactivityTimeout {
		t.Fatalf("error = %v, want inactivity-timeout StreamError", err)
	}
}

func TestCopyBoundedDoesNotTreatTotalDurationAsInactivity(t *testing.T) {
	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		for _, chunk := range []string{"a", "b", "c", "d"} {
			_, _ = io.WriteString(writer, chunk)
			time.Sleep(15 * time.Millisecond)
		}
	}()

	var destination bytes.Buffer
	copied, err := CopyBounded(context.Background(), &destination, reader, StreamLimits{
		InactivityTimeout: 35 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("CopyBounded(): %v", err)
	}
	if copied != 4 || destination.String() != "abcd" {
		t.Fatalf("copied=%d destination=%q", copied, destination.String())
	}
}

func TestCopyBoundedStopsWhenContextIsCancelled(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := CopyBounded(ctx, io.Discard, reader, StreamLimits{})
	var streamErr *StreamError
	if !errors.As(err, &streamErr) || streamErr.Kind != StreamErrorCancelled {
		t.Fatalf("error = %v, want cancelled StreamError", err)
	}
}

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

func TestCopyBoundedClosesSourceAfterCleanEOF(t *testing.T) {
	source := &trackingReadCloser{Reader: bytes.NewBufferString("ok")}
	var destination bytes.Buffer
	_, err := CopyBounded(context.Background(), &destination, source, StreamLimits{})
	if err != nil {
		t.Fatalf("CopyBounded(): %v", err)
	}
	if !source.closed {
		t.Fatal("source was not closed after clean EOF")
	}
}
