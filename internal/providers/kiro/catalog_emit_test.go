package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
)

func TestOpenEmitsMaterializedToolCatalogOnCurrentUserMessage(t *testing.T) {
	body := openCapture(t, protocol.ParsedRequest{
		UpstreamModelID: "claude-sonnet-4",
		Context: protocol.Context{
			Messages: []protocol.Message{{
				Role:    protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
			}},
			Tools: []protocol.Tool{
				{Name: "exec", Freeform: true, Description: "Code Mode"},
				{Name: "spawn_agent", Description: "Delegate", Parameters: map[string]any{"type": "object"}},
				{Name: "tool_search", ToolSearch: true, Parameters: map[string]any{"type": "object"}},
				{
					Name:                 "loaded_helper",
					LoadedFromToolSearch: true,
					Parameters:           map[string]any{"type": "object"},
				},
			},
		},
	})
	names := emittedToolSpecNames(t, body)
	if strings.Join(names, ",") != "exec,spawn_agent,tool_search" {
		t.Fatalf("emitted=%v body=%s", names, compactJSON(body))
	}
	if jsonContainsKey(body, "parallel_tool_calls") || jsonContainsKey(body, "parallelToolCalls") {
		t.Fatalf("parallel control leaked: %s", compactJSON(body))
	}
	specs := emittedToolSpecs(t, body)
	execSpec := specs[0].(map[string]any)["toolSpecification"].(map[string]any)
	if execSpec["name"] != "exec" || execSpec["description"] != "Code Mode" {
		t.Fatalf("exec spec=%#v", execSpec)
	}
	schema, ok := execSpec["inputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("exec inputSchema missing: %#v", execSpec)
	}
	if _, ok := schema["json"]; !ok {
		t.Fatalf("exec inputSchema.json missing: %#v", schema)
	}
}

func TestOpenSanitizesKiroToolSchemaKeywordsWithoutRenamingProperties(t *testing.T) {
	body := openCapture(t, protocol.ParsedRequest{
		UpstreamModelID: "claude-sonnet-4",
		Context: protocol.Context{
			Messages: []protocol.Message{{
				Role:    protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
			}},
			Tools: []protocol.Tool{{
				Name: "memories__add",
				Parameters: map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"format": map[string]any{"type": "string", "format": "email"},
					},
					"required": []any{},
				},
			}},
		},
	})
	spec := emittedToolSpecs(t, body)[0].(map[string]any)["toolSpecification"].(map[string]any)
	schema := spec["inputSchema"].(map[string]any)["json"].(map[string]any)
	if _, exists := schema["additionalProperties"]; exists {
		t.Fatalf("additionalProperties leaked: %#v", schema)
	}
	props := schema["properties"].(map[string]any)
	if _, ok := props["format"]; !ok {
		t.Fatalf("property format deleted: %#v", schema)
	}
	if _, ok := props["format"].(map[string]any)["format"]; ok {
		t.Fatalf("schema keyword format survived: %#v", props["format"])
	}
}

func TestOpenPinsCodeModeExecWhenCatalogBytesAreTight(t *testing.T) {
	fillers := make([]protocol.Tool, 0, 4)
	for i := 0; i < 4; i++ {
		fillers = append(fillers, protocol.Tool{
			Name:        "filler_" + string(rune('a'+i)),
			Description: strings.Repeat("z", 10_000),
			Parameters:  map[string]any{"type": "object"},
		})
	}
	body := openCapture(t, protocol.ParsedRequest{
		UpstreamModelID: "claude-sonnet-4",
		Context: protocol.Context{
			Messages: []protocol.Message{{
				Role:    protocol.RoleUser,
				Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
			}},
			Tools: append(fillers, protocol.Tool{Name: "exec", Freeform: true, Description: "Code Mode"}),
		},
	})
	names := emittedToolSpecNames(t, body)
	if !containsName(names, "exec") {
		t.Fatalf("code-mode exec missing under bound: %v", names)
	}
}

