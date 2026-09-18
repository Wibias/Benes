package openairesponses

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/responses/continuation"
	"github.com/Wibias/Benes/internal/sidecar/fabric"
)

func TestFabricContinuation_OpenAINativePreviousSerializesOnlyToolResult(t *testing.T) {
	var bodies []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		switch len(bodies) {
		case 1:
			fmt.Fprint(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_delegate\",\"name\":\"__benes_fabric_delegate_v1\",\"arguments\":\"\",\"status\":\"in_progress\"}}\n\n")
			fmt.Fprint(w, "data: {\"type\":\"response.function_call_arguments.done\",\"item_id\":\"fc_1\",\"arguments\":\"{\\\"instruction\\\":\\\"x\\\"}\"}\n\n")
			fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_native_primary\",\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"id\":\"fc_1\",\"call_id\":\"call_delegate\",\"name\":\"__benes_fabric_delegate_v1\",\"arguments\":\"{\\\"instruction\\\":\\\"x\\\"}\"}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
		default:
			fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"final\"}\n\n")
			fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_native_cont\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"final\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
		}
	}))
	defer upstream.Close()

	store := continuation.NewStore(continuation.StoreLimits{}, time.Now)
	authority, err := continuation.NewAuthority(store, nil, fabricContBytes32("install-salt-32-bytes-padded!!!!"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := New(Config{
		Endpoint: upstream.URL, APIKey: "k", HTTPClient: upstream.Client(),
		MaxStreamBytes: 1 << 20, InactivityTimeout: time.Second,
		Continuation: authority, ProviderID: "work-llm",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.Protocol() != "openai-responses" {
		t.Fatalf("protocol=%q", client.Protocol())
	}

	budget := resourcebudget.NewManager(resourcebudget.Limits{
		MaxActiveTurns: 4, MaxTurnBytes: 1 << 20, MaxProcessBytes: 1 << 20,
		ClassBytes: map[resourcebudget.Class]int64{resourcebudget.ClassContinuation: 1 << 20},
	})
	turn1, ok := budget.TryAcquireTurn("")
	if !ok {
		t.Fatal("turn1")
	}
	defer turn1.Close()

	primary := protocol.ParsedRequest{
		Source:          protocol.RequestSourceResponses,
		ModelID:         "work-llm/gpt",
		UpstreamModelID: "gpt",
		Context: protocol.Context{
			Messages: []protocol.Message{{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "do"}}}},
			Tools:    []protocol.Tool{{Name: fabric.FabricDelegateToolName, Parameters: map[string]any{"type": "object"}}},
		},
		Options: protocol.RequestOptions{ToolChoice: &protocol.ToolChoice{Kind: protocol.ToolChoiceAuto}, ParallelToolCalls: boolPtrFalse()},
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: primary, Turn: turn1})
	if err != nil {
		t.Fatalf("primary open: %v", err)
	}
	var nativeID string
	for {
		ev, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if ev.Type == protocol.EventDone {
			if raw, ok := ev.ProviderState["openai_responses_previous"]; ok {
				var body struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(raw, &body)
				nativeID = body.ID
			}
		}
	}
	_ = stream.Close()
	if nativeID != "resp_native_primary" {
		t.Fatalf("nativeID=%q", nativeID)
	}

	cont := protocol.ParsedRequest{
		Source:             protocol.RequestSourceResponses,
		ModelID:            "work-llm/gpt",
		UpstreamModelID:    "gpt",
		PreviousResponseID: nativeID,
		Context: protocol.Context{
			Messages: []protocol.Message{{
				Role:       protocol.RoleToolResult,
				ToolCallID: "call_delegate",
				ToolName:   fabric.FabricDelegateToolName,
				Content:    []protocol.ContentPart{{Type: protocol.ContentText, Text: "child-out"}},
			}},
		},
		Options: protocol.RequestOptions{ToolChoice: &protocol.ToolChoice{Kind: protocol.ToolChoiceNone}, ParallelToolCalls: boolPtrFalse()},
	}
	turn2, ok := budget.TryAcquireTurn("")
	if !ok {
		t.Fatal("turn2")
	}
	defer turn2.Close()
	stream2, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: cont, Turn: turn2})
	if err != nil {
		t.Fatalf("continuation open/compile: %v", err)
	}
	for {
		_, err := stream2.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = stream2.Close()

	if len(bodies) < 2 {
		t.Fatalf("bodies=%d", len(bodies))
	}
	contBody := bodies[1]
	if contBody["previous_response_id"] != "resp_native_primary" {
		t.Fatalf("previous_response_id=%#v (must be native, not bridge)", contBody["previous_response_id"])
	}
	input, _ := contBody["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("want only new tool result input, got %#v", input)
	}
	item, _ := input[0].(map[string]any)
	if item["type"] != "function_call_output" || item["call_id"] != "call_delegate" {
		t.Fatalf("input item=%#v", item)
	}
	if tools, ok := contBody["tools"]; ok && tools != nil {
		if arr, _ := tools.([]any); len(arr) > 0 {
			t.Fatalf("tools must be absent/empty: %#v", tools)
		}
	}
}

func TestFabricContinuation_BridgeReplayUndeclaredToolFailsAtCompiler(t *testing.T) {
	req := protocol.ParsedRequest{
		Source:             protocol.RequestSourceResponses,
		UpstreamModelID:    "gpt",
		PreviousResponseID: "resp_bridge_public_1",
		Context: protocol.Context{
			Messages: []protocol.Message{
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}}},
				{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
					Type: protocol.ContentToolCall, ToolCallID: "call_delegate", ToolName: fabric.FabricDelegateToolName, Arguments: map[string]any{"instruction": "x"},
				}}},
				{Role: protocol.RoleToolResult, ToolCallID: "call_delegate", ToolName: fabric.FabricDelegateToolName, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "out"}}},
			},
		},
	}
	_, err := Compile(req)
	if err == nil {
		t.Fatal("expected undeclared historical tool failure from real compiler")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "tool") && !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("err=%v", err)
	}
}

func fabricContBytes32(s string) []byte {
	b := make([]byte, 32)
	copy(b, s)
	return b
}

func boolPtrFalse() *bool {
	v := false
	return &v
}
