package openaichat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/providers"
)

const imageResponseMaxBytes = 64 << 20

func (c *Client) SupportsImageRelay() bool {
	if c == nil {
		return false
	}
	return providers.ImageCapable(c.endpoint, c.capability.AuthClass)
}

func (c *Client) RelayImage(ctx context.Context, _ providers.DispatchRequest, req providers.ImageRelayRequest) (int, http.Header, []byte, error) {
	if c == nil {
		return 0, nil, nil, fmt.Errorf("openai chat image client is not configured")
	}
	endpoint, err := providers.ImageURL(c.endpoint, req.Kind)
	if err != nil {
		return 0, nil, nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(req.Body))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("build OpenAI image request: %w", err)
	}
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = "application/json"
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(req.Body)), nil
	}
	httpReq.ContentLength = int64(len(req.Body))
	response, err := c.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		return 0, nil, nil, fmt.Errorf("OpenAI image request failed: %w", err)
	}
	defer response.Body.Close()
	header := response.Header.Clone()
	if declared := response.ContentLength; declared > imageResponseMaxBytes {
		_, _ = io.Copy(io.Discard, response.Body)
		return 0, nil, nil, fmt.Errorf("image response exceeded 64 MiB")
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, imageResponseMaxBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, nil, ctx.Err()
		}
		return 0, nil, nil, fmt.Errorf("failed to read image response: %w", err)
	}
	if int64(len(payload)) > imageResponseMaxBytes {
		return 0, nil, nil, fmt.Errorf("image response exceeded 64 MiB")
	}
	if err := ctx.Err(); err != nil {
		return 0, nil, nil, err
	}
	return response.StatusCode, header, payload, nil
}
