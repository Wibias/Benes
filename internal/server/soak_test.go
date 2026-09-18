package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

func TestSoak32Sessions10WavesResponsesIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "ok"},
			{Type: protocol.EventDone},
		}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	for range 10 {
		var wg sync.WaitGroup
		for range 32 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
				req.Header.Set("Authorization", "Bearer local-secret")
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, req)
				if rr.Code != http.StatusOK {
					t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
				}
			}()
		}
		wg.Wait()
		if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
			t.Fatalf("not idle: %#v", budget.Metrics())
		}
	}
}

func TestSoak32Sessions10WavesChatIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "ok"},
			{Type: protocol.EventDone},
		}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	body := `{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}],"stream":false}`
	for range 10 {
		var wg sync.WaitGroup
		for range 32 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer local-secret")
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, req)
				if rr.Code != http.StatusOK {
					t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
				}
			}()
		}
		wg.Wait()
		if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
			t.Fatalf("not idle: %#v", budget.Metrics())
		}
	}
}

func TestSoak32Sessions10WavesAnthropicIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "ok"},
			{Type: protocol.EventDone, StopReason: "end_turn"},
		}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	body := `{"model":"openai-apikey/gpt-5.6","max_tokens":64,"stream":false,"messages":[{"role":"user","content":"hi"}]}`
	for range 10 {
		var wg sync.WaitGroup
		for range 32 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer local-secret")
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, req)
				if rr.Code != http.StatusOK {
					t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
				}
			}()
		}
		wg.Wait()
		if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
			t.Fatalf("not idle: %#v", budget.Metrics())
		}
	}
}

func TestSoak64SessionBurstReturnsToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 64, MaxTurnBytes: 1 << 20, MaxProcessBytes: 64 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "ok"},
			{Type: protocol.EventDone},
		}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", budget.Metrics())
	}
}

func TestSoakCancelledRequestsReturnToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	provider := providerFunc(func(ctx context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true,"input":"hi"}`))
			req = req.WithContext(ctx)
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", budget.Metrics())
	}
}

func TestSoak429ResponsesReturnToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{{
			Type: protocol.EventError, Message: "slow down", HTTPStatus: http.StatusTooManyRequests,
		}}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code < 400 {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", budget.Metrics())
	}
}

func TestSoak503ResponsesReturnToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{{
			Type: protocol.EventError, Message: "unavailable", HTTPStatus: http.StatusServiceUnavailable,
		}}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code < 400 {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", budget.Metrics())
	}
}

func TestSoakPreFirstByteOpenFailureReturnsToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return nil, context.Canceled
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code < 400 {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", budget.Metrics())
	}
}

func TestSoakEightToolCallsReturnToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	events := make([]protocol.Event, 0, 17)
	for i := range 8 {
		id := "call_" + string(rune('a'+i))
		events = append(events,
			protocol.Event{Type: protocol.EventToolCallStart, ID: id, Name: "lookup"},
			protocol.Event{Type: protocol.EventToolCallEnd, ID: id, Name: "lookup"},
		)
	}
	events = append(events, protocol.Event{Type: protocol.EventDone})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: events}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", budget.Metrics())
	}
}

func TestSoakContinuationResponsesReturnToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "ok"},
			{Type: protocol.EventDone},
		}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	body := `{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"previous_response_id":"resp_1","input":"next"}`
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", budget.Metrics())
	}
}

func TestSoakShutdownWithActiveStreamsReturnToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	started := make(chan struct{}, 32)
	provider := providerFunc(func(ctx context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	cancels := make([]context.CancelFunc, 0, 32)
	var wg sync.WaitGroup
	for range 32 {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true,"input":"hi"}`))
			req = req.WithContext(ctx)
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
		}()
	}
	for range 32 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("provider did not start")
		}
	}
	for _, cancel := range cancels {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("requests did not return after shutdown")
	}
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", budget.Metrics())
	}
}

func TestSoakFragmentedSSEReturnToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	events := make([]protocol.Event, 0, 257)
	for range 256 {
		events = append(events, protocol.Event{Type: protocol.EventTextDelta, Text: "x"})
	}
	events = append(events, protocol.Event{Type: protocol.EventDone})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: events}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", budget.Metrics())
	}
}

func TestSoakHugeToolArgumentsReturnToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	args := `{"blob":"` + strings.Repeat("a", 65536) + `"}`
	events := []protocol.Event{
		{Type: protocol.EventToolCallStart, ID: "call_1", Name: "lookup"},
		{Type: protocol.EventToolCallDelta, ID: "call_1", Arguments: args},
		{Type: protocol.EventToolCallEnd, ID: "call_1", Name: "lookup"},
		{Type: protocol.EventDone},
	}
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: events}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("status=%d", rr.Code)
			}
		}()
	}
	wg.Wait()
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", budget.Metrics())
	}
}

func TestSoakTranslatorOverflowReturnsToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns:  32,
		MaxTurnBytes:    1 << 20,
		MaxProcessBytes: 32 << 20,
		ClassBytes:      map[resourcebudget.Class]int64{resourcebudget.ClassTranslator: 8},
	})
	provider := providerFunc(func(_ context.Context, req providercontract.DispatchRequest) (EventStream, error) {
		if req.Turn == nil {
			return nil, errors.New("missing turn")
		}

		if _, err := req.Turn.Reserve(resourcebudget.ClassTranslator, 64); err != nil {
			return nil, err
		}
		return &sliceStream{events: []protocol.Event{{Type: protocol.EventDone}}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	failed := 0
	var mu sync.Mutex
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code == http.StatusOK {
				t.Errorf("translator overflow succeeded")
				return
			}
			mu.Lock()
			failed++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if failed != 32 {
		t.Fatalf("failed=%d", failed)
	}
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", budget.Metrics())
	}
}

func TestSoakBlobOverflowReturnsToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns:  32,
		MaxTurnBytes:    1 << 20,
		MaxProcessBytes: 32 << 20,
		ClassBytes:      map[resourcebudget.Class]int64{resourcebudget.ClassBlob: 8},
	})
	provider := providerFunc(func(_ context.Context, req providercontract.DispatchRequest) (EventStream, error) {
		if req.Turn == nil {
			return nil, errors.New("missing turn")
		}
		if _, err := req.Turn.Reserve(resourcebudget.ClassBlob, 64); err != nil {
			return nil, err
		}
		return &sliceStream{events: []protocol.Event{{Type: protocol.EventDone}}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	failed := 0
	var mu sync.Mutex
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code == http.StatusOK {
				t.Errorf("blob overflow succeeded")
				return
			}
			mu.Lock()
			failed++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if failed != 32 {
		t.Fatalf("failed=%d", failed)
	}
	if budget.Metrics().ActiveTurns != 0 || budget.Metrics().ProcessBytes != 0 {
		t.Fatalf("not idle: %#v", budget.Metrics())
	}
}

func assertBudgetIdle(t *testing.T, budget *resourcebudget.Manager) {
	t.Helper()
	m := budget.Metrics()
	if m.ActiveTurns != 0 || m.ActiveReaders != 0 || m.ProcessBytes != 0 || m.BudgetWaiters != 0 || m.PostCommitRetryAttempts != 0 {
		t.Fatalf("not idle: %#v", m)
	}
}

func TestSoakSuccessReturnsReadersAndCommitIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "ok"},
			{Type: protocol.EventDone},
		}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("status=%d", rr.Code)
			}
		}()
	}
	wg.Wait()
	assertBudgetIdle(t, budget)
}

func TestSoakPanicDuringOpenReturnsToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	provider := providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		panic("provider boom")
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusInternalServerError {
				t.Errorf("status=%d", rr.Code)
			}
		}()
	}
	wg.Wait()
	assertBudgetIdle(t, budget)
}

func TestSoakRequestBodyOverflowReturnsToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns:  32,
		MaxTurnBytes:    1 << 20,
		MaxProcessBytes: 32 << 20,
		ClassBytes:      map[resourcebudget.Class]int64{resourcebudget.ClassRequestBody: 8},
	})
	provider := providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
		t.Error("provider should not open")
		return &sliceStream{events: []protocol.Event{{Type: protocol.EventDone}}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	body := `{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hello world"}`
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code == http.StatusOK {
				t.Errorf("request body overflow succeeded")
			}
		}()
	}
	wg.Wait()
	assertBudgetIdle(t, budget)
}

