package google

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/sidecar/fabric"
)

func TestFabricContinuation_GoogleCompilePreservesThoughtSignatureOnHistoricalCall(t *testing.T) {
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"final\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":1}}\n\n")
	}))
	defer upstream.Close()

	client, err := New(context.Background(), Config{APIKey: "k", HTTPClient: upstream.Client()})
	if err != nil {
		t.Fatal(err)
	}
	client.testOrigin = upstream.URL
	if client.Protocol() != "google" {
		t.Fatalf("protocol=%q", client.Protocol())
	}

	req := protocol.ParsedRequest{
		Source:          protocol.RequestSourceResponses,
		ModelID:         "custom-google/gemini",
		UpstreamModelID: "gemini-2.0-flash",
		Context: protocol.Context{
			Messages: []protocol.Message{
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "do"}}},
				{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
					Type:             protocol.ContentToolCall,
					ToolCallID:       "call_delegate",
					ToolName:         fabric.FabricDelegateToolName,
					Arguments:        map[string]any{"instruction": "x"},
					ThoughtSignature: "trusted-sig-abc",
					ProviderMetadata: &protocol.ProviderOpaqueMetadata{Google: &protocol.GoogleOpaqueMetadata{ThoughtSignature: "trusted-sig-abc"}},
				}}},
				{Role: protocol.RoleToolResult, ToolCallID: "call_delegate", ToolName: fabric.FabricDelegateToolName, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "child-out"}}},
			},
		},
		Options: protocol.RequestOptions{ToolChoice: &protocol.ToolChoice{Kind: protocol.ToolChoiceNone}},
	}
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: req})
	if err != nil {
		t.Fatalf("Open/compile: %v", err)
	}
	for {
		_, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	_ = stream.Close()

	contents, _ := gotBody["contents"].([]any)
	if len(contents) < 2 {
		t.Fatalf("contents=%#v", gotBody)
	}
	found := false
	for _, c := range contents {
		m, _ := c.(map[string]any)
		parts, _ := m["parts"].([]any)
		for _, p := range parts {
			pm, _ := p.(map[string]any)
			if _, ok := pm["functionCall"]; !ok {
				continue
			}
			sig, _ := pm["thoughtSignature"].(string)
			if sig != "trusted-sig-abc" {
				t.Fatalf("thoughtSignature=%q placement=%#v", sig, pm)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("historical functionCall with thoughtSignature missing: %#v", gotBody)
	}
	if tools, ok := gotBody["tools"]; ok && tools != nil {
		t.Fatalf("tools must be omitted when ToolChoice none / empty decls: %#v", tools)
	}
}

func TestFabricContinuation_GoogleCompileStripsSyntheticThoughtSignature(t *testing.T) {
	req := protocol.ParsedRequest{
		UpstreamModelID: "gemini-2.0-flash",
		Context: protocol.Context{
			Messages: []protocol.Message{
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "do"}}},
				{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
					Type:             protocol.ContentToolCall,
					ToolCallID:       "call_delegate",
					ToolName:         fabric.FabricDelegateToolName,
					Arguments:        map[string]any{"instruction": "x"},
					ThoughtSignature: "fc_synthetic",
				}}},
				{Role: protocol.RoleToolResult, ToolCallID: "call_delegate", ToolName: fabric.FabricDelegateToolName, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "out"}}},
			},
		},
	}
	body, err := CompileRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "fc_synthetic") || strings.Contains(string(raw), "thoughtSignature") {
		t.Fatalf("synthetic signature must not serialize: %s", raw)
	}
}
