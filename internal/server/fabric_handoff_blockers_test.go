package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/sidecar/fabric"
)

func TestFabricHandoffLive_OutboundCommitAppendFailureConvergesInterrupted(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4}),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := raw.(*handler)
	t.Cleanup(func() { _ = h.Close() })
	handler := http.Handler(h)

	var sawActiveDuringFail atomic.Bool
	var runID atomic.Value
	fabric.SetAppendLockedHookForTest(func(taskID, eventType string) error {
		if eventType != fabric.EventHandoffCommitted {
			return nil
		}
		rid, _ := runID.Load().(string)
		if rid != "" && h.fabricRuntime.hasActive(taskID, rid) {
			sawActiveDuringFail.Store(true)
		}
		return errors.New("injected outbound commit append failure")
	})
	t.Cleanup(func() { fabric.SetAppendLockedHookForTest(nil) })

	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		RunID string `json:"runId"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)
	runID.Store(accepted.RunID)

	waitFabricRunState(t, handler, id, "interrupted")
	if h.fabricRuntime.hasActive(id, accepted.RunID) {
		t.Fatal("active entry must be gone only after convergence")
	}
	if !sawActiveDuringFail.Load() {
		t.Fatal("expected active entry still present at half-commit failure (before convergence)")
	}
	if p.opens.Load() != 1 {
		t.Fatalf("child provider Open must not run; opens=%d", p.opens.Load())
	}
	detail := fabricDo(handler, http.MethodGet, "/api/fabric/tasks/"+id, "")
	if strings.Contains(detail.Body.String(), "ChildRunStarted") {
		t.Fatalf("ChildRunStarted must not appear: %s", detail.Body.String())
	}

	// Fresh fencing: new execute can acquire after interrupt released the lease.
	rr2 := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker2","model":"openai-apikey/gpt-primary","input":"again"}`)
	if rr2.Code != http.StatusAccepted {
		t.Fatalf("fresh execute after interrupt: %d %s", rr2.Code, rr2.Body.String())
	}
	waitFabricRunState(t, handler, id, "completed")
}

func TestFabricHandoffLive_ChildStartedAppendFailureConvergesInterrupted(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600)
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4}),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := raw.(*handler)
	t.Cleanup(func() { _ = h.Close() })
	handler := http.Handler(h)

	var sawActive atomic.Bool
	var runID atomic.Value
	fabric.SetAppendLockedHookForTest(func(taskID, eventType string) error {
		if eventType != fabric.EventChildRunStarted {
			return nil
		}
		rid, _ := runID.Load().(string)
		if rid != "" && h.fabricRuntime.hasActive(taskID, rid) {
			sawActive.Store(true)
		}
		return errors.New("injected child started append failure")
	})
	t.Cleanup(func() { fabric.SetAppendLockedHookForTest(nil) })

	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		RunID string `json:"runId"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)
	runID.Store(accepted.RunID)
	waitFabricRunState(t, handler, id, "interrupted")
	if h.fabricRuntime.hasActive(id, accepted.RunID) {
		t.Fatal("active must be gone after convergence")
	}
	if !sawActive.Load() {
		t.Fatal("active should remain until convergence")
	}
	if p.opens.Load() != 1 {
		t.Fatalf("opens=%d want 1", p.opens.Load())
	}
	rr2 := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker2","model":"openai-apikey/gpt-primary","input":"again"}`)
	if rr2.Code != http.StatusAccepted {
		t.Fatalf("fresh fencing failed: %d %s", rr2.Code, rr2.Body.String())
	}
	waitFabricRunState(t, handler, id, "completed")
}

func TestFabricHandoffLive_ReturnCommitAppendFailureConvergesInterrupted(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600)
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4}),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := raw.(*handler)
	t.Cleanup(func() { _ = h.Close() })
	handler := http.Handler(h)

	var commits atomic.Int32
	var sawActive atomic.Bool
	var runID atomic.Value
	fabric.SetAppendLockedHookForTest(func(taskID, eventType string) error {
		if eventType != fabric.EventHandoffCommitted {
			return nil
		}
		n := commits.Add(1)
		if n < 2 {
			return nil // parent->child commit ok
		}
		rid, _ := runID.Load().(string)
		if rid != "" && h.fabricRuntime.hasActive(taskID, rid) {
			sawActive.Store(true)
		}
		return errors.New("injected return commit append failure")
	})
	t.Cleanup(func() { fabric.SetAppendLockedHookForTest(nil) })

	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		RunID string `json:"runId"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)
	runID.Store(accepted.RunID)
	waitFabricRunState(t, handler, id, "interrupted")
	if h.fabricRuntime.hasActive(id, accepted.RunID) {
		t.Fatal("active must be gone after convergence")
	}
	if !sawActive.Load() {
		t.Fatal("active should remain until return half-commit convergence")
	}
	if p.opens.Load() != 2 {
		t.Fatalf("opens=%d want 2 (primary+child, no continuation)", p.opens.Load())
	}
	rr2 := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker2","model":"openai-apikey/gpt-primary","input":"again"}`)
	if rr2.Code != http.StatusAccepted {
		t.Fatalf("fresh fencing failed: %d %s", rr2.Code, rr2.Body.String())
	}
	waitFabricRunState(t, handler, id, "completed")
}

