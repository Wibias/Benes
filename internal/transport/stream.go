package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

type StreamErrorKind string

const (
	StreamErrorInactivityTimeout StreamErrorKind = "inactivity_timeout"
	StreamErrorByteLimit         StreamErrorKind = "output_byte_limit"
	StreamErrorCancelled         StreamErrorKind = "cancelled"
	StreamErrorReadFailure       StreamErrorKind = "read_failure"
	StreamErrorWriteFailure      StreamErrorKind = "write_failure"
)

type StreamError struct {
	Kind StreamErrorKind
	Err  error
}

func (e *StreamError) Error() string {
	if e.Err == nil {
		return string(e.Kind)
	}
	return fmt.Sprintf("%s: %v", e.Kind, e.Err)
}

func (e *StreamError) Unwrap() error {
	return e.Err
}

type StreamLimits struct {
	MaxBytes          int64
	InactivityTimeout time.Duration
	BufferSize        int
}

type readResult struct {
	n   int
	err error
}

func CopyBounded(parent context.Context, destination io.Writer, source io.ReadCloser, limits StreamLimits) (int64, error) {
	defer source.Close()
	if err := parent.Err(); err != nil {
		_ = source.Close()
		return 0, &StreamError{Kind: StreamErrorCancelled, Err: err}
	}

	bufferSize := limits.BufferSize
	if bufferSize <= 0 {
		bufferSize = 32 * 1024
	}
	if bufferSize > 256*1024 {
		bufferSize = 256 * 1024
	}
	buffer := make([]byte, bufferSize)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	results := make(chan readResult)
	acknowledge := make(chan struct{})
	go func() {
		for {
			n, err := source.Read(buffer)
			select {
			case results <- readResult{n: n, err: err}:
			case <-ctx.Done():
				return
			}
			if n > 0 {
				select {
				case <-acknowledge:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	var timer *time.Timer
	var timerChannel <-chan time.Time
	if limits.InactivityTimeout > 0 {
		timer = time.NewTimer(limits.InactivityTimeout)
		timerChannel = timer.C
		defer timer.Stop()
	}
	resetTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(limits.InactivityTimeout)
	}

	var copied int64
	for {
		select {
		case <-parent.Done():
			cancel()
			_ = source.Close()
			return copied, &StreamError{Kind: StreamErrorCancelled, Err: parent.Err()}
		case <-timerChannel:
			cancel()
			_ = source.Close()
			return copied, &StreamError{Kind: StreamErrorInactivityTimeout, Err: context.DeadlineExceeded}
		case result := <-results:
			if result.n > 0 {
				resetTimer()
				writeCount := result.n
				limitExceeded := false
				if limits.MaxBytes > 0 && copied+int64(writeCount) > limits.MaxBytes {
					writeCount = int(limits.MaxBytes - copied)
					if writeCount < 0 {
						writeCount = 0
					}
					limitExceeded = true
				}
				if writeCount > 0 {
					n, err := destination.Write(buffer[:writeCount])
					copied += int64(n)
					if err != nil {
						cancel()
						_ = source.Close()
						return copied, &StreamError{Kind: StreamErrorWriteFailure, Err: err}
					}
					if n != writeCount {
						cancel()
						_ = source.Close()
						return copied, &StreamError{Kind: StreamErrorWriteFailure, Err: io.ErrShortWrite}
					}
				}
				if limitExceeded {
					cancel()
					_ = source.Close()
					return copied, &StreamError{Kind: StreamErrorByteLimit}
				}
				select {
				case acknowledge <- struct{}{}:
				case <-parent.Done():
					cancel()
					_ = source.Close()
					return copied, &StreamError{Kind: StreamErrorCancelled, Err: parent.Err()}
				}
			}

			if result.err != nil {
				if errors.Is(result.err, io.EOF) {
					return copied, nil
				}
				return copied, &StreamError{Kind: StreamErrorReadFailure, Err: result.err}
			}
		}
	}
}
