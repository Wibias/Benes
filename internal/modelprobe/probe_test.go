package modelprobe

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProbeCatalogAvailableAndUnknownModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/models") {
			t.Errorf("path=%s method=%s", r.URL.Path, r.Method)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("auth=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"anthropic/claude-sonnet-5"}]}`))
	}))
	defer upstream.Close()

	got, err := Probe(context.Background(), Request{
		Provider: "vercel-ai-gateway", Model: "anthropic/claude-sonnet-5",
		BaseURL: upstream.URL + "/v1", APIKey: "k", Transport: upstream.Client().Transport,
		Now: func() time.Time { return time.Unix(1700000000, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateAvailable || got.Kind != KindCatalog || got.GenerationIncurred {
		t.Fatalf("%#v", got)
	}
	if got.Timing.TotalMs < 0 || got.TestedAt.IsZero() {
		t.Fatalf("timing %#v", got)
	}

	missing, err := Probe(context.Background(), Request{
		Provider: "vercel-ai-gateway", Model: "missing-model",
		BaseURL: upstream.URL + "/v1", APIKey: "k", Transport: upstream.Client().Transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	if missing.State != StateUnknown || missing.Reason != "model_not_listed" {
		t.Fatalf("%#v", missing)
	}
}

func TestProbeClassifiesAuthQuotaUnavailableAndTimeout(t *testing.T) {
	status := http.StatusUnauthorized
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	defer upstream.Close()
	req := Request{Provider: "p", Model: "m", BaseURL: upstream.URL, Transport: upstream.Client().Transport}

	got, _ := Probe(context.Background(), req)
	if got.State != StateAuthRequired {
		t.Fatalf("401 %#v", got)
	}
	status = http.StatusForbidden
	got, _ = Probe(context.Background(), req)
	if got.State != StateAuthRequired {
		t.Fatalf("403 %#v", got)
	}
	status = http.StatusPaymentRequired
	got, _ = Probe(context.Background(), req)
	if got.State != StateQuotaLimited {
		t.Fatalf("402 %#v", got)
	}
	status = http.StatusTooManyRequests
	got, _ = Probe(context.Background(), req)
	if got.State != StateQuotaLimited {
		t.Fatalf("429 %#v", got)
	}
	status = http.StatusInternalServerError
	got, _ = Probe(context.Background(), req)
	if got.State != StateUnavailable {
		t.Fatalf("500 %#v", got)
	}

	blocker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer blocker.Close()
	timed, err := Probe(context.Background(), Request{
		Provider: "p", Model: "m", BaseURL: blocker.URL,
		Transport: blocker.Client().Transport, Timeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if timed.State != StateUnavailable || timed.Reason != "timeout" {
		t.Fatalf("timeout %#v", timed)
	}
}

func TestProbeGenerationIsExplicitAndMinimal(t *testing.T) {
	var body map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("path=%s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer upstream.Close()
	got, err := Probe(context.Background(), Request{
		Provider: "p", Model: "gpt-x", BaseURL: upstream.URL + "/v1", Generate: true,
		Transport: upstream.Client().Transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateAvailable || !got.GenerationIncurred || got.Kind != KindGeneration {
		t.Fatalf("%#v", got)
	}
	if body["model"] != "gpt-x" || body["max_tokens"] != float64(1) || body["stream"] != false {
		t.Fatalf("body %#v", body)
	}
	messages := body["messages"].([]any)
	content := messages[0].(map[string]any)["content"]
	if content != "." {
		t.Fatalf("must not send user conversation content: %#v", body)
	}
}

func TestProbeDoesNotGenerateUnlessAsked(t *testing.T) {
	var posts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"m"}]}`))
	}))
	defer upstream.Close()
	_, _ = Probe(context.Background(), Request{Provider: "p", Model: "m", BaseURL: upstream.URL, Transport: upstream.Client().Transport})
	if posts.Load() != 0 {
		t.Fatal("automatic generation probe")
	}
}

func TestProbeStaticAndForwardAreUnsupportedWithoutGenerate(t *testing.T) {
	live := false
	got, _ := Probe(context.Background(), Request{Provider: "p", Model: "m", BaseURL: "https://example.com/v1", LiveModels: &live})
	if got.State != StateUnsupportedProbe || got.Reason != "static_catalog" {
		t.Fatalf("%#v", got)
	}
	got, _ = Probe(context.Background(), Request{Provider: "p", Model: "m", BaseURL: "https://example.com/v1", AuthMode: "forward"})
	if got.State != StateUnsupportedProbe || got.Reason != "forward_auth" {
		t.Fatalf("%#v", got)
	}
}

func TestProbeCancelDoesNotCacheUnavailable(t *testing.T) {
	started := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer upstream.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	_, err := Probe(ctx, Request{Provider: "p", Model: "m", BaseURL: upstream.URL, Transport: upstream.Client().Transport, Timeout: time.Second})
	if err == nil {
		t.Fatal("expected cancel")
	}
}

func TestProbeUserinfoDestinationIsRejected(t *testing.T) {
	got, err := Probe(context.Background(), Request{Provider: "p", Model: "m", BaseURL: "https://user:pass@example.com/v1"})
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateUnsupportedProbe || got.Reason != "lookalike_destination" {
		t.Fatalf("%#v", got)
	}
}

func TestCacheStaleIsUnknownNotUnavailable(t *testing.T) {
	cache := NewCache(time.Millisecond)
	cache.Store(Result{Provider: "p", Model: "m", State: StateAvailable, TestedAt: time.Now().Add(-time.Second), Kind: KindCatalog})
	time.Sleep(2 * time.Millisecond)
	got := cache.Lookup("p", "m", "", "")
	if got.State != StateUnknown || got.Reason != "stale" {
		t.Fatalf("%#v", got)
	}
	if cache.Lookup("p", "other", "", "").State != StateUnknown || cache.Lookup("p", "other", "", "").Reason != "unprobed" {
		t.Fatal("missing must be unprobed unknown")
	}
}

func TestCacheDoesNotKeepSecrets(t *testing.T) {
	cache := NewCache(time.Minute)
	cache.Store(Result{Provider: "p", Model: "m", State: StateAvailable, TestedAt: time.Now(), Reason: "Authorization: Bearer sk-secret"})
	got := cache.Lookup("p", "m", "", "")
	if strings.Contains(got.Reason, "sk-secret") || got.Reason != "probe_failed" {
		t.Fatalf("leaked %q", got.Reason)
	}
}

func TestProbeAllBoundsConcurrencyAndCancel(t *testing.T) {
	if ClampConcurrency(0) != DefaultConcurrency || ClampConcurrency(99) != MaxConcurrency {
		t.Fatal("clamp")
	}
	var current, max atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := current.Add(1)
		for {
			old := max.Load()
			if n <= old || max.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		current.Add(-1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[{"id":"a"},{"id":"b"},{"id":"c"}]}`))
	}))
	defer upstream.Close()
	results := ProbeAll(context.Background(), Request{Provider: "p", BaseURL: upstream.URL, Transport: upstream.Client().Transport}, []string{"a", "b", "c"}, 2)
	if len(results) != 3 {
		t.Fatalf("%d", len(results))
	}
	if max.Load() > 2 {
		t.Fatalf("concurrency %d", max.Load())
	}
}

func TestSafeReasonStripsBodies(t *testing.T) {
	if safeReason("upstream said {\"error\":\"nope\"}") != "probe_failed" {
		t.Fatal(safeReason("x"))
	}
}