func TestFabricHandoffRace_HandoffWinsVsCancel409(t *testing.T) {
	p := &fabricHandoffProvider{}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)

	var cancelCode atomic.Int32
	var cancelDone sync.WaitGroup
	var parentOwner string
	var parentFence int
	var metaMu sync.Mutex

	SetFabricAuthorityHandoffTestHooks(nil, func() {
		metaMu.Lock()
		owner, fence := parentOwner, parentFence
		metaMu.Unlock()
		cancelDone.Add(1)
		go func() {
			defer cancelDone.Done()
			body := `{"expectedOwner":"` + owner + `","expectedFencingToken":` + strconv.Itoa(fence) + `}`
			cr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", body)
			cancelCode.Store(int32(cr.Code))
		}()
	})
	t.Cleanup(func() { SetFabricAuthorityHandoffTestHooks(nil, nil) })

	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		Owner        string `json:"owner"`
		FencingToken int    `json:"fencingToken"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)
	metaMu.Lock()
	parentOwner, parentFence = accepted.Owner, accepted.FencingToken
	metaMu.Unlock()

	waitFabricRunState(t, h, id, "completed")
	cancelDone.Wait()
	if cancelCode.Load() != http.StatusConflict {
		t.Fatalf("handoff-wins cancel want 409 got %d", cancelCode.Load())
	}
}

func TestFabricHandoffRace_CancelWinsVsHandoffNoChild(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600)
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4}),
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	t.Cleanup(func() { _ = hh.Close() })
	h := http.Handler(hh)

	id := fabricCreate(t, h)
	atBoundary := make(chan struct{})
	release := make(chan struct{})
	cancelSelected := make(chan struct{})
	var boundaryOnce, selectedOnce sync.Once

	SetFabricAuthorityHandoffTestHooks(func() {
		boundaryOnce.Do(func() { close(atBoundary) })
		<-release
	}, nil)
	SetFabricAfterCancelSelectedTestHook(func() {
		selectedOnce.Do(func() { close(cancelSelected) })
	})
	t.Cleanup(func() {
		SetFabricAuthorityHandoffTestHooks(nil, nil)
		SetFabricAfterCancelSelectedTestHook(nil)
		select {
		case <-release:
		default:
			close(release)
		}
	})

	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		Owner        string `json:"owner"`
		FencingToken int    `json:"fencingToken"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)

	select {
	case <-atBoundary:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting authority boundary")
	}

	cancelCode := make(chan int, 1)
	go func() {
		body := `{"expectedOwner":"` + accepted.Owner + `","expectedFencingToken":` + strconv.Itoa(accepted.FencingToken) + `}`
		cr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", body)
		cancelCode <- cr.Code
	}()

	select {
	case <-cancelSelected:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting cancel to select intent")
	}
	close(release)

	select {
	case code := <-cancelCode:
		if code != http.StatusOK {
			t.Fatalf("cancel-wins want 200 got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting cancel HTTP")
	}
	waitFabricRunState(t, h, id, "cancelled")
	if p.opens.Load() != 1 {
		t.Fatalf("child provider Open must not run; opens=%d", p.opens.Load())
	}
	detail := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+id, "")
	if strings.Contains(detail.Body.String(), "ChildRunStarted") {
		t.Fatalf("ChildRunStarted leaked: %s", detail.Body.String())
	}
}

func TestFabricHandoffRace_ReturnHandoffWinsVsStaleChildCancel(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600)
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4}),
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	t.Cleanup(func() { _ = hh.Close() })
	h := http.Handler(hh)
	id := fabricCreate(t, h)

	var cancelCode atomic.Int32
	var cancelDone sync.WaitGroup
	var childOwner string
	var childFence int
	var childMu sync.Mutex

	SetFabricAuthorityHandoffTestHooks(nil, func() {
		repo, err := hh.fabricRepo()
		if err != nil {
			return
		}
		owner, fence, err := repo.LeaseSnapshot(id)
		if err != nil || owner == "" || fence <= 0 {
			return
		}
		childMu.Lock()
		childOwner, childFence = owner, fence
		childMu.Unlock()
	})
	SetFabricAuthorityReturnTestHooks(nil, func() {
		childMu.Lock()
		owner, fence := childOwner, childFence
		childMu.Unlock()
		if owner == "" || fence <= 0 {
			return
		}
		cancelDone.Add(1)
		go func() {
			defer cancelDone.Done()
			body := `{"expectedOwner":"` + owner + `","expectedFencingToken":` + strconv.Itoa(fence) + `}`
			cr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", body)
			cancelCode.Store(int32(cr.Code))
		}()
	})
	t.Cleanup(func() {
		SetFabricAuthorityHandoffTestHooks(nil, nil)
		SetFabricAuthorityReturnTestHooks(nil, nil)
	})

	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	waitFabricRunState(t, h, id, "completed")
	cancelDone.Wait()
	if cancelCode.Load() != http.StatusConflict {
		t.Fatalf("stale child cancel want 409 got %d", cancelCode.Load())
	}
}

func TestFabricHandoff_ContinuationBytesMaxProcessRejected(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600)
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns:  4,
		MaxProcessBytes: 1 << 20,
		MaxTurnBytes:    48,
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := attachHandlerClose(t, h)
	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	waitFabricRunState(t, handler, id, "failed")
	if got := budget.Metrics().ProcessBytes; got != 0 {
		t.Fatalf("ProcessBytes=%d want 0 after rejection", got)
	}
	if got := budget.Metrics().ActiveTurns; got != 0 {
		t.Fatalf("ActiveTurns=%d", got)
	}
}

func TestFabricHandoff_ContinuationBytesSuccessReleases(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600)
	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns:  4,
		MaxProcessBytes: 1 << 20,
		MaxTurnBytes:    1 << 20,
	})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := attachHandlerClose(t, h)
	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	waitFabricRunState(t, handler, id, "completed")
	if got := budget.Metrics().ProcessBytes; got != 0 {
		t.Fatalf("ProcessBytes=%d want 0", got)
	}
	if got := budget.Metrics().ActiveTurns; got != 0 {
		t.Fatalf("ActiveTurns=%d", got)
	}
}