func TestSoakDownstreamQueueOverflowReturnsToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns:  32,
		MaxTurnBytes:    1 << 20,
		MaxProcessBytes: 32 << 20,
		ClassBytes:      map[resourcebudget.Class]int64{resourcebudget.ClassDownstreamQueue: 8},
	})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "hello world"},
			{Type: protocol.EventDone},
		}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
		}()
	}
	wg.Wait()
	assertBudgetIdle(t, budget)
}

func TestSoakContinuationOverflowReturnsToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns:  32,
		MaxTurnBytes:    1 << 20,
		MaxProcessBytes: 32 << 20,
		ClassBytes:      map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 8},
	})
	provider := providerFunc(func(_ context.Context, req providercontract.DispatchRequest) (EventStream, error) {
		if req.Turn == nil {
			return nil, errors.New("missing turn")
		}
		if _, err := req.Turn.Reserve(resourcebudget.ClassContinuation, 64); err != nil {
			return nil, err
		}
		return &sliceStream{events: []protocol.Event{{Type: protocol.EventDone}}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	failed := 0
	var mu sync.Mutex
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code == http.StatusOK {
				t.Errorf("continuation overflow succeeded")
				return
			}
			mu.Lock()
			failed++
			mu.Unlock()
		}()
	}
	wg.Wait()
	if failed != 32 {
		t.Fatalf("failed=%d", failed)
	}
	assertBudgetIdle(t, budget)
}

type slowStream struct {
	events []protocol.Event
	delay  time.Duration
}

func (s *slowStream) Next() (protocol.Event, error) {
	if len(s.events) == 0 {
		return protocol.Event{}, io.EOF
	}
	time.Sleep(s.delay)
	event := s.events[0]
	s.events = s.events[1:]
	return event, nil
}

func (s *slowStream) Close() error { return nil }

func TestSoakSlowConsumerReturnsToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &slowStream{
			delay: 5 * time.Millisecond,
			events: []protocol.Event{
				{Type: protocol.EventTextDelta, Text: "ok"},
				{Type: protocol.EventDone},
			},
		}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":true,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("status=%d", rr.Code)
			}
		}()
	}
	wg.Wait()
	assertBudgetIdle(t, budget)
}

func TestSoakNamespacedAndCustomToolsReturnToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	events := []protocol.Event{
		{Type: protocol.EventToolCallStart, ID: "call_ns", Name: "math.add"},
		{Type: protocol.EventToolCallDelta, ID: "call_ns", Arguments: `{"a":1}`},
		{Type: protocol.EventToolCallEnd, ID: "call_ns", Name: "math.add"},
		{Type: protocol.EventToolCallStart, ID: "call_custom", Name: "custom.search"},
		{Type: protocol.EventToolCallEnd, ID: "call_custom", Name: "custom.search"},
		{Type: protocol.EventDone},
	}
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		copied := append([]protocol.Event(nil), events...)
		return &sliceStream{events: copied}, nil
	})

	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi"}`))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("status=%d", rr.Code)
			}
		}()
	}
	wg.Wait()
	assertBudgetIdle(t, budget)
}

type soakLiveEndpoint struct {
	name string
	path string
	body string
}

func soakLiveEndpoints() []soakLiveEndpoint {
	return []soakLiveEndpoint{
		{name: "responses", path: "/v1/responses", body: `{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hello world"}`},
		{name: "chat", path: "/v1/chat/completions", body: `{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hello world"}],"stream":false}`},
		{name: "anthropic", path: "/v1/messages", body: `{"model":"openai-apikey/gpt-5.6","max_tokens":64,"stream":false,"messages":[{"role":"user","content":"hello world"}]}`},
	}
}

