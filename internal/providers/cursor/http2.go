package cursor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Wibias/Benes/internal/resourcebudget"
)

const maxLiveWriteBytes = 1 << 20

var (
	errNoWriteAfterTerminal = errors.New("Cursor live write is forbidden after teardown")
	errReplyTooLarge        = errors.New("Cursor live write exceeds byte cap")
	errBlobMissing          = errors.New("Cursor blob is not available")
)

func (c *Client) openHTTP2(ctx context.Context, frame []byte, model, digest string, turn *resourcebudget.Turn) (*stream, error) {
	pr, pw := io.Pipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+runPath, pr)
	if err != nil {
		_ = pw.Close()
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/connect+proto")
	req.Header.Set("Connect-Protocol-Version", "1")

	type outcome struct {
		resp *http.Response
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		resp, err := c.httpClient.Do(req)
		done <- outcome{resp: resp, err: err}
	}()

	writeDone := make(chan error, 1)
	go func() {
		_, err := pw.Write(frame)
		writeDone <- err
	}()

	var res outcome
	select {
	case <-ctx.Done():
		_ = pw.CloseWithError(ctx.Err())
		<-done
		return nil, ctx.Err()
	case err := <-writeDone:
		if err != nil {
			_ = pw.CloseWithError(err)
			res = <-done
			if res.resp != nil {
				_ = res.resp.Body.Close()
			}
			if res.err != nil {
				if replayErr := c.session.MarkReachabilityFailure(); replayErr != nil {
					return nil, replayErr
				}
				return nil, res.err
			}
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = pw.CloseWithError(ctx.Err())
			res = <-done
			if res.resp != nil {
				_ = res.resp.Body.Close()
			}
			return nil, ctx.Err()
		case res = <-done:
		}
	case res = <-done:
		if res.err != nil {
			_ = pw.CloseWithError(res.err)
			if replayErr := c.session.MarkReachabilityFailure(); replayErr != nil {
				return nil, replayErr
			}
			return nil, res.err
		}
	}
	if res.err != nil {
		_ = pw.CloseWithError(res.err)
		if replayErr := c.session.MarkReachabilityFailure(); replayErr != nil {
			return nil, replayErr
		}
		return nil, res.err
	}
	c.session.CommitDispatch()
	if res.resp.StatusCode < 200 || res.resp.StatusCode >= 300 {
		_ = pw.Close()
		_ = res.resp.Body.Close()
		return nil, fmt.Errorf("Cursor upstream returned HTTP %d", res.resp.StatusCode)
	}
	out := &stream{
		body:        res.resp.Body,
		checkpoints: c.checkpoints,
		blobs:       c.blobs,
		model:       model,
		digest:      digest,
		ctx:         ctx,
		session:     c.session,
		writer:      pw,
		turn:        turn,
		duplex:      true,
	}
	context.AfterFunc(ctx, func() {
		out.closeWriter(ctx.Err())
		if body := out.body; body != nil {
			_ = body.Close()
		}
	})
	return out, nil
}

func (s *stream) writeLive(payload []byte) error {
	if s == nil {
		return errNoWriteAfterTerminal
	}
	if len(payload) > maxLiveWriteBytes {
		return errReplyTooLarge
	}
	frame, err := EncodeConnectFrame(payload, false)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	w, err := s.liveWriter(payload)
	if err != nil {
		return err
	}
	if _, err := w.Write(frame); err != nil {
		s.closeMu.Lock()
		s.writeErr = err
		s.terminal = true
		s.closeMu.Unlock()
		return err
	}
	return nil
}

func (s *stream) liveWriter(payload []byte) (pipeWriter, error) {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.terminal || s.writer == nil {
		return nil, errNoWriteAfterTerminal
	}
	if s.ctx != nil {
		if err := s.ctx.Err(); err != nil {
			s.terminal = true
			return nil, err
		}
	}
	if s.turn != nil {
		if _, err := s.turn.Reserve(resourcebudget.ClassDownstreamQueue, int64(len(payload))); err != nil {
			return nil, err
		}
	}
	return s.writer, nil
}

func (s *stream) closeWriter(err error) {
	if s == nil {
		return
	}
	s.closeMu.Lock()
	if s.terminal && s.writer == nil {
		s.closeMu.Unlock()
		return
	}
	s.terminal = true
	w := s.writer
	s.writer = nil
	s.closeMu.Unlock()
	if w == nil {
		return
	}
	if err != nil {
		_ = w.CloseWithError(err)
		return
	}
	_ = w.Close()
}

type pipeWriter interface {
	Write([]byte) (int, error)
	Close() error
	CloseWithError(error) error
}

var _ pipeWriter = (*io.PipeWriter)(nil)
