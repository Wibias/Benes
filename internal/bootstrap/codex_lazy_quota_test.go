package bootstrap

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
	"github.com/Wibias/Benes/internal/providerregistry"
	"github.com/Wibias/Benes/internal/providers"
)

type quotaPrimerStub struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release <-chan struct{}
}

func (p *quotaPrimerStub) Prime(ctx context.Context) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	if p.started != nil {
		select {
		case p.started <- struct{}{}:
		default:
		}
	}
	if p.release == nil {
		return
	}
	select {
	case <-p.release:
	case <-ctx.Done():
	}
}

func (p *quotaPrimerStub) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestCodexQuotaPrimeSchedulerIsNonBlockingAndSingleFlight(t *testing.T) {
	serverCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	managed := &quotaPrimerStub{started: started, release: release}
	main := &quotaPrimerStub{started: started, release: release}
	scheduler := newCodexQuotaPrimeScheduler(serverCtx, managed, main)

	returned := make(chan struct{})
	go func() {
		scheduler.Trigger()
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("Trigger blocked on quota priming")
	}
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("both quota primers did not start")
		}
	}
	for range 8 {
		scheduler.Trigger()
	}
	if managed.callCount() != 1 || main.callCount() != 1 {
		t.Fatalf("single-flight calls managed=%d main=%d", managed.callCount(), main.callCount())
	}

	close(release)
	deadline := time.Now().Add(time.Second)
	for scheduler.Running() {
		if time.Now().After(deadline) {
			t.Fatal("scheduler did not release single-flight slot")
		}
		time.Sleep(time.Millisecond)
	}
	scheduler.Trigger()
	deadline = time.Now().Add(time.Second)
	for managed.callCount() != 2 || main.callCount() != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("later trigger calls managed=%d main=%d", managed.callCount(), main.callCount())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCodexQuotaPrimeSchedulerUsesServerLifetimeNotRequestLifetime(t *testing.T) {
	serverCtx, cancelServer := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	managed := &quotaPrimerStub{started: started, release: release}
	scheduler := newCodexQuotaPrimeScheduler(serverCtx, managed, nil)

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	_ = requestCtx // scheduler intentionally has no request-context input.
	scheduler.Trigger()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request cancellation suppressed server-owned lazy prime")
	}
	if !scheduler.Running() {
		t.Fatal("server-owned prime ended before server cancellation")
	}

	cancelServer()
	deadline := time.Now().Add(time.Second)
	for scheduler.Running() {
		if time.Now().After(deadline) {
			t.Fatal("server cancellation did not stop in-flight quota prime")
		}
		time.Sleep(time.Millisecond)
	}

	before := managed.callCount()
	scheduler.Trigger()
	time.Sleep(20 * time.Millisecond)
	if managed.callCount() != before {
		t.Fatal("canceled server started a new quota prime")
	}
}

func TestCodexPoolAuthorityTriggersLazyPrimeWithoutBlockingCredentialResolution(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	writePrivateJSON(t, filepath.Join(benesHome, "codex-accounts.json"), map[string]any{
		"managed": map[string]any{"generation": 1, "credential": map[string]any{
			"accessToken": "managed-access", "refreshToken": "managed-refresh",
			"expiresAt": now.Add(time.Hour).UnixMilli(), "chatgptAccountId": "managed-chat",
		}},
	})
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	fetcher := bootstrapManagedQuotaFetcherFunc(func(ctx context.Context, _ codexauth.ManagedToken, plan string) (codexauth.WHAMFetchResult, error) {
		started <- struct{}{}
		select {
		case <-release:
			usage := 25.0
			return codexauth.WHAMFetchResult{StatusCode: http.StatusOK, Quota: codexauth.WHAMQuotaResult{Plan: plan, Quota: &codexauth.QuotaReading{WeeklyPercent: &usage}}}, nil
		case <-ctx.Done():
			return codexauth.WHAMFetchResult{}, ctx.Err()
		}
	})
	serverCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime, err := buildCodexPoolRuntime(serverCtx, poolDiskForTest(t, "{\"codexAccounts\":[{\"id\":\"managed\",\"plan\":\"plus\"}]}"), []providerregistry.Spec{poolSpecForTest()}, CodexPoolOptions{
		BenesHome: benesHome, CodexHome: codexHome, ManagedQuotaFetcher: fetcher, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	resolved := make(chan error, 1)
	go func() {
		credential, resolveErr := runtime.authorities["openai"].Resolve(context.Background(), providers.DispatchRequest{
			Parsed: protocolRequestForPoolTest("gpt-5.6"),
		})
		if resolveErr == nil && (credential.Authorization != "Bearer managed-access" || credential.ChatGPTAccountID != "managed-chat") {
			resolveErr = context.Canceled
		}
		resolved <- resolveErr
	}()
	select {
	case err := <-resolved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("credential resolution blocked on lazy quota prime")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("lazy quota prime did not reach managed WHAM fetcher")
	}
	close(release)
}
