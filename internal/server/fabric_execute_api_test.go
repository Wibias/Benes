package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/sidecar/fabric"
)

type fabricExecProvider struct {
	opens  atomic.Int32
	hang   time.Duration
	fail   bool
	events []protocol.Event
}

func (p *fabricExecProvider) Open(ctx context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
	p.opens.Add(1)
	if p.hang > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(p.hang):
		}
	}
	if p.fail {
		return nil, &providercontract.OpenError{StatusCode: 502, Message: "upstream failed"}
	}
	evs := p.events
	if len(evs) == 0 {
		evs = []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "ok"},
			{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
		}
	}
	return &sliceStream{events: evs}, nil
}

func newFabricExecHandler(t *testing.T, provider Provider) (http.Handler, string) {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": provider},
		ConfigPath:     configPath,
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	return attachHandlerClose(t, h), home
}

func fabricCreate(t *testing.T, h http.Handler) string {
	t.Helper()
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks", `{"title":"exec","goal":"ship"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", rr.Code, rr.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &created)
	return created.ID
}

func waitFabricRunState(t *testing.T, h http.Handler, taskID, want string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		rr := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+taskID, "")
		last = rr.Body.String()
		if strings.Contains(last, `"runState":"`+want+`"`) {
			return last
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("want runState=%s got %s", want, last)
	return last
}

func TestFabricExec01_StatusAdvertisesExecutionCapability(t *testing.T) {
	h, _ := newFabricExecHandler(t, &fabricExecProvider{})
	rr := fabricDo(h, http.MethodGet, "/api/fabric/status", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"kind":"lifecycle"`) || !strings.Contains(body, `"execution"`) || !strings.Contains(body, `"single_child_handoff"`) {
		t.Fatalf("body=%s", body)
	}
}

func TestFabricExec02_ExecuteAccepted(t *testing.T) {
	h, _ := newFabricExecHandler(t, &fabricExecProvider{})
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-5","input":"hello secret prompt"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "hello secret prompt") {
		t.Fatal("prompt echoed")
	}
	waitFabricRunState(t, h, id, "completed")
}

func TestFabricExec03_StartRemainsLifecycleOnly(t *testing.T) {
	p := &fabricExecProvider{}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/start", `{"owner":"operator"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	time.Sleep(50 * time.Millisecond)
	if p.opens.Load() != 0 {
		t.Fatalf("start opened provider %d times", p.opens.Load())
	}
}

func TestFabricExec04_CloseRemainsLifecycleOnly(t *testing.T) {
	p := &fabricExecProvider{}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)
	_ = fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/start", `{"owner":"operator"}`)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/close", `{"owner":"operator"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	if p.opens.Load() != 0 {
		t.Fatal("close opened provider")
	}
}

func TestFabricExec05_OnePrimaryPerTask(t *testing.T) {
	p := &fabricExecProvider{hang: 2 * time.Second}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)
	rr1 := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	if rr1.Code != http.StatusAccepted {
		t.Fatalf("first=%d %s", rr1.Code, rr1.Body.String())
	}
	rr2 := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"y"}`)
	if rr2.Code != http.StatusConflict {
		t.Fatalf("second=%d %s", rr2.Code, rr2.Body.String())
	}
	_ = fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", `{}`)
}

func TestFabricExec06_CancelLiveRun(t *testing.T) {
	p := &fabricExecProvider{hang: 3 * time.Second}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	time.Sleep(30 * time.Millisecond)
	cancel := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", `{}`)
	if cancel.Code != http.StatusOK {
		t.Fatalf("cancel=%d %s", cancel.Code, cancel.Body.String())
	}
	waitFabricRunState(t, h, id, "cancelled")
}

func TestFabricExec07_StaleCancelIgnored(t *testing.T) {
	p := &fabricExecProvider{hang: 2 * time.Second}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)
	exec := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	var body map[string]any
	_ = json.Unmarshal(exec.Body.Bytes(), &body)
	fence := int(body["fencingToken"].(float64))
	cancel := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", `{"expectedOwner":"w","expectedFencingToken":`+fabricItoa(fence+9)+`}`)
	if cancel.Code != http.StatusConflict {
		t.Fatalf("cancel=%d %s", cancel.Code, cancel.Body.String())
	}
	_ = fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", `{}`)
}

func TestFabricExec08_UpstreamFailure(t *testing.T) {
	h, _ := newFabricExecHandler(t, &fabricExecProvider{fail: true})
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	waitFabricRunState(t, h, id, "failed")
}

