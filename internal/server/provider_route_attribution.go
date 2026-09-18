package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/timeline"
)

type attributedProvider struct {
	Provider
	requestedProvider  string
	providerConnection string
	model              string
}

type outgoingIDBinder interface {
	BindOutgoingID(id string)
}

func withProviderRouteAttribution(provider Provider, requestedProvider, providerConnection, model string) Provider {
	if provider == nil {
		return nil
	}
	return attributedProvider{
		Provider:           provider,
		requestedProvider:  requestedProvider,
		providerConnection: providerConnection,
		model:              model,
	}
}

func (p attributedProvider) BindOutgoingID(id string) {
	if binder, ok := p.Provider.(outgoingIDBinder); ok {
		binder.BindOutgoingID(id)
	}
}

func bindPolicyOutgoingID(provider Provider, id string) {
	if binder, ok := provider.(outgoingIDBinder); ok {
		binder.BindOutgoingID(id)
	}
}

func (p attributedProvider) markRoute(ctx context.Context) {
	if tr := timeline.FromContext(ctx); tr != nil {
		tr.SetRoute(timeline.Route{
			RequestedProvider:  p.requestedProvider,
			ProviderConnection: p.providerConnection,
			Model:              p.model,
		})
	}
}

func (p attributedProvider) Open(ctx context.Context, request providercontract.DispatchRequest) (EventStream, error) {
	p.markRoute(ctx)
	return p.Provider.Open(ctx, request)
}

func (p attributedProvider) OpenCommitted(ctx context.Context, request providercontract.DispatchRequest) (EventStream, error) {
	p.markRoute(ctx)
	if committed, ok := p.Provider.(committedOpener); ok {
		return committed.OpenCommitted(ctx, request)
	}
	return p.Provider.Open(ctx, request)
}

func (p attributedProvider) NativeCodexForward() bool {
	forwarder, ok := p.Provider.(nativeCodexForwarder)
	return ok && forwarder.NativeCodexForward()
}

func (p attributedProvider) SupportsNativeCompact() bool {
	compactor, ok := p.Provider.(nativeCompactor)
	return ok && compactor.SupportsNativeCompact()
}

func (p attributedProvider) Compact(ctx context.Context, request providercontract.DispatchRequest, body []byte) (int, http.Header, []byte, error) {
	compactor, ok := p.Provider.(nativeCompactor)
	if !ok || !compactor.SupportsNativeCompact() {
		return 0, nil, nil, errors.New("provider does not support native compact")
	}
	p.markRoute(ctx)
	return compactor.Compact(ctx, request, body)
}

func (p attributedProvider) Search(ctx context.Context, request providercontract.DispatchRequest, body []byte) (int, http.Header, []byte, error) {
	searcher := nativeCodexSearch(p.Provider)
	if searcher == nil {
		return 0, nil, nil, errors.New("provider does not support native Codex search")
	}
	p.markRoute(ctx)
	return searcher.Search(ctx, request, body)
}

func (p attributedProvider) SupportsImageRelay() bool {
	relayer, ok := p.Provider.(providercontract.ImageRelay)
	return ok && relayer.SupportsImageRelay()
}

func (p attributedProvider) RelayImage(ctx context.Context, request providercontract.DispatchRequest, body providercontract.ImageRelayRequest) (int, http.Header, []byte, error) {
	relayer, ok := p.Provider.(providercontract.ImageRelay)
	if !ok || !relayer.SupportsImageRelay() {
		return 0, nil, nil, errors.New("provider does not support image relay")
	}
	p.markRoute(ctx)
	return relayer.RelayImage(ctx, request, body)
}

func (p attributedProvider) Protocol() string {
	if reporter, ok := p.Provider.(interface{ Protocol() string }); ok {
		return strings.TrimSpace(reporter.Protocol())
	}
	return ""
}
