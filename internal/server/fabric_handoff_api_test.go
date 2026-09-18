package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/sidecar/fabric"
)

type fabricHandoffProvider struct {
	opens atomic.Int32
	seen  atomic.Int32
	// scripted opens: 0=primary tool call, 1=child text, 2=continuation text
}

func (p *fabricHandoffProvider) Open(ctx context.Context, req providercontract.DispatchRequest) (EventStream, error) {
	n := int(p.opens.Add(1))
	// Detect forbidden prompt-paste continuation.
	for _, msg := range req.Parsed.Context.Messages {
		if msg.Role == protocol.RoleUser {
			for _, c := range msg.Content {
				if strings.Contains(c.Text, "Subagent result:") {
					return nil, &providercontract.OpenError{StatusCode: 400, Message: "prompt paste forbidden"}
				}
			}
		}
		if msg.Role == protocol.RoleToolResult {
			p.seen.Add(1)
		}
	}
	// Cycle so sequential tasks each see delegate/child/final (n%3: 1,2,0).
	switch n % 3 {
	case 1:
		args := `{"instruction":"CHILD_INSTRUCTION_SECRET"}`
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventToolCallStart, ID: "call_delegate", Name: fabric.FabricDelegateToolName},
			{Type: protocol.EventToolCallDelta, Arguments: args},
			{Type: protocol.EventToolCallEnd, ID: "call_delegate"},
			{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}, ProviderState: map[string]json.RawMessage{"openai_responses_previous": json.RawMessage(`{"id":"resp_native_primary_1"}`)}},
		}}, nil
	case 2:
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "child-output"},
			{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
		}}, nil
	default: // 0
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "final"},
			{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
		}}, nil
	}
}

func (p *fabricHandoffProvider) Protocol() string { return "openai-responses" }