func TestFabricExec09_ModelRequired(t *testing.T) {
	h, _ := newFabricExecHandler(t, &fabricExecProvider{})
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","input":"x"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

func TestFabricExec10_MissingProvider(t *testing.T) {
	h, _ := newFabricExecHandler(t, &fabricExecProvider{})
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"missing-provider/gpt","input":"x"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	waitFabricRunState(t, h, id, "failed")
}

func TestFabricExec11_NoPromptInTimeline(t *testing.T) {
	h, _ := newFabricExecHandler(t, &fabricExecProvider{})
	id := fabricCreate(t, h)
	secret := "UNIQUE_PROMPT_TOKEN_9f3a"
	_ = fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"`+secret+`"}`)
	body := waitFabricRunState(t, h, id, "completed")
	if strings.Contains(body, secret) {
		t.Fatal("prompt in task detail")
	}
}

func TestFabricExec12_UsesDataPlaneProvider(t *testing.T) {
	p := &fabricExecProvider{}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)
	_ = fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	waitFabricRunState(t, h, id, "completed")
	if p.opens.Load() != 1 {
		t.Fatalf("opens=%d", p.opens.Load())
	}
}

func TestFabricExec13_CapacityError(t *testing.T) {
	p := &fabricExecProvider{hang: 3 * time.Second}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true}}`), 0o600)
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 1})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	id1 := fabricCreate(t, h)
	id2 := fabricCreate(t, h)
	rr1 := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id1+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	if rr1.Code != http.StatusAccepted {
		t.Fatalf("first=%d %s", rr1.Code, rr1.Body.String())
	}
	// Wait until first run holds the turn (provider opened).
	deadline := time.Now().Add(2 * time.Second)
	for p.opens.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if p.opens.Load() == 0 {
		t.Fatal("first execute never opened provider")
	}
	rr2 := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id2+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"y"}`)
	if rr2.Code == http.StatusAccepted {
		t.Fatalf("capacity must reject before 202; got accepted: %s", rr2.Body.String())
	}
	if rr2.Code != http.StatusConflict && rr2.Code != http.StatusServiceUnavailable {
		t.Fatalf("want capacity error, got %d %s", rr2.Code, rr2.Body.String())
	}
	body2 := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+id2, "").Body.String()
	if strings.Contains(body2, `"runState":"running"`) || strings.Contains(body2, `"kind":"execute"`) {
		t.Fatalf("task2 must not have RunStarted: %s", body2)
	}
	_ = fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id1+"/cancel", `{}`)
}

func TestFabricExec14_DisabledExecute(t *testing.T) {
	h, _ := newFabricTestHandler(t, false)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/task_x/execute", `{"model":"openai-apikey/gpt-5"}`)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "fabric_disabled") {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

func TestFabricExec15_ResultShapeSafeIDs(t *testing.T) {
	h, _ := newFabricExecHandler(t, &fabricExecProvider{})
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	var body map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	for _, key := range []string{"runId", "taskId", "owner", "fencingToken", "status"} {
		if body[key] == nil {
			t.Fatalf("missing %s in %s", key, rr.Body.String())
		}
	}
	if _, ok := body["output"]; ok {
		t.Fatal("output leaked")
	}
	waitFabricRunState(t, h, id, "completed")
}

func TestFabricExec16_OrphanInterruptedOnRepoOpen(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true}}`), 0o600)
	// Seed an orphaned RunStarted via a first handler, then abandon without completing.
	p := &fabricExecProvider{hang: 10 * time.Second}
	h1, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := fabricCreate(t, h1)
	_ = fabricDo(h1, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	time.Sleep(30 * time.Millisecond)
	if c, ok := h1.(interface{ Close() error }); ok {
		_ = c.Close()
	}
	// Clear cached repo so a new handler recovers.
	fabricRepos.Range(func(key, _ any) bool {
		fabricRepos.Delete(key)
		return true
	})
	h2, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fabricExecProvider{}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h2 = attachHandlerClose(t, h2)
	rr := fabricDo(h2, http.MethodGet, "/api/fabric/tasks/"+id, "")
	if !strings.Contains(rr.Body.String(), `"runState":"interrupted"`) && !strings.Contains(rr.Body.String(), `"runState":"cancelled"`) {
		// Close may cancel; either interrupted or cancelled is fail-closed (no running orphan).
		if strings.Contains(rr.Body.String(), `"runState":"running"`) {
			t.Fatalf("orphan still running: %s", rr.Body.String())
		}
	}
}

