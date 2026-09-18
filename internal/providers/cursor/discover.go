package cursor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
)

const (
	UsableModelsPath     = "/agent.v1.AgentService/GetUsableModels"
	usableModelsPath     = UsableModelsPath
	maxDiscoveryBytes    = 4 << 20
	maxDiscoveryModels   = 256
	DiscoveryContentType = "application/proto"
	discoveryContentType = DiscoveryContentType
)

func (c *Client) DiscoverUsableModels(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, fmt.Errorf("cursor client is required")
	}
	return FetchUsableModels(ctx, c.httpClient, c.endpoint, c.apiKey)
}

func FetchUsableModels(ctx context.Context, httpClient *http.Client, endpoint, token string) ([]string, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	models, err := fetchUsableModelsOnce(ctx, httpClient, endpoint, token)
	if err != nil && ctx.Err() == nil && transientPreHeaderDiscoveryError(err) {
		return fetchUsableModelsOnce(ctx, httpClient, endpoint, token)
	}
	return models, err
}

func fetchUsableModelsOnce(ctx context.Context, httpClient *http.Client, endpoint, token string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+usableModelsPath, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", discoveryContentType)
	req.Header.Set("Connect-Protocol-Version", "1")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Cursor discovery returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxDiscoveryBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxDiscoveryBytes {
		return nil, fmt.Errorf("Cursor discovery response exceeds byte cap")
	}
	models := DecodeUsableModelIDs(raw)
	if len(models) > maxDiscoveryModels {
		return nil, fmt.Errorf("Cursor discovery exceeds model-count cap")
	}
	return models, nil
}

func transientPreHeaderDiscoveryError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && (errors.Is(opErr.Err, io.EOF) || errors.Is(opErr.Err, io.ErrUnexpectedEOF)) {
		return true
	}
	return false
}

func DecodeUsableModelIDs(raw []byte) []string {
	fields, err := decodeProtoFields(raw)
	if err != nil {
		return nil
	}
	var models []string
	for _, field := range fields {
		if field.num != 1 || field.wire != 2 {
			continue
		}
		inner, err := decodeProtoFields(field.bytes)
		if err != nil {
			continue
		}
		if id := string(fieldBytes(inner, 1)); id != "" {
			models = append(models, id)
		}
	}
	return models
}

func (c *Client) HTTPVersion() HTTPVersion {
	if c == nil || c.httpVersion == "" {
		return HTTPVersion2
	}
	return c.httpVersion
}