func TestSoakOutputOverflowFailClosesLiveEndpoints(t *testing.T) {
	for _, endpoint := range soakLiveEndpoints() {
		t.Run(endpoint.name, func(t *testing.T) {
			budget := resourcebudget.NewManager(resourcebudget.Limits{
				MaxActiveTurns:  32,
				MaxTurnBytes:    1 << 20,
				MaxProcessBytes: 32 << 20,
				ClassBytes:      map[resourcebudget.Class]int64{resourcebudget.ClassOutput: 4},
			})
			provider := providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
				return &sliceStream{events: []protocol.Event{
					{Type: protocol.EventTextDelta, Text: "hello world"},
					{Type: protocol.EventDone, StopReason: "end_turn"},
				}}, nil
			})
			h, err := NewHandler(Options{
				DataPlaneToken: "local-secret",
				Providers:      map[string]Provider{"openai-apikey": provider},
				ResourceBudget: budget,
			})
			if err != nil {
				t.Fatal(err)
			}
			h = attachHandlerClose(t, h)
			var wg sync.WaitGroup
			for range 32 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					req := httptest.NewRequest(http.MethodPost, endpoint.path, strings.NewReader(endpoint.body))
					req.Header.Set("Authorization", "Bearer local-secret")
					rr := httptest.NewRecorder()
					h.ServeHTTP(rr, req)
					if rr.Code == http.StatusOK {
						t.Errorf("output overflow succeeded")
					}
				}()
			}
			wg.Wait()
			assertBudgetIdle(t, budget)
		})
	}
}

func TestSoakRequestBodyOverflowFailClosesLiveEndpoints(t *testing.T) {
	for _, endpoint := range soakLiveEndpoints() {
		t.Run(endpoint.name, func(t *testing.T) {
			budget := resourcebudget.NewManager(resourcebudget.Limits{
				MaxActiveTurns:  32,
				MaxTurnBytes:    1 << 20,
				MaxProcessBytes: 32 << 20,
				ClassBytes:      map[resourcebudget.Class]int64{resourcebudget.ClassRequestBody: 8},
			})
			provider := providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
				t.Error("provider should not open")
				return &sliceStream{events: []protocol.Event{{Type: protocol.EventDone}}}, nil
			})
			h, err := NewHandler(Options{
				DataPlaneToken: "local-secret",
				Providers:      map[string]Provider{"openai-apikey": provider},
				ResourceBudget: budget,
			})
			if err != nil {
				t.Fatal(err)
			}
			h = attachHandlerClose(t, h)
			var wg sync.WaitGroup
			for range 32 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					req := httptest.NewRequest(http.MethodPost, endpoint.path, strings.NewReader(endpoint.body))
					req.Header.Set("Authorization", "Bearer local-secret")
					rr := httptest.NewRecorder()
					h.ServeHTTP(rr, req)
					if rr.Code == http.StatusOK {
						t.Errorf("request body overflow succeeded")
					}
				}()
			}
			wg.Wait()
			assertBudgetIdle(t, budget)
		})
	}
}

func TestSoakReservedClassOverflowFailClosesLiveEndpoints(t *testing.T) {
	classes := []resourcebudget.Class{
		resourcebudget.ClassTranslator,
		resourcebudget.ClassBlob,
		resourcebudget.ClassContinuation,
		resourcebudget.ClassToolArguments,
		resourcebudget.ClassStreamPending,
	}
	for _, class := range classes {
		for _, endpoint := range soakLiveEndpoints() {
			t.Run(string(class)+"/"+endpoint.name, func(t *testing.T) {
				budget := resourcebudget.NewManager(resourcebudget.Limits{
					MaxActiveTurns:  32,
					MaxTurnBytes:    1 << 20,
					MaxProcessBytes: 32 << 20,
					ClassBytes:      map[resourcebudget.Class]int64{class: 8},
				})
				provider := providerFunc(func(_ context.Context, req providercontract.DispatchRequest) (EventStream, error) {
					if req.Turn == nil {
						return nil, errors.New("missing turn")
					}
					if _, err := req.Turn.Reserve(class, 64); err != nil {
						return nil, err
					}
					return &sliceStream{events: []protocol.Event{{Type: protocol.EventDone, StopReason: "end_turn"}}}, nil
				})
				h, err := NewHandler(Options{
					DataPlaneToken: "local-secret",
					Providers:      map[string]Provider{"openai-apikey": provider},
					ResourceBudget: budget,
				})
				if err != nil {
					t.Fatal(err)
				}
				h = attachHandlerClose(t, h)
				var wg sync.WaitGroup
				failed := 0
				var mu sync.Mutex
				for range 32 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						req := httptest.NewRequest(http.MethodPost, endpoint.path, strings.NewReader(endpoint.body))
						req.Header.Set("Authorization", "Bearer local-secret")
						rr := httptest.NewRecorder()
						h.ServeHTTP(rr, req)
						if rr.Code == http.StatusOK {
							t.Errorf("%s overflow succeeded", class)
							return
						}
						mu.Lock()
						failed++
						mu.Unlock()
					}()
				}
				wg.Wait()
				if failed != 32 {
					t.Fatalf("failed=%d", failed)
				}
				assertBudgetIdle(t, budget)
			})
		}
	}
}