func TestFabricHandoff26_StatusAdvertisesCapability(t *testing.T) {
	h, _ := newFabricExecHandler(t, &fabricHandoffProvider{})
	rr := fabricDo(h, http.MethodGet, "/api/fabric/status", "")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "single_child_handoff") {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

func TestFabricHandoff27_HappyPathThreeTurnsOneRun(t *testing.T) {
	p := &fabricHandoffProvider{}
	h, home := newFabricExecHandler(t, p)
	_ = home
	id := fabricCreate(t, h)
	body := `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do work","delegation":{"model":"openai-apikey/gpt-child"}}`
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", body)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		RunID string `json:"runId"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)
	waitFabricRunState(t, h, id, "completed")
	if p.opens.Load() != 3 {
		t.Fatalf("opens=%d want 3", p.opens.Load())
	}
	if p.seen.Load() < 1 {
		t.Fatal("expected structured toolResult continuation")
	}
	detail := fabricDo(h, http.MethodGet, "/api/fabric/tasks/"+id, "")
	if strings.Contains(detail.Body.String(), "CHILD_INSTRUCTION_SECRET") || strings.Contains(detail.Body.String(), "Subagent result:") {
		t.Fatalf("privacy/prompt-paste leak: %s", detail.Body.String())
	}
	if !strings.Contains(detail.Body.String(), "ChildRunStarted") || !strings.Contains(detail.Body.String(), "ChildRunCompleted") {
		t.Fatalf("missing child events: %s", detail.Body.String())
	}
	// Exactly one primary runId in execute result path
	if strings.Count(detail.Body.String(), accepted.RunID) < 1 {
		t.Fatal("missing run id")
	}
}

func TestFabricHandoff28_ChildCapacityBeforeHandoff(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
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
	handler := attachHandlerClose(t, h)
	id := fabricCreate(t, handler)
	body := `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", body)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	// Parent turn is released before child TryAcquire; MaxActiveTurns=1 serial chain succeeds.
	waitFabricRunState(t, handler, id, "completed")
	if p.opens.Load() != 3 {
		t.Fatalf("opens=%d want 3 (parent/child/resume)", p.opens.Load())
	}
	if got := budget.Metrics().ActiveTurns; got != 0 {
		t.Fatalf("ActiveTurns=%d want 0", got)
	}
}

func TestFabricHandoff29_CancelDuringChild(t *testing.T) {
	gate := make(chan struct{})
	p := &fabricGateHandoffProvider{childGate: gate}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)
	body := `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", body)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		RunID        string `json:"runId"`
		Owner        string `json:"owner"`
		FencingToken int    `json:"fencingToken"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)
	// Wait until child open started.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && p.opens.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	cancelBody := `{}`
	cr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", cancelBody)
	if cr.Code != http.StatusOK {
		t.Fatalf("cancel %d %s", cr.Code, cr.Body.String())
	}
	close(gate)
	waitFabricRunState(t, h, id, "cancelled")
}

func TestFabricHandoff30_NoDelegationSkipsTool(t *testing.T) {
	p := &fabricExecProvider{}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-5","input":"hello"}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	waitFabricRunState(t, h, id, "completed")
	if p.opens.Load() != 1 {
		t.Fatalf("opens=%d", p.opens.Load())
	}
}

func TestFabricHandoff31_ExactlyOneChild(t *testing.T) {
	repo := fabric.New(t.TempDir())
	id, err := repo.Create(fabric.CreateTaskInput{Title: "t", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, fabric.ExecuteInput{Owner: "w", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.BeginChildHandoff(primary, "p/c")
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.BeginChildHandoff(fabric.RunIdentity{TaskID: id, RunID: primary.RunID, Owner: child.Owner, Fence: child.Fence}, "p/c2")
	if fabric.CodeOf(err) != fabric.CodeInvalidTransition {
		t.Fatalf("err=%v", err)
	}
}

type fabricGateHandoffProvider struct {
	opens     atomic.Int32
	childGate chan struct{}
}

func (p *fabricGateHandoffProvider) Open(ctx context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
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
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.childGate:
	}
	return &sliceStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "child"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
	}}, nil
}

func (p *fabricGateHandoffProvider) Protocol() string { return "openai-responses" }

func TestFabricHandoff32_ZeroActiveTurnsAfterSuccess(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 2})
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
	if got := budget.Metrics().ActiveTurns; got != 0 {
		t.Fatalf("ActiveTurns=%d", got)
	}
}

func TestFabricHandoff33_SequentialDelegationsNoSlotLeak(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 1})
	p := &fabricHandoffProvider{}
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
	for i := 0; i < 3; i++ {
		id := fabricCreate(t, handler)
		rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
		if rr.Code != http.StatusAccepted {
			t.Fatalf("iter %d: %d %s", i, rr.Code, rr.Body.String())
		}
		waitFabricRunState(t, handler, id, "completed")
		if got := budget.Metrics().ActiveTurns; got != 0 {
			t.Fatalf("iter %d ActiveTurns=%d", i, got)
		}
		detail := fabricDo(handler, http.MethodGet, "/api/fabric/tasks/"+id, "")
		if !strings.Contains(detail.Body.String(), "ChildRunStarted") {
			t.Fatalf("iter %d did not genuinely delegate: %s", i, detail.Body.String())
		}
	}
	if p.opens.Load() != 9 {
		t.Fatalf("opens=%d want 9 (3 tasks x 3 turns)", p.opens.Load())
	}
}

func TestFabricHandoff34_ExactlyOneChildAfterTerminal(t *testing.T) {
	repo := fabric.New(t.TempDir())
	id, err := repo.Create(fabric.CreateTaskInput{Title: "t", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	primary, err := repo.BeginExecute(id, fabric.ExecuteInput{Owner: "w", Model: "p/m"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.BeginChildHandoff(primary, "p/c")
	if err != nil {
		t.Fatal(err)
	}
	returned, err := repo.CompleteChildHandoff(child, "w")
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.BeginChildHandoff(returned, "p/c2")
	if fabric.CodeOf(err) != fabric.CodeInvalidTransition {
		t.Fatalf("err=%v", err)
	}
}

func TestFabricHandoff35_StaleParentCancelDuringChild(t *testing.T) {
	gate := make(chan struct{})
	p := &fabricGateHandoffProvider{childGate: gate}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)
	body := `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", body)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		RunID        string `json:"runId"`
		Owner        string `json:"owner"`
		FencingToken int    `json:"fencingToken"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && p.opens.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
	cancelBody := `{"expectedOwner":"` + accepted.Owner + `","expectedFencingToken":` + strconv.Itoa(accepted.FencingToken) + `}`
	cr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", cancelBody)
	if cr.Code != http.StatusConflict {
		t.Fatalf("stale parent cancel want 409 got %d %s", cr.Code, cr.Body.String())
	}
	close(gate)
	waitFabricRunState(t, h, id, "completed")
}

func TestFabricHandoff36_ChildTruncationStructuredToolResult(t *testing.T) {
	out, isErr := childToolResultPayload(modelTurnResult{OutputText: "partial", Truncated: true, Status: "completed"}, false, "")
	if isErr {
		t.Fatal("truncation alone is not error")
	}
	if !strings.Contains(out, `"truncated":true`) || !strings.Contains(out, `"output"`) {
		t.Fatalf("structured truncation missing: %s", out)
	}
	out2, isErr2 := childToolResultPayload(modelTurnResult{OutputText: "", Truncated: true, Status: "failed"}, true, "boom")
	if !isErr2 || !strings.Contains(out2, `"error"`) || !strings.Contains(out2, `"truncated":true`) {
		t.Fatalf("failed truncated: %s isErr=%v", out2, isErr2)
	}
}

func TestFabricHandoff37_ThreeDistinctRequestIDs(t *testing.T) {
	p := &fabricHandoffProvider{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": p},
		ConfigPath:     configPath,
		ResourceBudget: resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4}),
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := attachHandlerClose(t, h)
	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"PARENT_INPUT_SECRET","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		RunID        string `json:"runId"`
		ResultHandle string `json:"resultHandle"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)
	waitFabricRunState(t, handler, id, "completed")

	listed := getDiagnosticsList(t, handler, "/api/diagnostics/requests")
	byProto := map[string]string{}
	for _, row := range listed.Requests {
		if !strings.HasPrefix(row.RequestID, "req_") {
			continue
		}
		detail := getDiagnosticsDetail(t, handler, row.RequestID)
		if detail.Protocol != "" {
			byProto[detail.Protocol] = row.RequestID
		}
	}
	parent1 := byProto["fabric-execute"]
	childReq := byProto["fabric-execute-child"]
	parent2 := byProto["fabric-execute-continue"]
	if parent1 == "" || childReq == "" || parent2 == "" {
		t.Fatalf("want distinct parent1/child/parent2 via diagnostics protocol; got %#v from %s", byProto, mustJSON(listed.Requests))
	}
	if parent1 == childReq || childReq == parent2 || parent1 == parent2 {
		t.Fatalf("req_* not distinct: %q %q %q", parent1, childReq, parent2)
	}

	usagePath := filepath.Join(home, "usage", "active.jsonl")
	usageRaw, err := os.ReadFile(usagePath)
	if err != nil {
		usageRaw, err = os.ReadFile(filepath.Join(home, "usage.jsonl"))
		if err != nil {
			t.Fatalf("usage ledger missing: %v", err)
		}
	}
	usageText := string(usageRaw)
	for _, rid := range []string{parent1, childReq, parent2} {
		if !strings.Contains(usageText, rid) {
			t.Fatalf("usage ledger missing %s: %s", rid, usageText)
		}
	}
	// Attribution: child model only on child req line; primary model on parent turns.
	for _, line := range strings.Split(strings.TrimSpace(usageText), "\n") {
		if strings.Contains(line, childReq) && !strings.Contains(line, "gpt-child") {
			t.Fatalf("child usage attribution missing gpt-child: %s", line)
		}
		if strings.Contains(line, parent1) && !strings.Contains(line, "gpt-primary") {
			t.Fatalf("parent1 usage attribution missing gpt-primary: %s", line)
		}
		if strings.Contains(line, parent2) && !strings.Contains(line, "gpt-primary") {
			t.Fatalf("parent2 usage attribution missing gpt-primary: %s", line)
		}
	}

	// Request-history route-decision must resolve each req_* with model attribution.
	for _, tc := range []struct {
		id    string
		model string
	}{
		{parent1, "gpt-primary"},
		{childReq, "gpt-child"},
		{parent2, "gpt-primary"},
	} {
		hist := fabricDo(handler, http.MethodGet, "/api/request-history/"+tc.id+"/route-decision", "")
		if hist.Code != http.StatusOK {
			t.Fatalf("request-history %s: %d %s", tc.id, hist.Code, hist.Body.String())
		}
		if !strings.Contains(hist.Body.String(), tc.model) {
			t.Fatalf("request-history %s missing model %s: %s", tc.id, tc.model, hist.Body.String())
		}
	}

	res := fabricDo(handler, http.MethodGet, "/api/fabric/tasks/"+id+"/runs/"+accepted.RunID+"/result", "")
	if res.Code != http.StatusOK {
		t.Fatalf("result %d %s", res.Code, res.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &body)
	finalReq, _ := body["requestId"].(string)
	if finalReq != parent2 {
		t.Fatalf("final result.requestId=%q want parent2=%q", finalReq, parent2)
	}
	detail := fabricDo(handler, http.MethodGet, "/api/fabric/tasks/"+id, "")
	raw := detail.Body.String()
	for _, leak := range []string{"PARENT_INPUT_SECRET", "CHILD_INSTRUCTION_SECRET", "child-output", "Subagent result:"} {
		if strings.Contains(raw, leak) {
			t.Fatalf("privacy leak %q in detail", leak)
		}
	}
}

func TestFabricHandoff38_RaceCancelAndHandoff(t *testing.T) {
	gate := make(chan struct{})
	p := &fabricGateHandoffProvider{childGate: gate}
	h, _ := newFabricExecHandler(t, p)
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var accepted struct {
		Owner        string `json:"owner"`
		FencingToken int    `json:"fencingToken"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &accepted)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && p.opens.Load() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			body := `{"expectedOwner":"` + accepted.Owner + `","expectedFencingToken":` + strconv.Itoa(accepted.FencingToken) + `}`
			cr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/cancel", body)
			if cr.Code != http.StatusConflict {
				t.Errorf("stale parent cancel got %d want 409", cr.Code)
			}
		}
	}()
	close(gate)
	waitFabricRunState(t, h, id, "completed")
	<-done
}

type fabricMultiDelegateProvider struct {
	opens        atomic.Int32
	continuation atomic.Int32
}

func (p *fabricMultiDelegateProvider) Open(ctx context.Context, req providercontract.DispatchRequest) (EventStream, error) {
	n := int(p.opens.Add(1))
	for _, msg := range req.Parsed.Context.Messages {
		if msg.Role == protocol.RoleToolResult {
			p.continuation.Add(1)
		}
	}
	if n == 1 {
		args := `{"instruction":"CHILD_A"}`
		args2 := `{"instruction":"CHILD_B"}`
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventToolCallStart, ID: "call_delegate_a", Name: fabric.FabricDelegateToolName},
			{Type: protocol.EventToolCallDelta, Arguments: args},
			{Type: protocol.EventToolCallEnd, ID: "call_delegate_a"},
			{Type: protocol.EventToolCallStart, ID: "call_delegate_b", Name: fabric.FabricDelegateToolName},
			{Type: protocol.EventToolCallDelta, Arguments: args2},
			{Type: protocol.EventToolCallEnd, ID: "call_delegate_b"},
			{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
		}}, nil
	}
	return &sliceStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "should-not-run"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
	}}, nil
}

func TestFabricHandoffLive_TwoDelegateCallsFailClosedNoChild(t *testing.T) {
	p := &fabricMultiDelegateProvider{}
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

	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"openai-apikey/gpt-primary","input":"do","delegation":{"model":"openai-apikey/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	body := waitFabricRunState(t, handler, id, "failed")
	if !strings.Contains(body, `"runState":"failed"`) {
		t.Fatalf("want durable primary failed: %s", body)
	}
	if p.opens.Load() != 1 {
		t.Fatalf("opens=%d want 1 (primary only; no child/continuation)", p.opens.Load())
	}
	if p.continuation.Load() != 0 {
		t.Fatalf("continuation=%d want 0", p.continuation.Load())
	}
	repo, err := hh.fabricRepo()
	if err != nil {
		t.Fatal(err)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range detail.Events {
		if ev.EventType == fabric.EventChildRunStarted {
			t.Fatal("ChildRunStarted must not be emitted for ambiguous multi-delegate primary")
		}
		pl := string(ev.Payload)
		if strings.Contains(pl, "CHILD_A") || strings.Contains(pl, "CHILD_B") {
			t.Fatalf("durable event leaked prompt/tool args: %s %s", ev.EventType, pl)
		}
	}
	assertNoOpenHandoffProposal(t, repo, id)
	if got := budget.Metrics().ActiveTurns; got != 0 {
		t.Fatalf("ActiveTurns=%d want 0", got)
	}
	if hh.fabricRuntime.activeCountForTest() != 0 || hh.fabricRuntime.inflightForTest() != 0 {
		t.Fatalf("runtime leak active=%d inflight=%d", hh.fabricRuntime.activeCountForTest(), hh.fabricRuntime.inflightForTest())
	}
}

func TestFabricPrimary_EnforcesExactlyOneDelegateFailClosed(t *testing.T) {
	src, err := os.ReadFile("fabric_delegate.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "ParallelToolCalls:") || !strings.Contains(body, "boolPtr(false)") {
		t.Fatal("expected ParallelToolCalls=false on Fabric primary ParsedRequest")
	}
	if !strings.Contains(body, "decideFabricPrimaryToolCalls") {
		t.Fatal("expected fail-closed decideFabricPrimaryToolCalls")
	}
	call, decision, reason := decideFabricPrimaryToolCalls([]modelTurnToolCall{
		{CallID: "a", Name: fabric.FabricDelegateToolName, Arguments: `{"instruction":"x"}`},
		{CallID: "b", Name: fabric.FabricDelegateToolName, Arguments: `{"instruction":"y"}`},
	})
	if decision != fabricPrimaryReject || reason == "" {
		t.Fatalf("two delegates: decision=%v reason=%q call=%v", decision, reason, call)
	}
	_, decision, _ = decideFabricPrimaryToolCalls(nil)
	if decision != fabricPrimaryNoDelegate {
		t.Fatalf("zero calls: decision=%v", decision)
	}
	_, decision, _ = decideFabricPrimaryToolCalls([]modelTurnToolCall{
		{CallID: "a", Name: fabric.FabricDelegateToolName, Arguments: `{"instruction":"x"}`},
	})
	if decision != fabricPrimaryOneDelegate {
		t.Fatalf("one delegate: decision=%v", decision)
	}
}

type fabricGoogleDelegateProvider struct {
	opens atomic.Int32
	sig   string
}

func (p *fabricGoogleDelegateProvider) Open(ctx context.Context, req providercontract.DispatchRequest) (EventStream, error) {
	n := int(p.opens.Add(1))
	switch n {
	case 1:
		meta, _ := json.Marshal(map[string]any{"google": map[string]string{"thoughtSignature": p.sig}})
		args := `{"instruction":"CHILD_INSTRUCTION"}`
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventToolCallStart, ID: "call_delegate", Name: fabric.FabricDelegateToolName},
			{Type: protocol.EventToolCallDelta, Arguments: args},
			{Type: protocol.EventToolCallEnd, ID: "call_delegate", ProviderMetadata: meta},
			{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
		}}, nil
	case 2:
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "child-output"},
			{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
		}}, nil
	default:
		// Continuation: require ThoughtSignature on the assistant tool-call part.
		for _, msg := range req.Parsed.Context.Messages {
			if msg.Role != protocol.RoleAssistant {
				continue
			}
			for _, part := range msg.Content {
				if part.Type != protocol.ContentToolCall {
					continue
				}
				sig := strings.TrimSpace(part.ThoughtSignature)
				if sig == "" && part.ProviderMetadata != nil && part.ProviderMetadata.Google != nil {
					sig = strings.TrimSpace(part.ProviderMetadata.Google.ThoughtSignature)
				}
				if sig != p.sig {
					return nil, &providercontract.OpenError{StatusCode: 400, Message: "missing thought signature on continuation"}
				}
			}
		}
		if strings.TrimSpace(req.Parsed.PreviousResponseID) == "" {
			// OpenAI-style binding optional for google path; still require messages order.
		}
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "final"},
			{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
		}}, nil
	}
}

func (p *fabricGoogleDelegateProvider) Protocol() string { return "google" }

func TestFabricContinuation_PreservesGoogleThoughtSignatureThroughModelTurn(t *testing.T) {
	p := &fabricGoogleDelegateProvider{sig: "real-thought-sig-abc"}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"google":{"adapter":"google-gemini"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4})
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google": p},
		ConfigPath:     configPath,
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	t.Cleanup(func() { _ = hh.Close() })
	handler := http.Handler(hh)

	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"google/gemini-primary","input":"do","delegation":{"model":"google/gemini-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	body := waitFabricRunState(t, handler, id, "completed")
	if !strings.Contains(body, `"runState":"completed"`) {
		t.Fatalf("want completed: %s", body)
	}
	if p.opens.Load() != 3 {
		t.Fatalf("opens=%d want 3 (primary+child+continuation)", p.opens.Load())
	}
	repo, err := hh.fabricRepo()
	if err != nil {
		t.Fatal(err)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range detail.Events {
		pl := string(ev.Payload)
		if strings.Contains(pl, "real-thought-sig-abc") {
			t.Fatalf("ThoughtSignature must not appear in Fabric durable events: %s", pl)
		}
	}
}

func TestFabricContinuation_GoogleMissingThoughtSignatureFailsBeforeHandoff(t *testing.T) {
	p := &fabricGoogleDelegateProvider{sig: ""} // empty → gating fails
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true},"providers":{"google":{"adapter":"google-gemini"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4})
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"google": p},
		ConfigPath:     configPath,
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	hh := raw.(*handler)
	t.Cleanup(func() { _ = hh.Close() })
	handler := http.Handler(hh)

	id := fabricCreate(t, handler)
	rr := fabricDo(handler, http.MethodPost, "/api/fabric/tasks/"+id+"/execute", `{"owner":"worker","model":"google/gemini-primary","input":"do","delegation":{"model":"google/gemini-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	waitFabricRunState(t, handler, id, "failed")
	if p.opens.Load() != 1 {
		t.Fatalf("opens=%d want 1 (fail before child)", p.opens.Load())
	}
	repo, err := hh.fabricRepo()
	if err != nil {
		t.Fatal(err)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range detail.Events {
		if ev.EventType == fabric.EventChildRunStarted {
			t.Fatal("ChildRunStarted must not emit when ThoughtSignature missing")
		}
	}
	if got := budget.Metrics().ActiveTurns; got != 0 {
		t.Fatalf("ActiveTurns=%d want 0", got)
	}
}

func TestFabricContinuation_OpenAINativePreviousOnlyToolResult(t *testing.T) {
	call := modelTurnToolCall{
		CallID:    "call_delegate",
		Name:      fabric.FabricDelegateToolName,
		Arguments: `{"instruction":"x"}`,
	}
	parsed := buildFabricContinuationParsed("m", "hello", call, "child-out", false, fabricContinuationPlan{
		Mode:             fabricContOpenAINativePrevious,
		NativePreviousID: "resp_native_abc",
	})
	if parsed.PreviousResponseID != "resp_native_abc" {
		t.Fatalf("PreviousResponseID=%q", parsed.PreviousResponseID)
	}
	if len(parsed.Context.Messages) != 1 || parsed.Context.Messages[0].Role != protocol.RoleToolResult {
		t.Fatalf("want only tool result, got %#v", parsed.Context.Messages)
	}
	if parsed.Options.ToolChoice == nil || parsed.Options.ToolChoice.Kind != protocol.ToolChoiceNone {
		t.Fatalf("tool choice=%#v", parsed.Options.ToolChoice)
	}
	if len(parsed.Context.Tools) != 0 {
		t.Fatalf("tools must stay empty: %#v", parsed.Context.Tools)
	}
}

func TestFabricContinuation_GoogleReplayKeepsThoughtSignature(t *testing.T) {
	call := modelTurnToolCall{
		CallID:           "call_delegate",
		Name:             fabric.FabricDelegateToolName,
		Arguments:        `{"instruction":"x"}`,
		ThoughtSignature: "sig-1",
		ProviderMetadata: &protocol.ProviderOpaqueMetadata{Google: &protocol.GoogleOpaqueMetadata{ThoughtSignature: "sig-1"}},
	}
	parsed := buildFabricContinuationParsed("m", "hello", call, "child-out", false, fabricContinuationPlan{Mode: fabricContGoogleReplay})
	if parsed.PreviousResponseID != "" {
		t.Fatalf("google replay must not set previous_response_id: %q", parsed.PreviousResponseID)
	}
	if len(parsed.Context.Messages) != 3 {
		t.Fatalf("messages=%d", len(parsed.Context.Messages))
	}
	part := parsed.Context.Messages[1].Content[0]
	if part.ThoughtSignature != "sig-1" {
		t.Fatalf("ThoughtSignature=%q", part.ThoughtSignature)
	}
}

func TestFabricContinuation_OpenAIReasoningWithoutNativePreviousFailsClosed(t *testing.T) {
	err := validateFabricContinuationCapability("openai-responses", modelTurnToolCall{CallID: "c", Name: fabric.FabricDelegateToolName}, map[string]any{
		"id":     "resp_bridge_1",
		"output": []any{map[string]any{"type": "reasoning"}, map[string]any{"type": "function_call"}},
	}, "")
	if err == nil {
		t.Fatal("expected fail closed without provider-native previous_response_id")
	}
	err = validateFabricContinuationCapability("openai-responses", modelTurnToolCall{CallID: "c", Name: fabric.FabricDelegateToolName}, map[string]any{
		"id":     "resp_bridge_1",
		"output": []any{map[string]any{"type": "reasoning"}, map[string]any{"type": "function_call"}},
	}, "resp_native_owned_1")
	if err != nil {
		t.Fatalf("native previous should allow: %v", err)
	}
}

func TestFabricContinuation_RouteNameSubstringDoesNotGate(t *testing.T) {
	err := validateFabricContinuationCapability("my-googleish-openai", modelTurnToolCall{CallID: "c", Name: fabric.FabricDelegateToolName}, nil, "")
	if err == nil {
		t.Fatal("expected fail closed for unknown protocol")
	}
	err = validateFabricContinuationCapability("google", modelTurnToolCall{CallID: "c", Name: fabric.FabricDelegateToolName, ThoughtSignature: "sig"}, nil, "")
	if err != nil {
		t.Fatalf("resolved google protocol should allow with signature: %v", err)
	}
}