func TestFabricContinuationVisibleBytesHelper(t *testing.T) {
	parsed := buildFabricContinuationParsed("m", "hello", modelTurnToolCall{
		CallID: "c1", Name: fabric.FabricDelegateToolName, Arguments: `{"instruction":"x"}`,
	}, "out", false, fabricContinuationPlan{Mode: fabricContGoogleReplay})
	n := fabricContinuationVisibleBytes(parsed)
	if n <= 0 {
		t.Fatalf("bytes=%d", n)
	}
}

func TestFabricHandoffRace_NoPreconditionCancelWinsBeforeOutbound(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600)
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4}),
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	t.Cleanup(func() { _ = hh.Close() })
	h := http.Handler(hh)
	id := fabricCreate(t, h)

	atBoundary := make(chan struct{})
	release := make(chan struct{})
	cancelSelected := make(chan struct{})
	var boundaryOnce, selectedOnce sync.Once

	SetFabricAuthorityHandoffTestHooks(func() {
		boundaryOnce.Do(func() { close(atBoundary) })
		<-release
	}, nil)
	SetFabricAfterCancelSelectedTestHook(func() {
		selectedOnce.Do(func() { close(cancelSelected) })
	})
	t.Cleanup(func() {
		SetFabricAuthorityHandoffTestHooks(nil, nil)
		SetFabricAfterCancelSelectedTestHook(nil)
		select {
		case <-release:
		default:
			close(release)
		}
	})

	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}

	select {
	case <-atBoundary:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting authority boundary")
	}

	cancelCode := make(chan int, 1)
	go func() {
		cr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", `{}`)
		cancelCode <- cr.Code
	}()

	select {
	case <-cancelSelected:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting cancel to select intent")
	}
	close(release)

	select {
	case code := <-cancelCode:
		if code != http.StatusOK {
			t.Fatalf("no-precondition cancel-wins want 200 got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting cancel HTTP")
	}
	waitFabricRunState(t, h, id, "cancelled")
	if p.opens.Load() != 1 {
		t.Fatalf("child provider Open must not run; opens=%d", p.opens.Load())
	}
	detail := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+id, "")
	if strings.Contains(detail.Body.String(), "ChildRunStarted") {
		t.Fatalf("ChildRunStarted leaked: %s", detail.Body.String())
	}
}

func TestFabricHandoffRace_OutboundWinsThenNoPreconditionCancelStill200(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600)
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4}),
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	t.Cleanup(func() { _ = hh.Close() })
	h := http.Handler(hh)
	id := fabricCreate(t, h)

	atPublish := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	// Hold after durable outbound CAS, before child claim publish completes unlocking.
	SetFabricAuthorityHandoffTestHooks(nil, func() {
		once.Do(func() { close(atPublish) })
		<-release
	})
	t.Cleanup(func() {
		SetFabricAuthorityHandoffTestHooks(nil, nil)
		select {
		case <-release:
		default:
			close(release)
		}
	})

	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}

	select {
	case <-atPublish:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting outbound CAS boundary")
	}

	cancelCode := make(chan int, 1)
	go func() {
		cr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", `{}`)
		cancelCode <- cr.Code
	}()
	// Give cancel time to block on authorityMu behind the handoff holder.
	time.Sleep(50 * time.Millisecond)
	close(release)

	select {
	case code := <-cancelCode:
		if code != http.StatusOK {
			t.Fatalf("no-precondition cancel after outbound want 200 got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting cancel HTTP")
	}
	waitFabricRunState(t, h, id, "cancelled")
}

func TestFabricHandoffRace_ReturnWinsThenNoPreconditionCancelOnParent(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600)
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4}),
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	t.Cleanup(func() { _ = hh.Close() })
	h := http.Handler(hh)
	id := fabricCreate(t, h)

	atPublish := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	SetFabricAuthorityReturnTestHooks(nil, func() {
		once.Do(func() { close(atPublish) })
		<-release
	})
	t.Cleanup(func() {
		SetFabricAuthorityReturnTestHooks(nil, nil)
		select {
		case <-release:
		default:
			close(release)
		}
	})

	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}

	select {
	case <-atPublish:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting return CAS boundary")
	}

	cancelCode := make(chan int, 1)
	go func() {
		cr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", `{}`)
		cancelCode <- cr.Code
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)

	select {
	case code := <-cancelCode:
		if code != http.StatusOK {
			t.Fatalf("no-precondition cancel on parent N+2 want 200 got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting cancel HTTP")
	}
	waitFabricRunState(t, h, id, "cancelled")
}