func TestOpenKeepsToolResultsBesideEmittedCatalog(t *testing.T) {
	body := openCapture(t, protocol.ParsedRequest{
		UpstreamModelID: "claude-sonnet-4",
		Context: protocol.Context{
			Messages: []protocol.Message{
				{
					Role: protocol.RoleUser,
					Content: []protocol.ContentPart{{
						Type: protocol.ContentText, Text: "use it",
					}},
				},
				{
					Role: protocol.RoleAssistant,
					Content: []protocol.ContentPart{{
						Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "exec",
						Arguments: map[string]any{"cmd": "echo"},
					}},
				},
				{
					Role:       protocol.RoleToolResult,
					ToolCallID: "c1",
					ToolName:   "exec",
					Content:    []protocol.ContentPart{{Type: protocol.ContentText, Text: "ok"}},
				},
			},
			Tools: []protocol.Tool{{Name: "exec", Freeform: true, Description: "Code Mode"}},
		},
	})
	ctx := currentUserInputContext(t, body)
	if _, ok := ctx["toolResults"]; !ok {
		t.Fatalf("toolResults missing: %#v", ctx)
	}
	if _, ok := ctx["tools"]; !ok {
		t.Fatalf("tools missing next to toolResults: %#v", ctx)
	}
	if jsonContainsKey(ctx, "unavailable") || jsonContainsKey(ctx, "unavailableTools") {
		t.Fatalf("unexpected unavailable diagnostic: %#v", ctx)
	}
}

func TestOpenOmitsToolCatalogWhenNoToolsDeclared(t *testing.T) {
	body := openCapture(t, protocol.ParsedRequest{
		UpstreamModelID: "claude-sonnet-4",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hi"}},
		}}},
	})
	uim := currentUserInputMessage(t, body)
	if _, ok := uim["userInputMessageContext"]; ok {
		t.Fatalf("empty context: %#v", uim)
	}
	if jsonContainsKey(body, "toolSpecification") || jsonContainsKey(body, "tools") {
		t.Fatalf("catalog leaked without tools: %s", compactJSON(body))
	}
}

func openCapture(t *testing.T, parsed protocol.ParsedRequest) map[string]any {
	t.Helper()
	var body map[string]any
	frame := EncodeEventStreamMessage(map[string]string{":event-type": "assistantResponseEvent"}, []byte(`{"content":"ok"}`))
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		_, _ = w.Write(frame)
	}))
	t.Cleanup(upstream.Close)
	client, err := NewHardened(context.Background(), Config{
		Account: AccountSnapshot{
			AccessToken: "tok",
			ProfileARN:  "arn:aws:codewhisperer:us-east-1:123456789012:profile/abc",
			APIRegion:   "us-east-1",
		},
		HTTPClient: upstream.Client(),
		Endpoint:   "https://runtime.us-east-1.kiro.dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	client.endpoint = upstream.URL
	stream, err := client.Open(context.Background(), providers.DispatchRequest{Parsed: parsed})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	return body
}

func currentUserInputMessage(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	state, _ := body["conversationState"].(map[string]any)
	current, _ := state["currentMessage"].(map[string]any)
	uim, _ := current["userInputMessage"].(map[string]any)
	if uim == nil {
		t.Fatalf("userInputMessage missing: %#v", body)
	}
	return uim
}

func currentUserInputContext(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	uim := currentUserInputMessage(t, body)
	ctx, _ := uim["userInputMessageContext"].(map[string]any)
	if ctx == nil {
		t.Fatalf("userInputMessageContext missing: %#v", uim)
	}
	return ctx
}

func emittedToolSpecs(t *testing.T, body map[string]any) []any {
	t.Helper()
	ctx := currentUserInputContext(t, body)
	raw, ok := ctx["tools"].([]any)
	if !ok || len(raw) == 0 {
		t.Fatalf("tools missing: %#v", ctx)
	}
	return raw
}

func emittedToolSpecNames(t *testing.T, body map[string]any) []string {
	t.Helper()
	var names []string
	for _, item := range emittedToolSpecs(t, body) {
		obj, _ := item.(map[string]any)
		spec, _ := obj["toolSpecification"].(map[string]any)
		name, _ := spec["name"].(string)
		if name == "" {
			t.Fatalf("toolSpecification.name missing: %#v", item)
		}
		names = append(names, name)
	}
	return names
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func compactJSON(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func TestOpenLiveAcceptsToolSpecCatalog(t *testing.T) {
	if os.Getenv("BENES_KIRO_LIVE_PROBE") != "1" {
		t.Skip("set BENES_KIRO_LIVE_PROBE=1")
	}
	imported, err := ImportLocalSessionStable(LiveHost())
	if err != nil {
		t.Fatal(err)
	}
	snap, err := ImportSnapshot(imported.Credential)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if refreshed, refreshErr := RefreshToken(ctx, RefreshInput{
		Refresh: imported.Credential.Refresh,
		Stored: &RefreshAccount{
			ProfileARN:   imported.Credential.ProfileARN,
			SSORegion:    imported.Credential.SSORegion,
			APIRegion:    imported.Credential.APIRegion,
			ClientID:     imported.Credential.ClientID,
			ClientSecret: imported.Credential.ClientSecret,
			Source:       imported.Source,
		},
		Host: LiveHost(),
	}); refreshErr != nil {
		t.Skip("Kiro access token could not be refreshed; re-run benes login kiro")
	} else if token := strings.TrimSpace(refreshed.AccessToken); token != "" {
		snap.AccessToken = token
	}
	client, err := NewHardened(ctx, Config{Account: snap})
	if err != nil {
		t.Fatal(err)
	}
	cap := &liveCatalogCapture{base: client.httpClient.Transport}
	if cap.base == nil {
		cap.base = http.DefaultTransport
	}
	wrapped := *client.httpClient
	wrapped.Transport = cap
	client.httpClient = &wrapped
	noTools := protocol.ParsedRequest{
		UpstreamModelID: "claude-sonnet-4.5",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "Reply with the single word NONE."}},
		}}},
	}
	control, err := client.Open(ctx, providers.DispatchRequest{Parsed: noTools})
	if err != nil {
		t.Fatalf("control open status=%d class=%s err=%v", cap.status, cap.errClass, err)
	}
	_, _ = control.Next()
	_ = control.Close()
	if cap.status < 200 || cap.status >= 300 {
		t.Fatalf("control status=%d class=%s", cap.status, cap.errClass)
	}
	stream, err := client.Open(ctx, providers.DispatchRequest{Parsed: protocol.ParsedRequest{
		UpstreamModelID: "claude-sonnet-4.5",
		Context: protocol.Context{
			Messages: []protocol.Message{{
				Role: protocol.RoleUser,
				Content: []protocol.ContentPart{{
					Type: protocol.ContentText,
					Text: "If you have tools, do not call them. Reply with the single word NONE.",
				}},
			}},
			Tools: []protocol.Tool{
				{Name: "exec", Freeform: true, Description: "Code Mode"},
				{Name: "spawn_agent", Description: "Delegate", Parameters: map[string]any{"type": "object"}},
			},
		},
	}})
	if err != nil {
		t.Fatalf("open status=%d class=%s tools=%v err=%v", cap.status, cap.errClass, cap.toolNames, err)
	}
	defer stream.Close()
	var types []string
	for {
		ev, nextErr := stream.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			t.Fatalf("stream status=%d class=%s tools=%v err=%v", cap.status, cap.errClass, cap.toolNames, nextErr)
		}
		types = append(types, string(ev.Type))
		if ev.Type == protocol.EventDone || ev.Type == protocol.EventError {
			break
		}
	}
	if cap.status < 200 || cap.status >= 300 {
		t.Fatalf("status=%d class=%s tools=%v events=%v", cap.status, cap.errClass, cap.toolNames, types)
	}
	if !containsName(cap.toolNames, "exec") || !containsName(cap.toolNames, "spawn_agent") {
		t.Fatalf("live catalog names=%v", cap.toolNames)
	}
}

