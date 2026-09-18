package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
)

type scriptedOpener struct {
	t     *testing.T
	calls int
}

func (o *scriptedOpener) Open(_ context.Context, req providers.DispatchRequest) (providers.EventStream, error) {
	o.calls++
	if o.calls == 1 {
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventToolCallEnd, ID: "c1", Name: "web_search", Arguments: `{"query":"benes"}`},
			{Type: protocol.EventDone},
		}}, nil
	}
	if o.calls != 2 {
		o.t.Fatalf("unexpected open count %d", o.calls)
	}
	msgs := req.Parsed.Context.Messages
	if len(msgs) < 2 || msgs[len(msgs)-2].Role != protocol.RoleAssistant || msgs[len(msgs)-1].Role != protocol.RoleToolResult {
		o.t.Fatalf("continuation messages=%#v", msgs)
	}
	if msgs[len(msgs)-1].ToolCallID != "c1" {
		o.t.Fatalf("tool result id=%#v", msgs[len(msgs)-1])
	}
	for _, tool := range req.Parsed.Context.Tools {
		if tool.Name == "web_search" {
			o.t.Fatal("force-answer iteration still offered web_search")
		}
	}
	return &sliceStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "Benes is a proxy."},
		{Type: protocol.EventDone},
	}}, nil
}

func TestOpenLoopReopensProviderWithToolResult(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&map[string]any{})
		_, _ = w.Write([]byte(`{"sources":[{"url":"https://example.com","title":"Example"}]}`))
	}))
	t.Cleanup(sidecar.Close)
	client, err := New(Config{
		Enabled: true, ProviderID: "opencode-zen", Endpoint: sidecar.URL,
		AuthClass: "key", Wire: "openai-responses", ModelID: "deepseek-v4-flash",
		AllowedModels: []string{"deepseek-v4-flash"}, APIKey: "sidecar-key", HTTPClient: sidecar.Client(),
		MaxSearches: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	opener := &scriptedOpener{t: t}
	stream, err := OpenLoop(context.Background(), opener.Open, providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{Context: protocol.Context{
			Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "search"}}}},
			Tools:    []protocol.Tool{SyntheticTool()},
		}},
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	begin, err := stream.Next()
	if err != nil || begin.Type != protocol.EventWebSearchCallBegin {
		t.Fatalf("begin=%#v err=%v", begin, err)
	}
	end, err := stream.Next()
	if err != nil || end.Type != protocol.EventWebSearchCallEnd || end.Status != "completed" {
		t.Fatalf("end=%#v err=%v", end, err)
	}
	text, err := stream.Next()
	if err != nil || text.Type != protocol.EventTextDelta || text.Text != "Benes is a proxy." {
		t.Fatalf("text=%#v err=%v", text, err)
	}
	done, err := stream.Next()
	if err != nil || done.Type != protocol.EventDone {
		t.Fatalf("done=%#v err=%v", done, err)
	}
	if _, err := stream.Next(); err != io.EOF && err != nil {
		// drain
	}
	if opener.calls != 2 {
		t.Fatalf("opens=%d", opener.calls)
	}
	_ = stream.Close()
}

func TestOpenLoopReservesContinuationBytesOnReopen(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sources":[{"url":"https://example.com","title":"Example"}]}`))
	}))
	t.Cleanup(sidecar.Close)
	client, err := New(Config{
		Enabled: true, ProviderID: "opencode-zen", Endpoint: sidecar.URL,
		AuthClass: "key", Wire: "openai-responses", ModelID: "deepseek-v4-flash",
		AllowedModels: []string{"deepseek-v4-flash"}, APIKey: "sidecar-key", HTTPClient: sidecar.Client(),
		MaxSearches: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 1 << 20},
	})
	turn, err := mgr.AcquireTurn(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turn.Close() })
	opener := &scriptedOpener{t: t}
	stream, err := OpenLoop(context.Background(), opener.Open, providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{Context: protocol.Context{
			Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "search"}}}},
			Tools:    []protocol.Tool{SyntheticTool()},
		}},
		Turn: turn,
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	for {
		_, nextErr := stream.Next()
		if nextErr != nil {
			break
		}
	}
	_ = stream.Close()
	if mgr.Metrics().Bytes[resourcebudget.ClassContinuation] <= 0 {
		t.Fatal("continuation bytes were not reserved on sidecar reopen")
	}
}

func TestOpenLoopFailsClosedWhenContinuationBudgetExceeded(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sources":[{"url":"https://example.com"}]}`))
	}))
	t.Cleanup(sidecar.Close)
	client, err := New(Config{
		Enabled: true, ProviderID: "opencode-zen", Endpoint: sidecar.URL,
		AuthClass: "key", Wire: "openai-responses", ModelID: "deepseek-v4-flash",
		AllowedModels: []string{"deepseek-v4-flash"}, APIKey: "sidecar-key", HTTPClient: sidecar.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 1},
	})
	turn, err := mgr.AcquireTurn(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = turn.Close() })
	opens := 0
	stream, err := OpenLoop(context.Background(), func(_ context.Context, _ providers.DispatchRequest) (providers.EventStream, error) {
		opens++
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventToolCallEnd, ID: "c1", Name: "web_search", Arguments: `{"query":"benes"}`},
			{Type: protocol.EventDone},
		}}, nil
	}, providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{Context: protocol.Context{
			Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "search"}}}},
			Tools:    []protocol.Tool{SyntheticTool()},
		}},
		Turn: turn,
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	var got protocol.Event
	for {
		ev, nextErr := stream.Next()
		if nextErr != nil {
			break
		}
		got = ev
		if ev.Type == protocol.EventError {
			break
		}
	}
	_ = stream.Close()
	if opens != 1 {
		t.Fatalf("opens=%d", opens)
	}
	if got.Type != protocol.EventError || got.Code != "resource_exhausted" {
		t.Fatalf("terminal=%#v", got)
	}
}