func TestFabricHandoffRace_ExplicitStaleParentAfterChildStill409(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600)
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4}),
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	t.Cleanup(func() { _ = hh.Close() })
	h := http.Handler(hh)
	id := fabricCreate(t, h)

	var parentOwner string
	var parentFence int
	atPublish := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var metaMu sync.Mutex

	SetFabricAuthorityHandoffTestHooks(nil, func() {
		once.Do(func() { close(atPublish) })
		<-release
	})
	t.Cleanup(func() {
		SetFabricAuthorityHandoffTestHooks(nil, nil)
		select {
		case <-release:
		default:
			close(release)
		}
	})

	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		Owner        string `json:"owner"`
		FencingToken int    `json:"fencingToken"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)
	metaMu.Lock()
	parentOwner, parentFence = accepted.Owner, accepted.FencingToken
	metaMu.Unlock()

	select {
	case <-atPublish:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting child publish boundary")
	}

	cancelCode := make(chan int, 1)
	go func() {
		metaMu.Lock()
		body := `{"expectedOwner":"` + parentOwner + `","expectedFencingToken":` + strconv.Itoa(parentFence) + `}`
		metaMu.Unlock()
		cr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", body)
		cancelCode <- cr.Code
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)

	select {
	case code := <-cancelCode:
		if code != http.StatusConflict {
			t.Fatalf("stale parent N after N+1 want 409 got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting cancel HTTP")
	}
	waitFabricRunState(t, h, id, "completed")
}

func TestFabricHandoffRace_ExplicitStaleChildAfterReturnStill409(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	_ = os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600)
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4}),
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	t.Cleanup(func() { _ = hh.Close() })
	h := http.Handler(hh)
	id := fabricCreate(t, h)

	var childOwner string
	var childFence int
	var childMu sync.Mutex
	atPublish := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	SetFabricAuthorityHandoffTestHooks(nil, func() {
		repo, err := hh.fabricRepo()
		if err != nil {
			return
		}
		owner, fence, err := repo.LeaseSnapshot(id)
		if err != nil || owner == "" || fence <= 0 {
			return
		}
		childMu.Lock()
		childOwner, childFence = owner, fence
		childMu.Unlock()
	})
	SetFabricAuthorityReturnTestHooks(nil, func() {
		once.Do(func() { close(atPublish) })
		<-release
	})
	t.Cleanup(func() {
		SetFabricAuthorityHandoffTestHooks(nil, nil)
		SetFabricAuthorityReturnTestHooks(nil, nil)
		select {
		case <-release:
		default:
			close(release)
		}
	})

	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}

	select {
	case <-atPublish:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting return publish boundary")
	}

	cancelCode := make(chan int, 1)
	go func() {
		childMu.Lock()
		body := `{"expectedOwner":"` + childOwner + `","expectedFencingToken":` + strconv.Itoa(childFence) + `}`
		childMu.Unlock()
		cr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", body)
		cancelCode <- cr.Code
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)

	select {
	case code := <-cancelCode:
		if code != http.StatusConflict {
			t.Fatalf("stale child N+1 after N+2 want 409 got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting cancel HTTP")
	}
	waitFabricRunState(t, h, id, "completed")
}

func assertNoOpenHandoffProposal(t *testing.T, repo *fabric.Repo, taskID string) {
	t.Helper()
	detail, err := repo.Get(taskID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var openID string
	for _, ev := range detail.Events {
		pl := map[string]any{}
		_ = json.Unmarshal(ev.Payload, &pl)
		hid, _ := pl["handoff_id"].(string)
		if hid == "" {
			if s, ok := pl["handoffId"].(string); ok {
				hid = s
			}
		}
		switch ev.EventType {
		case fabric.EventHandoffProposed:
			if hid != "" {
				openID = hid
			}
		case fabric.EventHandoffCommitted, fabric.EventHandoffFailed, fabric.EventHandoffRolledBack:
			if openID != "" && (hid == "" || hid == openID) {
				openID = ""
			}
		}
	}
	if openID != "" {
		t.Fatalf("open half-committed handoff remains: %s", openID)
	}
}

func TestFabricHandoffLive_OutboundCommitAppendFailureConcurrentShutdownConverges(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4})
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	handler := http.Handler(hh)

	var shutdownWon atomic.Bool
	fabric.SetAppendLockedHookForTest(func(taskID, eventType string) error {
		if eventType != fabric.EventHandoffCommitted {
			return nil
		}
		// Mirror shutdown selecting fabricIntentShutdown without authorityMu while
		// handoff still holds authorityMu inside BeginChildHandoff (post-CAS).
		if hh.fabricRuntime.selectTerminalForTest(taskID, fabricIntentShutdown) {
			shutdownWon.Store(true)
		}
		return errors.New("injected outbound commit append failure concurrent shutdown")
	})
	t.Cleanup(func() { fabric.SetAppendLockedHookForTest(nil) })

	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		RunID string `json:"runId"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)

	body := waitFabricRunState(t, handler, id, "interrupted")
	if !shutdownWon.Load() {
		t.Fatal("expected shutdown intent to win during outbound half-commit")
	}
	if hh.fabricRuntime.hasActive(id, accepted.RunID) {
		t.Fatal("live entry must be gone after in-process convergence")
	}
	if p.opens.Load() != 1 {
		t.Fatalf("child/continuation must not run; opens=%d", p.opens.Load())
	}
	if strings.Contains(body, "ChildRunStarted") {
		t.Fatalf("ChildRunStarted must not appear: %s", body)
	}
	if p.seen.Load() != 0 {
		t.Fatalf("tool-result continuation must not run; seen=%d", p.seen.Load())
	}

	repo, err := hh.fabricRepo()
	if err != nil {
		t.Fatal(err)
	}
	assertNoOpenHandoffProposal(t, repo, id)
	owner, fence, err := repo.LeaseSnapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if owner != "" {
		t.Fatalf("lease owner must be cleared after convergence; owner=%q fence=%d", owner, fence)
	}
	if fence <= 0 {
		t.Fatalf("released lease must retain bumped fencing token; fence=%d", fence)
	}
	n, rerr := repo.RecoverOrphans()
	if rerr != nil {
		t.Fatalf("RecoverOrphans err=%v", rerr)
	}
	if n != 0 {
		t.Fatalf("RecoverOrphans must not be required; n=%d", n)
	}

	if err := hh.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := budget.Metrics().ActiveTurns; got != 0 {
		t.Fatalf("ActiveTurns=%d want 0", got)
	}
	if hh.fabricRuntime.activeCountForTest() != 0 || hh.fabricRuntime.inflightForTest() != 0 {
		t.Fatalf("runtime leak active=%d inflight=%d", hh.fabricRuntime.activeCountForTest(), hh.fabricRuntime.inflightForTest())
	}
}