func TestSoak32Sessions10RecallWavesReturnToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "ok"},
			{Type: protocol.EventDone},
		}}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	body := `{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"previous_response_id":"resp_1","input":"recall"}`
	for range 10 {
		var wg sync.WaitGroup
		for range 32 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
				req.Header.Set("Authorization", "Bearer local-secret")
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, req)
				if rr.Code != http.StatusOK {
					t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
				}
			}()
		}
		wg.Wait()
		assertBudgetIdle(t, budget)
	}
}

func TestSoakToolSearchCallsReturnToIdle(t *testing.T) {
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 32, MaxTurnBytes: 1 << 20, MaxProcessBytes: 32 << 20})
	events := []protocol.Event{
		{Type: protocol.EventToolCallStart, ID: "ts1", Name: "tool_search"},
		{Type: protocol.EventToolCallDelta, ID: "ts1", Arguments: `{"query":"db"}`},
		{Type: protocol.EventToolCallEnd, ID: "ts1", Name: "tool_search"},
		{Type: protocol.EventDone},
	}
	provider := providerFunc(func(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
		copied := append([]protocol.Event(nil), events...)
		return &sliceStream{events: copied}, nil
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	body := `{"model":"openai-apikey/gpt-5.6","store":false,"stream":false,"input":"hi","tools":[{"type":"tool_search"}]}`
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer local-secret")
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Errorf("status=%d body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	assertBudgetIdle(t, budget)
}

func TestSoakDownstreamQueueOverflowFailClosesChatAndAnthropic(t *testing.T) {
	endpoints := []soakLiveEndpoint{
		{name: "chat", path: "/v1/chat/completions", body: `{"model":"openai-apikey/gpt-5.6","messages":[{"role":"user","content":"hi"}],"stream":true}`},
		{name: "anthropic", path: "/v1/messages", body: `{"model":"openai-apikey/gpt-5.6","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}`},
	}
	for _, endpoint := range endpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			budget := resourcebudget.NewManager(resourcebudget.Limits{
				MaxActiveTurns:  32,
				MaxTurnBytes:    1 << 20,
				MaxProcessBytes: 32 << 20,
				ClassBytes:      map[resourcebudget.Class]int64{resourcebudget.ClassDownstreamQueue: 8},
			})
			provider := providerFunc(func(context.Context, providercontract.DispatchRequest) (EventStream, error) {
				return &sliceStream{events: []protocol.Event{
					{Type: protocol.EventTextDelta, Text: "hello world"},
					{Type: protocol.EventDone, StopReason: "end_turn"},
				}}, nil
			})
			h, err := NewHandler(Options{
				DataPlaneToken: "local-secret",
				Providers:      map[string]Provider{"openai-apikey": provider},
				ResourceBudget: budget,
			})
			if err != nil {
				t.Fatal(err)
			}
			h = attachHandlerClose(t, h)
			var wg sync.WaitGroup
			for range 32 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					req := httptest.NewRequest(http.MethodPost, endpoint.path, strings.NewReader(endpoint.body))
					req.Header.Set("Authorization", "Bearer local-secret")
					rr := httptest.NewRecorder()
					h.ServeHTTP(rr, req)
				}()
			}
			wg.Wait()
			assertBudgetIdle(t, budget)
		})
	}
}