type budgetOpener struct {
	t     *testing.T
	max   int
	calls int
}

func (o *budgetOpener) Open(_ context.Context, req providers.DispatchRequest) (providers.EventStream, error) {
	o.calls++
	hasTool := false
	for _, tool := range req.Parsed.Context.Tools {
		if tool.Name == "web_search" || tool.HostedWebSearch {
			hasTool = true
			break
		}
	}
	if o.calls <= o.max {
		if !hasTool {
			o.t.Fatalf("open %d dropped web_search before the search budget", o.calls)
		}
		id := fmt.Sprintf("c%d", o.calls)
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventToolCallEnd, ID: id, Name: "web_search", Arguments: `{"query":"benes"}`},
			{Type: protocol.EventDone},
		}}, nil
	}
	if hasTool {
		o.t.Fatalf("force-answer open %d still offered web_search", o.calls)
	}
	if o.calls != o.max+1 {
		o.t.Fatalf("unexpected open count %d", o.calls)
	}
	return &sliceStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "final"},
		{Type: protocol.EventDone},
	}}, nil
}

func TestOpenLoopKeepsSearchToolUntilBudget(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sources":[{"url":"https://example.com","title":"Example"}]}`))
	}))
	t.Cleanup(sidecar.Close)
	client, err := New(Config{
		Enabled: true, ProviderID: "opencode-zen", Endpoint: sidecar.URL,
		AuthClass: "key", Wire: "openai-responses", ModelID: "deepseek-v4-flash",
		AllowedModels: []string{"deepseek-v4-flash"}, APIKey: "sidecar-key", HTTPClient: sidecar.Client(),
		MaxSearches: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	opener := &budgetOpener{t: t, max: 3}
	stream, err := OpenLoop(context.Background(), opener.Open, providers.DispatchRequest{
		Parsed: protocol.ParsedRequest{Context: protocol.Context{
			Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "search"}}}},
			Tools:    []protocol.Tool{SyntheticTool()},
		}},
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	var searches int
	for {
		ev, nextErr := stream.Next()
		if nextErr != nil {
			break
		}
		if ev.Type == protocol.EventWebSearchCallBegin {
			searches++
		}
	}
	_ = stream.Close()
	if searches != 3 {
		t.Fatalf("searches=%d", searches)
	}
	if opener.calls != 4 {
		t.Fatalf("opens=%d", opener.calls)
	}
}

func TestOpenLoopConcurrentTurnsReleaseContinuation(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sources":[{"url":"https://example.com","title":"Example"}]}`))
	}))
	t.Cleanup(sidecar.Close)
	client, err := New(Config{
		Enabled: true, ProviderID: "opencode-zen", Endpoint: sidecar.URL,
		AuthClass: "key", Wire: "openai-responses", ModelID: "deepseek-v4-flash",
		AllowedModels: []string{"deepseek-v4-flash"}, APIKey: "sidecar-key", HTTPClient: sidecar.Client(),
		MaxSearches: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr := resourcebudget.NewManager(resourcebudget.Limits{
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 1 << 20},
	})
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			turn, acquireErr := mgr.AcquireTurn(context.Background(), "")
			if acquireErr != nil {
				errCh <- acquireErr
				return
			}
			defer turn.Close()
			opens := 0
			stream, openErr := OpenLoop(context.Background(), func(_ context.Context, _ providers.DispatchRequest) (providers.EventStream, error) {
				opens++
				if opens == 1 {
					return &sliceStream{events: []protocol.Event{
						{Type: protocol.EventToolCallEnd, ID: "c1", Name: "web_search", Arguments: `{"query":"benes"}`},
						{Type: protocol.EventDone},
					}}, nil
				}
				return &sliceStream{events: []protocol.Event{
					{Type: protocol.EventTextDelta, Text: "ok"},
					{Type: protocol.EventDone},
				}}, nil
			}, providers.DispatchRequest{
				Parsed: protocol.ParsedRequest{Context: protocol.Context{
					Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "search"}}}},
					Tools:    []protocol.Tool{SyntheticTool()},
				}},
				Turn: turn,
			}, client)
			if openErr != nil {
				errCh <- openErr
				return
			}
			for {
				_, nextErr := stream.Next()
				if nextErr != nil {
					break
				}
			}
			_ = stream.Close()
		}()
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Fatal(e)
	}
	if got := mgr.Metrics().Bytes[resourcebudget.ClassContinuation]; got != 0 {
		t.Fatalf("leaked continuation bytes=%d", got)
	}
}