func TestFabricHandoffLive_ReturnCommitAppendFailureConcurrentShutdownConverges(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4})
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	handler := http.Handler(hh)

	var commits atomic.Int32
	var shutdownWon atomic.Bool
	fabric.SetAppendLockedHookForTest(func(taskID, eventType string) error {
		if eventType != fabric.EventHandoffCommitted {
			return nil
		}
		n := commits.Add(1)
		if n < 2 {
			return nil // parent->child commit ok
		}
		if hh.fabricRuntime.selectTerminalForTest(taskID, fabricIntentShutdown) {
			shutdownWon.Store(true)
		}
		return errors.New("injected return commit append failure concurrent shutdown")
	})
	t.Cleanup(func() { fabric.SetAppendLockedHookForTest(nil) })

	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		RunID string `json:"runId"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)

	waitFabricRunState(t, handler, id, "interrupted")
	if !shutdownWon.Load() {
		t.Fatal("expected shutdown intent to win during return half-commit")
	}
	if hh.fabricRuntime.hasActive(id, accepted.RunID) {
		t.Fatal("live entry must be gone after in-process convergence")
	}
	if p.opens.Load() != 2 {
		t.Fatalf("opens=%d want 2 (primary+child, no continuation)", p.opens.Load())
	}
	if p.seen.Load() != 0 {
		t.Fatalf("continuation tool-result must not run; seen=%d", p.seen.Load())
	}

	repo, err := hh.fabricRepo()
	if err != nil {
		t.Fatal(err)
	}
	assertNoOpenHandoffProposal(t, repo, id)
	owner, fence, err := repo.LeaseSnapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if owner != "" {
		t.Fatalf("lease owner must be cleared after convergence; owner=%q fence=%d", owner, fence)
	}
	if fence <= 0 {
		t.Fatalf("released lease must retain bumped fencing token; fence=%d", fence)
	}
	n, rerr := repo.RecoverOrphans()
	if rerr != nil {
		t.Fatalf("RecoverOrphans err=%v", rerr)
	}
	if n != 0 {
		t.Fatalf("RecoverOrphans must not be required; n=%d", n)
	}

	if err := hh.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := budget.Metrics().ActiveTurns; got != 0 {
		t.Fatalf("ActiveTurns=%d want 0", got)
	}
	if hh.fabricRuntime.activeCountForTest() != 0 || hh.fabricRuntime.inflightForTest() != 0 {
		t.Fatalf("runtime leak active=%d inflight=%d", hh.fabricRuntime.activeCountForTest(), hh.fabricRuntime.inflightForTest())
	}
}

func TestFabricHandoffLive_ChildOwnedCancelReturnCommitAppendFailureConverges(t *testing.T) {
	childEntered := make(chan struct{})
	childGate := make(chan struct{}) // released only via ctx cancel from cancelCurrent
	p := &fabricChildGateSignalProvider{entered: childEntered, gate: childGate}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4})
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	t.Cleanup(func() { _ = hh.Close() })
	handler := http.Handler(hh)

	var commits atomic.Int32
	var returnCommitFailed atomic.Bool
	fabric.SetAppendLockedHookForTest(func(taskID, eventType string) error {
		if eventType != fabric.EventHandoffCommitted {
			return nil
		}
		n := commits.Add(1)
		if n < 2 {
			return nil // parent→child commit ok
		}
		// Hook runs after return CAS inside terminalChildHandoff and before append;
		// must not call Repo methods (repo.mu is already held).
		returnCommitFailed.Store(true)
		return errors.New("injected cancel-return HandoffCommitted append failure")
	})
	t.Cleanup(func() { fabric.SetAppendLockedHookForTest(nil) })

	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		RunID string `json:"runId"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)

	select {
	case <-childEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting child-owned gate")
	}

	cr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", `{}`)
	if cr.Code != http.StatusOK {
		t.Fatalf("unconditioned cancel HTTP want 200 got %d %s", cr.Code, cr.Body.String())
	}

	// If cancel returned 200, durable run must already have converged.
	body := waitFabricRunState(t, handler, id, "interrupted")
	if !returnCommitFailed.Load() {
		t.Fatal("expected injected failure on return HandoffCommitted after cancel CAS")
	}
	if commits.Load() < 2 {
		t.Fatalf("expected return commit attempt; commits=%d", commits.Load())
	}
	if hh.fabricRuntime.hasActive(id, accepted.RunID) {
		t.Fatal("live entry must be gone after in-process convergence")
	}
	if p.opens.Load() != 2 {
		t.Fatalf("opens=%d want 2 (primary+child, no continuation)", p.opens.Load())
	}
	if p.continuation.Load() != 0 {
		t.Fatalf("continuation/provider replay must not run; continuation=%d", p.continuation.Load())
	}
	if strings.Contains(body, `"runState":"running"`) {
		t.Fatalf("run must not remain active: %s", body)
	}

	repo, err := hh.fabricRepo()
	if err != nil {
		t.Fatal(err)
	}
	assertCancelReturnHalfCommitEvidence(t, repo, id, "worker")
	assertNoOpenHandoffProposal(t, repo, id)
	owner, fence, err := repo.LeaseSnapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if owner != "" {
		t.Fatalf("lease owner must be cleared; owner=%q fence=%d", owner, fence)
	}
	if fence < 2 {
		t.Fatalf("released lease must retain bumped fencing; fence=%d", fence)
	}
	n, rerr := repo.RecoverOrphans()
	if rerr != nil {
		t.Fatalf("RecoverOrphans err=%v", rerr)
	}
	if n != 0 {
		t.Fatalf("RecoverOrphans must not be required; n=%d", n)
	}
	if got := budget.Metrics().ActiveTurns; got != 0 {
		t.Fatalf("ActiveTurns=%d want 0", got)
	}
	if hh.fabricRuntime.activeCountForTest() != 0 || hh.fabricRuntime.inflightForTest() != 0 {
		t.Fatalf("runtime leak active=%d inflight=%d", hh.fabricRuntime.activeCountForTest(), hh.fabricRuntime.inflightForTest())
	}
}