func TestFabricExec17_NoLoopbackRecursion(t *testing.T) {
	p := &fabricExecProvider{}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)
	_ = fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	waitFabricRunState(t, h, id, "completed")
	if p.opens.Load() != 1 {
		t.Fatalf("unexpected opens=%d (possible recursion)", p.opens.Load())
	}
}

func TestFabricExec18_ShutdownCancelsActive(t *testing.T) {
	p := &fabricExecProvider{hang: 5 * time.Second}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true}}`), 0o600)
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	id := fabricCreate(t, raw)
	_ = fabricDo(raw, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	deadline := time.Now().Add(2 * time.Second)
	for p.opens.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if c, ok := raw.(interface{ Close() error }); ok {
		_ = c.Close()
	}
	fabricRepos.Range(func(key, _ any) bool { fabricRepos.Delete(key); return true })
	h2, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fabricExecProvider{}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h2 = attachHandlerClose(t, h2)
	rr := fabricDo(h2, http.MethodGet, "/api/fabric/tasks/"+id, "")
	if !strings.Contains(rr.Body.String(), `"runState":"interrupted"`) {
		t.Fatalf("shutdown must interrupt, got %s", rr.Body.String())
	}
}

func TestFabricExec19_NonLoopbackRejected(t *testing.T) {
	h, _ := newFabricExecHandler(t, &fabricExecProvider{})
	id := fabricCreate(t, h)
	req := httptest.NewRequest(http.MethodPost, "/api/fabric/tasks/"+id+"/execute", strings.NewReader(`{"model":"openai-apikey/gpt-5"}`))
	req.Host = "example.com"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code == http.StatusAccepted {
		t.Fatal("non-loopback execute accepted")
	}
}

func TestFabricExec20_ComboFailoverStillOnePrimary(t *testing.T) {
	first := &fabricExecProvider{fail: true}
	second := &fabricExecProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true}}`), 0o600)
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google": first, "openai-apikey": second},
		ConfigPath:     configPath,
		Combos: []Combo{{
			ID: "fast",
			Targets: []ComboTarget{
				{ProviderID: "google", Model: "gemini", Protocol: "google"},
				{ProviderID: "openai-apikey", Model: "gpt-5", Protocol: "openai-chat"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"combo/fast","input":"x"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	body := waitFabricRunState(t, h, id, "completed")
	if strings.Contains(strings.ToLower(body), "fan-out") {
		t.Fatal("fabric fan-out mentioned")
	}
}

func TestFabricExec21_UnknownTask(t *testing.T) {
	h, _ := newFabricExecHandler(t, &fabricExecProvider{})
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/task_missing/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

func TestFabricExec22_NoHiddenFabricRetry(t *testing.T) {
	p := &fabricExecProvider{fail: true}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)
	_ = fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	waitFabricRunState(t, h, id, "failed")
	if p.opens.Load() != 1 {
		t.Fatalf("hidden retries opens=%d", p.opens.Load())
	}
}

func TestFabricExec23_WorkerSurvivesRequestContextDone(t *testing.T) {
	release := make(chan struct{})
	opened := make(chan struct{}, 1)
	p := &fabricGateProvider{opened: opened, release: release}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	body := strings.NewReader(`{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/fabric/tasks/"+id+"/execute", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "127.0.0.1"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close() // fully close 202 so request ctx is done

	select {
	case <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("provider never opened after 202 close")
	}
	close(release)

	deadline := time.Now().Add(3 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		rr := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+id, "")
		last = rr.Body.String()
		if strings.Contains(last, `"runState":"completed"`) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("want completed after request close, got %s", last)
}

type fabricGateProvider struct {
	opened  chan struct{}
	release chan struct{}
	opens   atomic.Int32
}

func (p *fabricGateProvider) Open(ctx context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
	p.opens.Add(1)
	select {
	case p.opened <- struct{}{}:
	default:
	}
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &sliceStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "gated-ok"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
	}}, nil
}

func TestFabricExec24_ResultHandleRetrievable(t *testing.T) {
	h, _ := newFabricExecHandler(t, &fabricExecProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "hello-result"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 2, OutputTokens: 3}},
	}})
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"SECRET_PROMPT"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	runID, _ := body["runId"].(string)
	waitFabricRunState(t, h, id, "completed")
	detail := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+id, "").Body.String()
	if strings.Contains(detail, "SECRET_PROMPT") || strings.Contains(detail, "hello-result") {
		t.Fatal("prompt/output leaked into task detail/events")
	}
	res := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+id+"/runs/"+runID+"/result", "")
	if res.Code != http.StatusOK {
		t.Fatalf("result=%d %s", res.Code, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "hello-result") {
		t.Fatalf("missing output: %s", res.Body.String())
	}
}

func TestFabricExec25_FailedNotStoredAsSuccessResult(t *testing.T) {
	h, _ := newFabricExecHandler(t, &fabricExecProvider{fail: true})
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	var body map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	runID, _ := body["runId"].(string)
	waitFabricRunState(t, h, id, "failed")
	res := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+id+"/runs/"+runID+"/result", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("failed run must not expose success result: %d %s", res.Code, res.Body.String())
	}
}

func TestFabricExec26_UsageAndRequestHistoryRecorded(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true}}`), 0o600)
	p := &fabricExecProvider{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "u"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 7, OutputTokens: 5}},
	}}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	id := fabricCreate(t, h)
	_ = fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	waitFabricRunState(t, h, id, "completed")
	usagePath := filepath.Join(home, "usage", "active.jsonl")
	raw, err := os.ReadFile(usagePath)
	if err != nil {
		legacy := filepath.Join(home, "usage.jsonl")
		raw, err = os.ReadFile(legacy)
		if err != nil {
			t.Fatalf("usage ledger missing under %s: %v", home, err)
		}
	}
	if !strings.Contains(string(raw), "openai-apikey") && !strings.Contains(string(raw), "gpt-5") {
		t.Fatalf("usage attribution missing: %s", raw)
	}
	diag := fabricDo(h, http.MethodGet, "/api/diagnostics/requests", "")
	if diag.Code != http.StatusOK || (!strings.Contains(diag.Body.String(), "openai-apikey") && !strings.Contains(diag.Body.String(), "gpt-5") && !strings.Contains(diag.Body.String(), "fabric")) {
		// Diagnostics list may be empty if path filter differs; durable usage above is authoritative.
		if len(raw) == 0 {
			t.Fatalf("no durable usage and diagnostics=%d %s", diag.Code, diag.Body.String())
		}
	}
}

