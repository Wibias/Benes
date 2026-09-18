package bootstrap

import (
	"context"
	"sync"

	"github.com/Wibias/Benes/internal/codexauth"
)

type deferredManagedQuotaFetcher struct {
	mu     sync.Mutex
	client *codexauth.WHAMClient
}

func newDeferredManagedQuotaFetcher() *deferredManagedQuotaFetcher {
	return &deferredManagedQuotaFetcher{}
}

func (f *deferredManagedQuotaFetcher) Fetch(
	ctx context.Context,
	token codexauth.ManagedToken,
	plan string,
) (codexauth.WHAMFetchResult, error) {
	client, err := f.clientFor(ctx)
	if err != nil {
		return codexauth.WHAMFetchResult{}, err
	}
	return client.Fetch(ctx, token, plan)
}

func (f *deferredManagedQuotaFetcher) FetchMain(
	ctx context.Context,
	token codexauth.ManagedToken,
	plan string,
) (codexauth.WHAMFetchResult, error) {
	client, err := f.clientFor(ctx)
	if err != nil {
		return codexauth.WHAMFetchResult{}, err
	}
	return client.FetchMain(ctx, token, plan)
}

func (f *deferredManagedQuotaFetcher) clientFor(ctx context.Context) (*codexauth.WHAMClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.client != nil {
		return f.client, nil
	}
	client, err := codexauth.NewWHAMClientHardened(ctx, codexauth.WHAMClientConfig{})
	if err != nil {
		return nil, err
	}
	f.client = client
	return client, nil
}