func TestFabricHandoffLive_ChildOwnedCancelDualStorageFailureDoesNotReportSuccess(t *testing.T) {
	// Dual storage failure: return CAS N+1→N+2 ok; HandoffCommitted append fails;
	// reconcile also cannot persist (HandoffRolledBack or RunInterrupted inject).
	// Unconditioned cancel must NOT HTTP 200 {"success":true} while durable remains open.
	childEntered := make(chan struct{})
	childGate := make(chan struct{})
	p := &fabricChildGateSignalProvider{entered: childEntered, gate: childGate}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4})
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	t.Cleanup(func() { _ = hh.Close() })
	handler := http.Handler(hh)

	var commits atomic.Int32
	var returnCommitFailed atomic.Bool
	var reconcileFailed atomic.Bool
	fabric.SetAppendLockedHookForTest(func(taskID, eventType string) error {
		switch eventType {
		case fabric.EventHandoffCommitted:
			n := commits.Add(1)
			if n < 2 {
				return nil
			}
			returnCommitFailed.Store(true)
			return errors.New("injected cancel-return HandoffCommitted append failure")
		case fabric.EventHandoffRolledBack, fabric.EventRunInterrupted:
			reconcileFailed.Store(true)
			return errors.New("injected reconcile append failure")
		default:
			return nil
		}
	})
	t.Cleanup(func() { fabric.SetAppendLockedHookForTest(nil) })

	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		RunID string `json:"runId"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)

	select {
	case <-childEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting child-owned gate")
	}

	cr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", `{}`)
	if cr.Code == http.StatusOK && strings.Contains(cr.Body.String(), `"success":true`) {
		t.Fatalf("dual storage failure must not report cancel success; got %d %s", cr.Code, cr.Body.String())
	}
	if cr.Code == http.StatusConflict {
		t.Fatalf("dual storage failure must not map to fencing 409; got %d %s", cr.Code, cr.Body.String())
	}
	if cr.Code < 500 {
		t.Fatalf("prefer existing 5xx persistence semantics; got %d %s", cr.Code, cr.Body.String())
	}
	if !returnCommitFailed.Load() {
		t.Fatal("expected injected failure on return HandoffCommitted after cancel CAS")
	}
	if !reconcileFailed.Load() {
		t.Fatal("expected injected failure during reconcile (HandoffRolledBack or RunInterrupted)")
	}
	if commits.Load() < 2 {
		t.Fatalf("expected return commit attempt; commits=%d", commits.Load())
	}

	repo, err := hh.fabricRepo()
	if err != nil {
		t.Fatal(err)
	}
	assertCancelReturnHalfCommitEvidence(t, repo, id, "worker")
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if isFabricTerminalRunState(detail.Summary.RunState) {
		t.Fatalf("durable run must not be terminal after dual storage failure; runState=%q", detail.Summary.RunState)
	}
	openID, _ := fabricOpenHandoffProposalForTest(repo, id)
	if openID == "" {
		t.Fatal("expected open return proposal or unresolved handoff after dual failure")
	}

	fabric.SetAppendLockedHookForTest(nil)
	n, rerr := repo.RecoverOrphans()
	if rerr != nil {
		t.Fatalf("RecoverOrphans err=%v", rerr)
	}
	if n < 1 {
		t.Fatalf("RecoverOrphans must converge after inject cleared; n=%d", n)
	}
	assertNoOpenHandoffProposal(t, repo, id)
	body := waitFabricRunState(t, handler, id, "interrupted")
	if strings.Contains(body, `"runState":"running"`) {
		t.Fatalf("run must converge after RecoverOrphans: %s", body)
	}
	if p.continuation.Load() != 0 {
		t.Fatalf("continuation/provider replay must not run; continuation=%d", p.continuation.Load())
	}
	if got := budget.Metrics().ActiveTurns; got != 0 {
		t.Fatalf("ActiveTurns=%d want 0", got)
	}
}

func TestFabricHandoffLive_ChildOwnedCancelExpectedDualStorageFailureDoesNotReportSuccess(t *testing.T) {
	// Explicit-precondition regression: cancelExpected shares cancelUnderAuthority.
	childEntered := make(chan struct{})
	childGate := make(chan struct{})
	p := &fabricChildGateSignalProvider{entered: childEntered, gate: childGate}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4}),
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	t.Cleanup(func() { _ = hh.Close() })
	handler := http.Handler(hh)

	var commits atomic.Int32
	fabric.SetAppendLockedHookForTest(func(taskID, eventType string) error {
		switch eventType {
		case fabric.EventHandoffCommitted:
			if commits.Add(1) < 2 {
				return nil
			}
			return errors.New("injected return HandoffCommitted failure")
		case fabric.EventHandoffRolledBack, fabric.EventRunInterrupted:
			return errors.New("injected reconcile failure")
		default:
			return nil
		}
	})
	t.Cleanup(func() { fabric.SetAppendLockedHookForTest(nil) })

	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}

	select {
	case <-childEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting child-owned gate")
	}

	repo, err := hh.fabricRepo()
	if err != nil {
		t.Fatal(err)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	owner := detail.Projection.CurrentOwner
	fence := detail.Projection.FencingToken
	if owner == "" || fence <= 0 {
		t.Fatalf("expected child-owned live claim; owner=%q fence=%d", owner, fence)
	}
	body := `{"expectedOwner":"` + owner + `","expectedFencingToken":` + strconv.Itoa(fence) + `}`
	cr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", body)
	if cr.Code == http.StatusOK && strings.Contains(cr.Body.String(), `"success":true`) {
		t.Fatalf("cancelExpected dual failure must not report success; got %d %s", cr.Code, cr.Body.String())
	}
	if cr.Code == http.StatusConflict {
		t.Fatalf("cancelExpected dual failure must not be fencing 409; got %d %s", cr.Code, cr.Body.String())
	}
	if cr.Code < 500 {
		t.Fatalf("prefer 5xx; got %d %s", cr.Code, cr.Body.String())
	}
	_ = p
}

func TestFabricHandoffLive_ChildOwnedShutdownCloseReturnCommitAppendFailureConverges(t *testing.T) {
	childEntered := make(chan struct{})
	childGate := make(chan struct{})
	p := &fabricChildGateSignalProvider{entered: childEntered, gate: childGate}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4})
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	handler := http.Handler(hh)

	var commits atomic.Int32
	var returnCommitFailed atomic.Bool
	fabric.SetAppendLockedHookForTest(func(taskID, eventType string) error {
		if eventType != fabric.EventHandoffCommitted {
			return nil
		}
		n := commits.Add(1)
		if n < 2 {
			return nil
		}
		// Hook runs after return CAS inside terminalChildHandoff and before append;
		// must not call Repo methods (repo.mu is already held).
		returnCommitFailed.Store(true)
		return errors.New("injected interrupt-return HandoffCommitted append failure")
	})
	t.Cleanup(func() { fabric.SetAppendLockedHookForTest(nil) })

	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		RunID string `json:"runId"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)

	select {
	case <-childEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting child-owned gate before Close")
	}

	// Actual runtime/handler shutdown — not selectTerminalForTest alone.
	if err := hh.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !returnCommitFailed.Load() {
		t.Fatal("expected injected failure on return HandoffCommitted after interrupt CAS")
	}
	if commits.Load() < 2 {
		t.Fatalf("expected return commit attempt; commits=%d", commits.Load())
	}
	if hh.fabricRuntime.hasActive(id, accepted.RunID) {
		t.Fatal("live entry must be gone after Close convergence")
	}
	if p.opens.Load() != 2 {
		t.Fatalf("opens=%d want 2 (primary+child, no continuation)", p.opens.Load())
	}
	if p.continuation.Load() != 0 {
		t.Fatalf("continuation/provider replay must not run; continuation=%d", p.continuation.Load())
	}

	repo, err := hh.fabricRepo()
	if err != nil {
		t.Fatal(err)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Summary.RunState != "interrupted" {
		t.Fatalf("runState=%q want interrupted after Close half-commit reconcile", detail.Summary.RunState)
	}
	assertCancelReturnHalfCommitEvidence(t, repo, id, "worker")
	assertNoOpenHandoffProposal(t, repo, id)
	owner, fence, err := repo.LeaseSnapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if owner != "" {
		t.Fatalf("lease owner must be cleared; owner=%q fence=%d", owner, fence)
	}
	if fence < 2 {
		t.Fatalf("released lease must retain bumped fencing; fence=%d", fence)
	}
	n, rerr := repo.RecoverOrphans()
	if rerr != nil {
		t.Fatalf("RecoverOrphans err=%v", rerr)
	}
	if n != 0 {
		t.Fatalf("RecoverOrphans must not be required; n=%d", n)
	}
	if got := budget.Metrics().ActiveTurns; got != 0 {
		t.Fatalf("ActiveTurns=%d want 0", got)
	}
	if hh.fabricRuntime.activeCountForTest() != 0 || hh.fabricRuntime.inflightForTest() != 0 {
		t.Fatalf("runtime leak active=%d inflight=%d", hh.fabricRuntime.activeCountForTest(), hh.fabricRuntime.inflightForTest())
	}
}