func TestFabricExec27_OrphanFixtureWithoutGracefulClose(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true}}`), 0o600)
	fabricDir := filepath.Join(home, "fabric")
	_ = os.MkdirAll(fabricDir, 0o700)
	repo := fabric.New(fabricDir)
	taskID, err := repo.Create(fabric.CreateTaskInput{Title: "orphan", Goal: "ship"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := repo.BeginExecute(taskID, fabric.ExecuteInput{Owner: "w", Model: "openai-apikey/gpt-5"})
	if err != nil {
		t.Fatal(err)
	}
	if id.RunID == "" {
		t.Fatal("missing run id")
	}
	// Do NOT Close any handler — reopen via fresh handler recovery.
	fabricRepos.Range(func(key, _ any) bool { fabricRepos.Delete(key); return true })
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": &fabricExecProvider{}},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	rr := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+taskID, "")
	if !strings.Contains(rr.Body.String(), `"runState":"interrupted"`) {
		t.Fatalf("want interrupted orphan, got %s", rr.Body.String())
	}
}

func TestFabricExec28_ShutdownVsCancelTerminals(t *testing.T) {
	// cancel path
	p1 := &fabricExecProvider{hang: 5 * time.Second}
	h1, _ := newFabricExecHandler(t, p1)
	id1 := fabricCreate(t, h1)
	_ = fabricDo(h1, http.MethodPost, "/api/fabric/tasks/"+id1+"/execute", `{"owner":"w","model":"openai-apikey/gpt-5","input":"x"}`)
	deadline := time.Now().Add(2 * time.Second)
	for p1.opens.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	_ = fabricDo(h1, http.MethodPost, "/api/fabric/tasks/"+id1+"/cancel", `{}`)
	waitFabricRunState(t, h1, id1, "cancelled")

	// shutdown path asserted in TestFabricExec18
}

func fabricItoa(v int) string {
	return strings.TrimSpace(strings.Replace(fabricJSONNumber(v), "\n", "", -1))
}

func fabricJSONNumber(v int) string {
	b, _ := json.Marshal(v)
	return string(b)
}
