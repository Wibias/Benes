package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/router"
)

type attributingProvider struct {
	account providers.CommittedUsageAccount
	inner   Provider
	after   func()
}

func (p attributingProvider) Open(ctx context.Context, req providers.DispatchRequest) (providers.EventStream, error) {
	if obs := providers.CommittedUsageAccountObserverFrom(ctx); obs != nil && p.account != "" {
		obs.NoteCommittedUsageAccount(p.account)
	}
	if p.after != nil {
		p.after()
	}
	return p.inner.Open(ctx, req)
}

func TestUsageFinalizationDoesNotLoadManagedAccounts(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"codexAccounts":[{"id":"acct-uuid","logLabel":"alpha"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": exactGPT56Provider()},
		UsageLogPath:   usagePath,
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	loads := installCountingManagedAccountLoads(h)
	postUsage(t, h, "thread-none", "corr-none", "")
	if loads.Load() != 0 {
		t.Fatalf("non-Codex usage finalization loaded managed accounts %d times", loads.Load())
	}
	body := readUsageLog(t, usagePath)
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &row); err != nil {
		t.Fatal(err)
	}
	if _, ok := row["account"]; ok {
		t.Fatalf("non-Codex account leaked: %s", body)
	}
}

func TestUsageOmitsAccountWithoutCommittedIdentityAndSkipsConfigLookup(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"codexAccounts":[{"id":"acct-uuid","logLabel":"alpha"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": attributingProvider{inner: exactGPT56Provider()}},
		UsageLogPath:   usagePath,
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	loads := installCountingManagedAccountLoads(h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if loads.Load() != 0 {
		t.Fatalf("uncommitted Codex usage loaded managed accounts %d times", loads.Load())
	}
	body := readUsageLog(t, usagePath)
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &row); err != nil {
		t.Fatal(err)
	}
	if _, ok := row["account"]; ok {
		t.Fatalf("uncommitted account leaked: %s", body)
	}
}

func TestUsageRowUsesCommittedSafeLabelWithoutRawIdentity(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	configPath := filepath.Join(home, "config.json")
	cfg := `{
		"codexAccounts": [
			{"id":"acct-uuid","email":"secret.user@example.com","logLabel":"alpha","chatgptAccountId":"chat-secret","isMain":false}
		]
	}`
	if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{
			"openai": attributingProvider{account: "alpha", inner: exactGPT56Provider()},
		},
		UsageLogPath: usagePath,
		ConfigPath:   configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	loads := installCountingManagedAccountLoads(h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if loads.Load() != 0 {
		t.Fatalf("committed usage loaded managed accounts %d times", loads.Load())
	}
	body := readUsageLog(t, usagePath)
	if strings.Contains(body, "acct-uuid") || strings.Contains(body, "secret.user@example.com") || strings.Contains(body, "chat-secret") || strings.Contains(body, "sk-") {
		t.Fatalf("pii/secret leaked: %s", body)
	}
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &row); err != nil {
		t.Fatal(err)
	}
	if row["account"] != "alpha" {
		t.Fatalf("account=%v body=%s", row["account"], body)
	}
}

func TestUsageRowUsesCanonicalMainLogLabelWithoutConfigLookup(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{
			"openai": attributingProvider{account: "main", inner: exactGPT56Provider()},
		},
		UsageLogPath: usagePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	loads := installCountingManagedAccountLoads(h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if loads.Load() != 0 {
		t.Fatalf("main usage loaded managed accounts %d times", loads.Load())
	}
	body := readUsageLog(t, usagePath)
	var row map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &row); err != nil {
		t.Fatal(err)
	}
	if row["account"] != "main" {
		t.Fatalf("main account=%v body=%s", row["account"], body)
	}
	if strings.Contains(body, "__main__") {
		t.Fatalf("internal main id leaked: %s", body)
	}
}

func TestUsageRowKeepsCommittedLabelAfterConfigMutation(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"codexAccounts":[{"id":"acct-b","email":"b@example.com","logLabel":"bravo"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	committed := make(chan struct{})
	mutated := make(chan struct{})
	provider := attributingProvider{
		account: "bravo",
		inner:   exactGPT56Provider(),
		after: func() {
			close(committed)
			<-mutated
		},
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": provider},
		UsageLogPath:   usagePath,
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	loads := installCountingManagedAccountLoads(h)
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai/gpt-5.6","store":false,"stream":true}`))
		req.Header.Set("Authorization", "Bearer local-secret")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
		}
	}()
	<-committed
	if err := os.WriteFile(configPath, []byte(`{"codexAccounts":[{"id":"acct-b","logLabel":"changed"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	close(mutated)
	<-done
	if loads.Load() != 0 {
		t.Fatalf("finalization loaded managed accounts %d times", loads.Load())
	}
	body := readUsageLog(t, usagePath)
	if strings.Contains(body, "changed") || strings.Contains(body, "acct-b") || strings.Contains(body, "b@example.com") {
		t.Fatalf("config mutation rewrote usage: %s", body)
	}
	if !strings.Contains(body, `"account":"bravo"`) {
		t.Fatalf("committed label missing: %s", body)
	}
}

func TestUsageRowKeepsCommittedLabelWhenConfigBecomesUnreadable(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"codexAccounts":[{"id":"acct-b","logLabel":"bravo"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	committed := make(chan struct{})
	broken := make(chan struct{})
	provider := attributingProvider{
		account: "bravo",
		inner:   exactGPT56Provider(),
		after: func() {
			close(committed)
			<-broken
		},
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": provider},
		UsageLogPath:   usagePath,
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai/gpt-5.6","store":false,"stream":true}`))
		req.Header.Set("Authorization", "Bearer local-secret")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
		}
	}()
	<-committed
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(configPath, 0o700); err != nil {
		t.Fatal(err)
	}
	close(broken)
	<-done
	body := readUsageLog(t, usagePath)
	if !strings.Contains(body, `"account":"bravo"`) {
		t.Fatalf("unreadable config erased committed label: %s", body)
	}
}

func TestUsageRowFollowsCommittedAccountNotRouteAccount(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	inner := &routeAwareProvider{account: "bravo", events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken:         "local-secret",
		Providers:              map[string]Provider{"openai": inner},
		UsageLogPath:           usagePath,
		CodexAccountNamespaces: map[string]string{"acctns": "acct-a"},
		Aliases:                router.AliasTable{},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"acctns/gpt-5.6","store":false,"stream":true}`))
	req.Header.Set("Authorization", "Bearer local-secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := readUsageLog(t, usagePath)
	if strings.Contains(body, "acct-a") || strings.Contains(body, "acct-b") {
		t.Fatalf("raw account id leaked: %s", body)
	}
	if !strings.Contains(body, `"account":"bravo"`) {
		t.Fatalf("committed logLabel missing: %s", body)
	}
	if inner.routeID != "acct-a" {
		t.Fatalf("fixed route account=%q", inner.routeID)
	}
}

func TestConcurrentUsageRowsKeepOwnCommittedLabels(t *testing.T) {
	home := t.TempDir()
	usagePath := filepath.Join(home, "usage.jsonl")
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": concurrentLabelProvider{}},
		UsageLogPath:   usagePath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for _, label := range []string{"alpha", "beta"} {
		wg.Add(1)
		go func(label string) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai/gpt-5.6","store":false,"stream":true}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			req.Header.Set("thread-id", label)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("%s status=%d body=%s", label, rr.Code, rr.Body.String())
			}
		}(label)
	}
	wg.Wait()
	body := readUsageLog(t, usagePath)
	if !strings.Contains(body, `"account":"alpha"`) || !strings.Contains(body, `"account":"beta"`) {
		t.Fatalf("concurrent labels missing: %s", body)
	}
}

type routeAwareProvider struct {
	account providers.CommittedUsageAccount
	events  []protocol.Event
	routeID string
}

func (p *routeAwareProvider) Open(ctx context.Context, req providers.DispatchRequest) (providers.EventStream, error) {
	p.routeID = req.CodexAccountID
	if obs := providers.CommittedUsageAccountObserverFrom(ctx); obs != nil && p.account != "" {
		obs.NoteCommittedUsageAccount(p.account)
	}
	return &sliceStream{events: append([]protocol.Event(nil), p.events...)}, nil
}

type concurrentLabelProvider struct{}

func (p concurrentLabelProvider) Open(ctx context.Context, req providers.DispatchRequest) (providers.EventStream, error) {
	if obs := providers.CommittedUsageAccountObserverFrom(ctx); obs != nil {
		obs.NoteCommittedUsageAccount(providers.CommittedUsageAccount(req.ForwardHeaders.Get("thread-id")))
	}
	return &sliceStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
	}}, nil
}

func installCountingManagedAccountLoads(h http.Handler) *atomic.Int32 {
	impl := h.(*handler)
	n := &atomic.Int32{}
	impl.onManagedAccountLoad = func() { n.Add(1) }
	return n
}
