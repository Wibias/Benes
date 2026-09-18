package openairesponses

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/Wibias/Benes/internal/transport"
)

type deferredForwardTransport struct {
	endpoint string
	policy   transport.DestinationPolicy
	options  transport.ClientOptions

	mu   sync.Mutex
	next http.RoundTripper
}

func newDeferredForwardHTTPClient(endpoint string, policy transport.DestinationPolicy, options transport.ClientOptions) *http.Client {
	return &http.Client{
		Transport: &deferredForwardTransport{endpoint: endpoint, policy: policy, options: options},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (t *deferredForwardTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil {
		return nil, fmt.Errorf("forward request is required")
	}
	next, err := t.resolve(request.Context())
	if err != nil {
		return nil, err
	}
	return next.RoundTrip(request)
}

func (t *deferredForwardTransport) resolve(ctx context.Context) (http.RoundTripper, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.next != nil {
		return t.next, nil
	}
	target, err := transport.ResolveTarget(ctx, t.endpoint, t.policy)
	if err != nil {
		return nil, fmt.Errorf("validate OpenAI Responses forward destination: %w", err)
	}
	client := transport.NewClient(target, t.options)
	if client.Transport == nil {
		return nil, fmt.Errorf("create OpenAI Responses forward transport")
	}
	if target.ResolutionDeferred {
		return client.Transport, nil
	}
	t.next = client.Transport
	return t.next, nil
}

func (t *deferredForwardTransport) CloseIdleConnections() {
	t.mu.Lock()
	next := t.next
	t.mu.Unlock()
	if closer, ok := next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
