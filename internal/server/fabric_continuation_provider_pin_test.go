package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	providercontract "github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/sidecar/fabric"
)

// fabricPinMember is a distinguishable OpenAI Responses-compatible combo member.
// failBeforeCommit makes Open fail before any model-visible event (combo hops).
type fabricPinMember struct {
	id               string
	nativeRespID     string
	opens            atomic.Int32
	failBeforeCommit atomic.Bool
	contOpens        atomic.Int32
	lastPrevID       atomic.Value // string
	lastToolResults  atomic.Int32
	lastMsgCount     atomic.Int32
	sawUserReplay    atomic.Bool
}

func (p *fabricPinMember) Protocol() string { return "openai-responses" }

func (p *fabricPinMember) Open(_ context.Context, req providercontract.DispatchRequest) (EventStream, error) {
	_ = p.opens.Add(1)
	if p.failBeforeCommit.Load() {
		return nil, &providercontract.OpenError{StatusCode: 503, Message: "member " + p.id + " precommit unavailable"}
	}
	prev := strings.TrimSpace(req.Parsed.PreviousResponseID)
	p.lastPrevID.Store(prev)
	toolResults := int32(0)
	msgs := int32(0)
	sawUser := false
	for _, msg := range req.Parsed.Context.Messages {
		msgs++
		switch msg.Role {
		case protocol.RoleToolResult:
			toolResults++
		case protocol.RoleUser:
			sawUser = true
		}
	}
	p.lastMsgCount.Store(msgs)
	p.lastToolResults.Store(toolResults)
	if prev != "" || toolResults > 0 {
		if sawUser {
			p.sawUserReplay.Store(true)
		}
		p.contOpens.Add(1)
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "final-from-" + p.id},
			{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
		}}, nil
	}
	native := p.nativeRespID
	if native == "" {
		native = "resp_" + p.id
	}
	args := `{"instruction":"CHILD_INSTRUCTION_SECRET"}`
	return &sliceStream{events: []protocol.Event{
		{Type: protocol.EventToolCallStart, ID: "call_delegate", Name: fabric.FabricDelegateToolName},
		{Type: protocol.EventToolCallDelta, Arguments: args},
		{Type: protocol.EventToolCallEnd, ID: "call_delegate"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}, ProviderState: map[string]json.RawMessage{
			"openai_responses_previous": json.RawMessage(`{"id":"` + native + `"}`),
		}},
	}}, nil
}

type fabricPinChild struct {
	opens  atomic.Int32
	onOpen func()
}

func (p *fabricPinChild) Protocol() string { return "openai-responses" }

func (p *fabricPinChild) Open(_ context.Context, _ providercontract.DispatchRequest) (EventStream, error) {
	p.opens.Add(1)
	if p.onOpen != nil {
		p.onOpen()
	}
	return &sliceStream{events: []protocol.Event{
		{Type: protocol.EventTextDelta, Text: "child-output"},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
	}}, nil
}

