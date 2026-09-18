package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/router"
	"github.com/Wibias/Benes/internal/timeline"
)

type routeTraceProvider struct{}

func (routeTraceProvider) Open(context.Context, providercontract.DispatchRequest) (EventStream, error) {
	return nil, nil
}

type routeTraceNativeProvider struct{ routeTraceProvider }

func (routeTraceNativeProvider) NativeCodexForward() bool    { return true }
func (routeTraceNativeProvider) SupportsNativeCompact() bool { return true }
func (routeTraceNativeProvider) Compact(context.Context, providercontract.DispatchRequest, []byte) (int, http.Header, []byte, error) {
	return http.StatusOK, nil, []byte(`{}`), nil
}

func TestResolvedProviderAttributesLogicalOpenAIRouteOnDispatch(t *testing.T) {
	cacheKey := "test://" + t.Name()
	providerDefaultAccessCache.Store(cacheKey, config.DefaultAccessAPI)
	t.Cleanup(func() { providerDefaultAccessCache.Delete(cacheKey) })
	h := &handler{
		configPath: cacheKey,
		providers: map[string]Provider{
			config.LogicalOpenAIID:     routeTraceProvider{},
			config.OpenAIAPIConnection: routeTraceProvider{},
		},
	}
	resolved := h.resolveProvider(router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5.6"})
	tr := timeline.New("req-route", 8)
	ctx := timeline.WithTrace(context.Background(), tr)
	if _, err := resolved.Provider.Open(ctx, providercontract.DispatchRequest{}); err != nil {
		t.Fatal(err)
	}
	route := tr.Route()
	if route.RequestedProvider != config.LogicalOpenAIID || route.ProviderConnection != config.OpenAIAPIConnection || route.Model != "gpt-5.6" {
		t.Fatalf("route=%#v", route)
	}
}

func TestAttributedProviderPreservesNativeCompactCapabilities(t *testing.T) {
	h := &handler{providers: map[string]Provider{config.LogicalOpenAIID: routeTraceNativeProvider{}}}
	resolved := h.resolveProvider(router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5.6", CodexAccountID: "acct-1"})
	if !isNativeCodexForward(resolved.Provider) {
		t.Fatal("native Codex forward capability was lost")
	}
	if nativeCompact(resolved.Provider) == nil {
		t.Fatal("native compact capability was lost")
	}
}
