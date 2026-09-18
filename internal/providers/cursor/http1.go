package cursor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/providers"
)

const (
	runSSEPath = "/agent.v1.AgentService/RunSSE"
	appendPath = "/agent.v1.AgentService/BidiAppend"
)

func (c *Client) openHTTP1(ctx context.Context, frame []byte, model, digest string) (providers.EventStream, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+runSSEPath, bytes.NewReader(frame))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/connect+proto")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if replayErr := c.session.MarkReachabilityFailure(); replayErr != nil {
			return nil, replayErr
		}
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("Cursor RunSSE returned HTTP %d", resp.StatusCode)
	}
	requestID := strings.TrimSpace(resp.Header.Get("X-Request-Id"))
	if requestID == "" {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("Cursor RunSSE omitted request id")
	}
	if err := c.session.Register(requestID); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	if err := c.session.Append(requestID, 1); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	appendReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+appendPath, bytes.NewReader(frame))
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	appendReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	appendReq.Header.Set("Content-Type", "application/connect+proto")
	appendReq.Header.Set("X-Request-Id", requestID)
	appendReq.Header.Set("X-Cursor-Seq", "1")
	appendResp, err := c.httpClient.Do(appendReq)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	_ = appendResp.Body.Close()
	if appendResp.StatusCode < 200 || appendResp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("Cursor BidiAppend returned HTTP %d", appendResp.StatusCode)
	}
	return &stream{
		body:        resp.Body,
		checkpoints: c.checkpoints,
		blobs:       c.blobs,
		model:       model,
		digest:      digest,
		http:        c.httpClient,
		ctx:         ctx,
		endpoint:    c.endpoint,
		token:       c.apiKey,
		requestID:   requestID,
		session:     c.session,
	}, nil
}

func (s *stream) writeKV(payload []byte) error {
	if s == nil {
		return nil
	}
	if s.duplex {
		return s.writeLive(payload)
	}
	if s.http == nil || s.requestID == "" {
		return nil
	}
	seq, err := s.session.AllocateAppend(s.requestID)
	if err != nil {
		return err
	}
	frame, err := EncodeConnectFrame(payload, false)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, s.endpoint+appendPath, bytes.NewReader(frame))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/connect+proto")
	req.Header.Set("X-Request-Id", s.requestID)
	req.Header.Set("X-Cursor-Seq", fmt.Sprintf("%d", seq))
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Cursor KV append returned HTTP %d", resp.StatusCode)
	}
	return nil
}