type liveCatalogCapture struct {
	base      http.RoundTripper
	status    int
	errClass  string
	toolNames []string
}

func (c *liveCatalogCapture) RoundTrip(req *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(raw))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	req.ContentLength = int64(len(raw))
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	c.toolNames = liveToolNames(body)
	resp, err := c.base.RoundTrip(req)
	if resp != nil {
		c.status = resp.StatusCode
		if resp.StatusCode >= 300 {
			errRaw, readErr := io.ReadAll(resp.Body)
			if readErr == nil {
				resp.Body = io.NopCloser(bytes.NewReader(errRaw))
				c.errClass = liveErrorClass(errRaw)
			}
		}
	}
	return resp, err
}

func liveErrorClass(raw []byte) string {
	var parsed map[string]any
	if json.Unmarshal(raw, &parsed) != nil {
		return "non_json"
	}
	for _, key := range []string{"__type", "code"} {
		if value, ok := parsed[key].(string); ok && value != "" {
			if msg, ok := parsed["message"].(string); ok && msg != "" {
				if len(msg) > 160 {
					msg = msg[:160]
				}
				return value + " " + msg
			}
			return value
		}
	}
	keys := make([]string, 0, len(parsed))
	for key := range parsed {
		keys = append(keys, key)
	}
	return "keys=" + strings.Join(keys, ",")
}

func liveToolNames(body map[string]any) []string {
	state, _ := body["conversationState"].(map[string]any)
	current, _ := state["currentMessage"].(map[string]any)
	uim, _ := current["userInputMessage"].(map[string]any)
	ctx, _ := uim["userInputMessageContext"].(map[string]any)
	raw, _ := ctx["tools"].([]any)
	var names []string
	for _, item := range raw {
		obj, _ := item.(map[string]any)
		spec, _ := obj["toolSpecification"].(map[string]any)
		name, _ := spec["name"].(string)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