// fabricChildGateSignalProvider delegates once, then blocks the child Open on gate
// (or ctx cancel) and signals entered without sleeps/polls.
type fabricChildGateSignalProvider struct {
	opens        atomic.Int32
	continuation atomic.Int32
	entered      chan struct{}
	gate         chan struct{}
	enterOnce    sync.Once
}

func (p *fabricChildGateSignalProvider) Open(ctx context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
	n := int(p.opens.Add(1))
	if n == 1 {
		args := `{"instruction":"x"}`
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventToolCallStart, ID: "call_delegate", Name: fabric.FabricDelegateToolName},
			{Type: protocol.EventToolCallDelta, Arguments: args},
			{Type: protocol.EventToolCallEnd, ID: "call_delegate"},
			{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}, ProviderState: map[string]json.RawMessage{"openai_responses_previous": json.RawMessage(`{"id":"resp_native_primary_1"}`)}},
		}}, nil
	}
	if n >= 3 {
		p.continuation.Add(1)
	}
	p.enterOnce.Do(func() {
		if p.entered != nil {
			close(p.entered)
		}
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.gate:
	}
	return &sliceStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "child"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
	}}, nil
}

// assertCancelReturnHalfCommitEvidence proves the child→parent return CAS N+1→N+2
// occurred (lease moved / proposed_fence retained in history) and that the injected
// failure was specifically the return HandoffCommitted after that CAS. After successful
// in-process reconcile the proposal is rolled back and the lease released, but event
// history still shows ChildRunCancelled|Interrupted + return HandoffProposed and a
// fencing token that advanced past the child N+1 generation.