func newFabricComboPinHandler(t *testing.T, a, b, child Provider, comboID string) http.Handler {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{
			"openai-a": a,
			"openai-b": b,
			"openai-c": child,
		},
		ConfigPath:     configPath,
		ResourceBudget: budget,
		Combos: []Combo{{
			ID: comboID,
			Targets: []ComboTarget{
				{ProviderID: "openai-a", Model: "gpt-a", Protocol: "openai-responses"},
				{ProviderID: "openai-b", Model: "gpt-b", Protocol: "openai-responses"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return attachHandlerClose(t, h)
}

// TestFabricContinuation_ComboPinsCommittedPhysicalProvider RED/GREEN: after combo
// failover A->B on primary, Fabric continuation must stay on physical B even when A
// becomes healthy again before continuation. Same openai-responses protocol on A and B
// is not sufficient ownership.
func TestFabricContinuation_ComboPinsCommittedPhysicalProvider(t *testing.T) {
	a := &fabricPinMember{id: "A", nativeRespID: "resp_A"}
	b := &fabricPinMember{id: "B", nativeRespID: "resp_B"}
	a.failBeforeCommit.Store(true)
	child := &fabricPinChild{onOpen: func() {
		// Heal A after primary committed on B and before continuation Open.
		a.failBeforeCommit.Store(false)
	}}

	h := newFabricComboPinHandler(t, a, b, child, "pin-ab")
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute",
		`{"owner":"worker","model":"combo/pin-ab","input":"do work","delegation":{"model":"openai-c/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("execute=%d %s", rr.Code, rr.Body.String())
	}
	body := waitFabricRunState(t, h, id, "completed")
	if !strings.Contains(body, `"runState":"completed"`) {
		t.Fatalf("want completed: %s", body)
	}

	if got := a.opens.Load(); got != 1 {
		t.Fatalf("A opens=%d want exactly 1 (primary precommit fail only); continuation reopened A", got)
	}
	if got := b.opens.Load(); got != 2 {
		t.Fatalf("B opens=%d want 2 (primary commit + continuation)", got)
	}
	if got := b.contOpens.Load(); got != 1 {
		t.Fatalf("B continuation opens=%d want 1", got)
	}
	if got := child.opens.Load(); got != 1 {
		t.Fatalf("child opens=%d want 1", got)
	}
	prev, _ := b.lastPrevID.Load().(string)
	if prev != "resp_B" {
		t.Fatalf("continuation previous_response_id=%q want resp_B", prev)
	}
	if b.lastToolResults.Load() != 1 {
		t.Fatalf("continuation tool results=%d want 1", b.lastToolResults.Load())
	}
	if b.lastMsgCount.Load() != 1 {
		t.Fatalf("continuation messages=%d want only tool-result (1)", b.lastMsgCount.Load())
	}
	if b.sawUserReplay.Load() {
		t.Fatal("continuation replayed user history; want tool-result only")
	}
	if a.contOpens.Load() != 0 {
		t.Fatalf("A must not receive continuation; contOpens=%d", a.contOpens.Load())
	}
}


func newFabricPolicyPinHandler(t *testing.T, a, b, child Provider, profileID string) http.Handler {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	cfg := `{"fabric":{"enabled":true},"routingProfiles":{"` + profileID + `":{"candidates":[{"provider":"openai-a","model":"gpt-a"},{"provider":"openai-b","model":"gpt-b"}]}}}`
	if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4})
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{
			"openai-a": a,
			"openai-b": b,
			"openai-c": child,
		},
		ConfigPath:     configPath,
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	return attachHandlerClose(t, h)
}

// TestFabricContinuation_PolicyPinsCommittedPhysicalProvider: policy sticky evidence
// differs between primary (FirstUserText) and continuation (tool-result + native previous),
// so sticky alone is not ownership. Continuation must stay on committed physical B.
func TestFabricContinuation_PolicyPinsCommittedPhysicalProvider(t *testing.T) {
	a := &fabricPinMember{id: "A", nativeRespID: "resp_A"}
	b := &fabricPinMember{id: "B", nativeRespID: "resp_B"}
	a.failBeforeCommit.Store(true)
	child := &fabricPinChild{onOpen: func() { a.failBeforeCommit.Store(false) }}

	h := newFabricPolicyPinHandler(t, a, b, child, "pin-policy")
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute",
		`{"owner":"worker","model":"policy/pin-policy","input":"do work","delegation":{"model":"openai-c/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("execute=%d %s", rr.Code, rr.Body.String())
	}
	body := waitFabricRunState(t, h, id, "completed")
	if !strings.Contains(body, `"runState":"completed"`) {
		t.Fatalf("want completed: %s", body)
	}
	if got := a.opens.Load(); got != 1 {
		t.Fatalf("A opens=%d want 1; policy continuation re-evaluated and hopped", got)
	}
	if got := b.opens.Load(); got != 2 {
		t.Fatalf("B opens=%d want 2", got)
	}
	prev, _ := b.lastPrevID.Load().(string)
	if prev != "resp_B" {
		t.Fatalf("previous_response_id=%q want resp_B", prev)
	}
	if b.contOpens.Load() != 1 || a.contOpens.Load() != 0 {
		t.Fatalf("cont opens B=%d A=%d", b.contOpens.Load(), a.contOpens.Load())
	}
}

type fabricGooglePinMember struct {
	id               string
	sig              string
	opens            atomic.Int32
	failBeforeCommit atomic.Bool
	contOpens        atomic.Int32
	lastSig          atomic.Value // string
}

func (p *fabricGooglePinMember) Protocol() string { return "google" }

func (p *fabricGooglePinMember) Open(_ context.Context, req providercontract.DispatchRequest) (EventStream, error) {
	_ = p.opens.Add(1)
	if p.failBeforeCommit.Load() {
		return nil, &providercontract.OpenError{StatusCode: 503, Message: "google member " + p.id + " precommit unavailable"}
	}
	toolResults := 0
	sigSeen := ""
	for _, msg := range req.Parsed.Context.Messages {
		if msg.Role == protocol.RoleToolResult {
			toolResults++
		}
		for _, part := range msg.Content {
			if part.Type == protocol.ContentToolCall && strings.TrimSpace(part.ThoughtSignature) != "" {
				sigSeen = part.ThoughtSignature
			}
			if part.ProviderMetadata != nil && part.ProviderMetadata.Google != nil && part.ProviderMetadata.Google.ThoughtSignature != "" {
				sigSeen = part.ProviderMetadata.Google.ThoughtSignature
			}
		}
	}
	if toolResults > 0 || sigSeen != "" {
		p.contOpens.Add(1)
		p.lastSig.Store(sigSeen)
		return &sliceStream{events: []protocol.Event{
			{Type: protocol.EventTextDelta, Text: "final-google-" + p.id},
			{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
		}}, nil
	}
	args := `{"instruction":"CHILD_INSTRUCTION_SECRET"}`
	meta, _ := json.Marshal(map[string]any{"google": map[string]string{"thoughtSignature": p.sig}})
	return &sliceStream{events: []protocol.Event{
		{Type: protocol.EventToolCallStart, ID: "call_delegate", Name: fabric.FabricDelegateToolName},
		{Type: protocol.EventToolCallDelta, Arguments: args},
		{Type: protocol.EventToolCallEnd, ID: "call_delegate", Name: fabric.FabricDelegateToolName, Arguments: args, ProviderMetadata: meta},
		{Type: protocol.EventDone, Usage: &protocol.Usage{InputTokens: 1, OutputTokens: 1}},
	}}, nil
}

// TestFabricContinuation_GoogleComboPinsThoughtSignatureOwner: physical B owns the
// trusted thought signature; continuation must not hop to healed A (matching google
// protocol is insufficient across credentials/members).
func TestFabricContinuation_GoogleComboPinsThoughtSignatureOwner(t *testing.T) {
	a := &fabricGooglePinMember{id: "A", sig: "sig-A"}
	b := &fabricGooglePinMember{id: "B", sig: "sig-B-trusted"}
	a.failBeforeCommit.Store(true)
	child := &fabricPinChild{onOpen: func() { a.failBeforeCommit.Store(false) }}

	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4})
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{
			"google-a": a,
			"google-b": b,
			"openai-c": child,
		},
		ConfigPath:     configPath,
		ResourceBudget: budget,
		Combos: []Combo{{
			ID: "gpin",
			Targets: []ComboTarget{
				{ProviderID: "google-a", Model: "gem-a", Protocol: "google"},
				{ProviderID: "google-b", Model: "gem-b", Protocol: "google"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := attachHandlerClose(t, raw)
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute",
		`{"owner":"worker","model":"combo/gpin","input":"do","delegation":{"model":"openai-c/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("execute=%d %s", rr.Code, rr.Body.String())
	}
	body := waitFabricRunState(t, h, id, "completed")
	if !strings.Contains(body, `"runState":"completed"`) {
		t.Fatalf("want completed: %s", body)
	}
	if got := a.opens.Load(); got != 1 {
		t.Fatalf("A opens=%d want 1; google ownership hopped", got)
	}
	if got := b.opens.Load(); got != 2 {
		t.Fatalf("B opens=%d want 2", got)
	}
	if b.contOpens.Load() != 1 || a.contOpens.Load() != 0 {
		t.Fatalf("cont opens B=%d A=%d", b.contOpens.Load(), a.contOpens.Load())
	}
	sig, _ := b.lastSig.Load().(string)
	if sig != "sig-B-trusted" {
		t.Fatalf("continuation thought signature=%q want sig-B-trusted", sig)
	}
}

// TestFabricContinuation_DirectProviderStableWithoutExtraPinComplexity documents that
// a direct (non-combo/policy) primary already stays on the same provider instance path
// for continuation model identity; pin still supplies PreferCommitted harmlessly.
func TestFabricContinuation_DirectProviderStaysOnSameAccount(t *testing.T) {
	b := &fabricPinMember{id: "B", nativeRespID: "resp_direct_B"}
	child := &fabricPinChild{}
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"fabric":{"enabled":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	budget := resourcebudget.NewManager(resourcebudget.Limits{MaxActiveTurns: 4})
	raw, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-b": b, "openai-c": child},
		ConfigPath:     configPath,
		ResourceBudget: budget,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := attachHandlerClose(t, raw)
	id := fabricCreate(t, h)
	rr := fabricDo(h, http.MethodPost, "/api/fabric/tasks/"+id+"/execute",
		`{"owner":"worker","model":"openai-b/gpt-b","input":"do work","delegation":{"model":"openai-c/gpt-child"}}`)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("execute=%d %s", rr.Code, rr.Body.String())
	}
	waitFabricRunState(t, h, id, "completed")
	if b.opens.Load() != 2 {
		t.Fatalf("opens=%d want 2", b.opens.Load())
	}
	prev, _ := b.lastPrevID.Load().(string)
	if prev != "resp_direct_B" {
		t.Fatalf("previous_response_id=%q", prev)
	}
}
