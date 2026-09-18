package openairesponses

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/transport"
)

type forwardResolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f forwardResolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

func TestNewForwardHardenedDefersDNSUntilFirstUpstreamAttempt(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	resolver := forwardResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil, &net.DNSError{Err: "offline", Name: "chatgpt.com"}
	})
	client, err := NewForwardHardened(context.Background(), ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		DestinationPolicy: transport.DestinationPolicy{Resolver: resolver},
		CredentialAuthority: forwardCredentialAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
			return ForwardCredential{Authorization: "Bearer selected", ChatGPTAccountID: "acct"}, nil
		}),
	})
	if err != nil {
		t.Fatalf("constructor performed destination resolution: %v", err)
	}
	mu.Lock()
	before := calls
	mu.Unlock()
	if before != 0 {
		t.Fatalf("constructor resolver calls=%d", before)
	}

	dispatch := canonicalRequest(t, `{"model":"openai/gpt-5.6","store":false,"input":"hi"}`, "gpt-5.6")
	_, err = client.Open(context.Background(), dispatch)
	if err == nil || !strings.Contains(err.Error(), "validate OpenAI Responses forward destination") {
		t.Fatalf("Open err=%v", err)
	}
	mu.Lock()
	after := calls
	mu.Unlock()
	if after != 1 {
		t.Fatalf("first Open resolver calls=%d", after)
	}
}

func TestDeferredForwardDestinationRetriesResolutionAfterFailure(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	resolver := forwardResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		mu.Lock()
		calls++
		current := calls
		mu.Unlock()
		return nil, &net.DNSError{Err: "offline-" + string(rune('0'+current)), Name: "chatgpt.com"}
	})
	client, err := NewForwardHardened(context.Background(), ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		DestinationPolicy: transport.DestinationPolicy{Resolver: resolver},
		CredentialAuthority: forwardCredentialAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
			return ForwardCredential{Authorization: "Bearer selected", ChatGPTAccountID: "acct"}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := canonicalRequest(t, `{"model":"openai/gpt-5.6","store":false,"input":"hi"}`, "gpt-5.6")
	for range 2 {
		if _, err := client.Open(context.Background(), dispatch); err == nil {
			t.Fatal("resolution failure unexpectedly succeeded")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("resolver calls=%d want=2", calls)
	}
}

func TestDeferredForwardDestinationDoesNotCacheProxyDeferredTarget(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	resolver := forwardResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil, &net.DNSError{Err: "proxy-dns", Name: "chatgpt.com"}
	})
	client, err := NewForwardHardened(context.Background(), ForwardConfig{
		Endpoint: testCanonicalForwardResponsesEndpoint,
		DestinationPolicy: transport.DestinationPolicy{Resolver: resolver, AllowProxyResolution: true},
		CredentialAuthority: forwardCredentialAuthorityFunc(func(context.Context, providers.DispatchRequest) (ForwardCredential, error) {
			return ForwardCredential{Authorization: "Bearer selected", ChatGPTAccountID: "acct"}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := canonicalRequest(t, `{"model":"openai/gpt-5.6","store":false,"input":"hi"}`, "gpt-5.6")
	for range 2 {
		if _, err := client.Open(context.Background(), dispatch); err == nil {
			t.Fatal("proxy-deferred direct request unexpectedly succeeded")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 2 {
		t.Fatalf("resolver calls=%d want=2", calls)
	}
}

func TestNewForwardHardenedStillRejectsCanceledConstructionContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewForwardHardened(ctx, ForwardConfig{Endpoint: testCanonicalForwardResponsesEndpoint})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