func (p *fabricChildGateSignalProvider) Protocol() string { return "openai-responses" }

func isFabricTerminalRunState(state string) bool {
	switch strings.TrimSpace(state) {
	case "completed", "failed", "cancelled", "canceled", "interrupted":
		return true
	default:
		return false
	}
}

func fabricOpenHandoffProposalForTest(repo *fabric.Repo, taskID string) (string, string) {
	detail, err := repo.Get(taskID)
	if err != nil {
		return "", ""
	}
	return fabricOpenHandoffFromEvents(detail.Events)
}

func fabricOpenHandoffFromEvents(events []fabric.Event) (string, string) {
	openID, stage := "", ""
	for _, ev := range events {
		pl := map[string]any{}
		_ = json.Unmarshal(ev.Payload, &pl)
		hid, _ := pl["handoff_id"].(string)
		if hid == "" {
			hid, _ = pl["handoffId"].(string)
		}
		switch ev.EventType {
		case fabric.EventHandoffProposed:
			openID = hid
			stage, _ = pl["stage"].(string)
			if stage == "" {
				dir, _ := pl["direction"].(string)
				stage = dir
			}
		case fabric.EventHandoffCommitted, fabric.EventHandoffFailed, fabric.EventHandoffRolledBack:
			if hid == openID || openID != "" {
				openID, stage = "", ""
			}
		}
	}
	return openID, stage
}

func assertCancelReturnHalfCommitEvidence(t *testing.T, repo *fabric.Repo, taskID, primaryOwner string) {
	t.Helper()
	detail, err := repo.Get(taskID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var sawChildTerminal bool
	var sawReturnPropose bool
	var returnProposeFence int
	var returnCommittedAfterPropose bool
	for _, ev := range detail.Events {
		pl := map[string]any{}
		_ = json.Unmarshal(ev.Payload, &pl)
		switch ev.EventType {
		case fabric.EventChildRunCancelled, fabric.EventChildRunInterrupted:
			sawChildTerminal = true
		case fabric.EventHandoffProposed:
			dir, _ := pl["direction"].(string)
			toOwner, _ := pl["to_owner"].(string)
			if toOwner == "" {
				toOwner, _ = pl["toOwner"].(string)
			}
			pf := 0
			switch v := pl["proposed_fence"].(type) {
			case float64:
				pf = int(v)
			case int:
				pf = v
			}
			if dir == fabric.HandoffDirectionChildToParent && toOwner == primaryOwner {
				sawReturnPropose = true
				returnProposeFence = pf
				returnCommittedAfterPropose = false
			}
		case fabric.EventHandoffCommitted:
			dir, _ := pl["direction"].(string)
			if dir == fabric.HandoffDirectionChildToParent {
				returnCommittedAfterPropose = true
			}
		}
	}
	if !sawChildTerminal {
		t.Fatal("expected child terminal before return half-commit")
	}
	if !sawReturnPropose {
		t.Fatal("expected child→parent HandoffProposed (return CAS path)")
	}
	if returnProposeFence < 2 {
		t.Fatalf("return proposed_fence must be N+2 generation; got %d", returnProposeFence)
	}
	if returnCommittedAfterPropose {
		t.Fatal("injected failure must prevent return HandoffCommitted")
	}
	_, fence, err := repo.LeaseSnapshot(taskID)
	if err != nil {
		t.Fatalf("LeaseSnapshot: %v", err)
	}
	if fence < returnProposeFence {
		t.Fatalf("lease fence %d must retain return CAS bump to at least proposed_fence %d", fence, returnProposeFence)
	}
}

func TestFabricContract_ChildReturnCallSitesPublishOrReconcile(t *testing.T) {
	// Focused source contract: every Cancel/Interrupt/Complete/FailChildHandoff call
	// site in fabric_delegate.go must either publish the returned N+2 claim on success
	// or reconcile uncertain return (never ignore herr/err and persist with a stale claim).
	src, err := os.ReadFile("fabric_delegate.go")
	if err != nil {
		t.Fatalf("read fabric_delegate.go: %v", err)
	}
	body := string(src)
	for _, sym := range []string{"CancelChildHandoff", "InterruptChildHandoff", "CompleteChildHandoff", "FailChildHandoff"} {
		if !strings.Contains(body, "repo."+sym) {
			t.Fatalf("expected production call site for %s", sym)
		}
	}
	// Unsafe legacy pattern: ignore herr then persist with possibly-stale claim.
	unsafe := []string{
		"if returned, herr := repo.InterruptChildHandoff",
		"if returned, herr := repo.CancelChildHandoff",
	}
	for _, pat := range unsafe {
		if strings.Contains(body, pat) {
			t.Fatalf("unsafe ignored-herr child-return pattern still present: %s", pat)
		}
	}
	if !strings.Contains(body, "func (rt *fabricRuntime) applyChildReturnTerminal") {
		t.Fatal("expected shared applyChildReturnTerminal helper")
	}
	if !strings.Contains(body, "reconcileHandoffStorageFailure(repo, live, child.RunID") {
		t.Fatal("expected child-return paths to reconcile uncertain return storage failures")
	}
	// Complete/Fail retain always-reconcile on err (publish on success is replaceClaim(returned)).
	if !strings.Contains(body, "live.replaceClaim(returned)") {
		t.Fatal("expected successful child-return to publish returned N+2 claim")
	}
}
