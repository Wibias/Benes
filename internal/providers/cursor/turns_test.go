package cursor

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func TestUserMessageGoldenBytes(t *testing.T) {
	got := encodeTurnUserMessage("first", "digest", 0)
	// Field 1 text="first" is stable; message_id is a digest suffix.
	if !bytes.HasPrefix(got, []byte{0x0a, 0x05, 'f', 'i', 'r', 's', 't'}) {
		t.Fatalf("user message=%x", got)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "user_message.golden"))
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := hex.DecodeString(strings.TrimSpace(string(want)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(got, prefix) {
		t.Fatalf("golden prefix=%x got=%x", prefix, got)
	}
}

func TestConversationTurnsUseGeneratedProtoShapes(t *testing.T) {
	req, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{
			SystemPrompt: []string{"sys"},
			Messages: []protocol.Message{
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "first"}}},
				{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "ok"}}},
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "second"}}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	turns := ConversationTurnBlobs(req)
	if len(turns) != 1 {
		t.Fatalf("turns=%d", len(turns))
	}
	state := encodeConversationState(req)
	fields, err := decodeProtoFields(state)
	if err != nil {
		t.Fatal(err)
	}
	var turnIDs [][]byte
	for _, field := range fields {
		if field.num == 8 {
			turnIDs = append(turnIDs, field.bytes)
		}
	}
	if len(turnIDs) != 1 || !bytes.Equal(turnIDs[0], BlobID(turns[0])) {
		t.Fatalf("turn ids=%x want=%x", turnIDs, BlobID(turns[0]))
	}
	root, err := decodeProtoFields(turns[0])
	if err != nil {
		t.Fatal(err)
	}
	agent, err := decodeProtoFields(fieldBytes(root, 1))
	if err != nil || len(fieldBytes(agent, 1)) != 32 {
		t.Fatalf("agent turn=%x err=%v", turns[0], err)
	}
}

func TestConversationTurnsOmitActiveUserAndKeepAction(t *testing.T) {
	req, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "first"}}},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "ok"}}},
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "second"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, blob := range append(RootPromptBlobs(req), flattenConversationBlobs(req)...) {
		if strings.Contains(string(blob), "second") {
			t.Fatalf("active user leaked: %s", blob)
		}
	}
	action, err := decodeProtoFields(encodeConversationAction(req))
	if err != nil {
		t.Fatal(err)
	}
	if len(fieldBytes(action, 1)) == 0 {
		t.Fatal("expected user_message_action")
	}
	if string(fieldBytes(mustFields(t, fieldBytes(mustFields(t, fieldBytes(action, 1)), 1)), 1)) != "second" {
		t.Fatal("active user missing from action")
	}
}

func TestToolCallAndResultStayPaired(t *testing.T) {
	req, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "use tool"}}},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{
				Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup", Arguments: map[string]any{"q": "x"},
			}}},
			{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "found"}}},
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "next"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	turns := ConversationTurnBlobs(req)
	if len(turns) != 1 {
		t.Fatalf("turns=%d", len(turns))
	}
	store := NewBlobStore()
	if err := store.PutConversation(req); err != nil {
		t.Fatal(err)
	}
	agent := mustFields(t, fieldBytes(mustFields(t, turns[0]), 1))
	stepID := fieldBytes(agent, 2)
	stepRaw, ok := store.Get(stepID)
	if !ok {
		t.Fatal("missing step blob")
	}
	step := mustFields(t, stepRaw)
	if len(fieldBytes(step, 2)) == 0 {
		t.Fatalf("expected tool_call step, got %x", stepRaw)
	}
	tool := mustFields(t, fieldBytes(step, 2))
	mcp := mustFields(t, fieldBytes(tool, 15))
	args := mustFields(t, fieldBytes(mcp, 1))
	if string(fieldBytes(args, 3)) != "c1" {
		t.Fatalf("call id=%q", fieldBytes(args, 3))
	}
	if len(fieldBytes(mcp, 2)) == 0 {
		t.Fatal("tool result missing from paired step")
	}
}

func TestDuplicateToolCallIDPairsByFullIdentity(t *testing.T) {
	req, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "use tools"}}},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{
				{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup", Arguments: map[string]any{"q": "x"}},
				{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "search", Arguments: map[string]any{"q": "y"}},
			}},
			{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "found"}}},
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "next"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	steps := conversationToolSteps(t, req)
	if len(steps) != 2 {
		t.Fatalf("steps=%#v", steps)
	}
	if steps[0].name != "lookup" || !steps[0].hasResult || steps[0].id != "c1" {
		t.Fatalf("lookup should keep its result: %#v", steps[0])
	}
	if steps[1].name != "search" || steps[1].hasResult {
		t.Fatalf("search must not steal the lookup result: %#v", steps[1])
	}
}

func TestBlankToolCallIDDoesNotDropOrRebind(t *testing.T) {
	req, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "use tools"}}},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{
				{Type: protocol.ContentToolCall, ToolName: "lookup", Arguments: map[string]any{"q": "x"}},
				{Type: protocol.ContentToolCall, ToolName: "search", Arguments: map[string]any{"q": "y"}},
			}},
			{Role: protocol.RoleToolResult, ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "found"}}},
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "next"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	steps := conversationToolSteps(t, req)
	if len(steps) != 2 {
		t.Fatalf("blank ids must not drop the first call: %#v", steps)
	}
	if steps[0].name != "lookup" || steps[0].hasResult {
		t.Fatalf("blank lookup must fail closed: %#v", steps[0])
	}
	if steps[1].name != "search" || steps[1].hasResult {
		t.Fatalf("blank search must fail closed: %#v", steps[1])
	}
}

func TestNamespacedToolNameIsPartOfPairingIdentity(t *testing.T) {
	req, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "use tools"}}},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{
				{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolNamespace: "mcp", ToolName: "lookup", Arguments: map[string]any{"q": "x"}},
			}},
			{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolNamespace: "other", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "found"}}},
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "next"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	steps := conversationToolSteps(t, req)
	if len(steps) != 1 || steps[0].name != "mcp__lookup" || steps[0].hasResult {
		t.Fatalf("namespace mismatch must not pair: %#v", steps)
	}
}

func TestAmbiguousDuplicateIdentityFailsClosed(t *testing.T) {
	req, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "use tools"}}},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{
				{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup", Arguments: map[string]any{"q": "x"}},
				{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup", Arguments: map[string]any{"q": "y"}},
			}},
			{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "found"}}},
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "next"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	steps := conversationToolSteps(t, req)
	if len(steps) != 2 {
		t.Fatalf("steps=%#v", steps)
	}
	if steps[0].hasResult || steps[1].hasResult {
		t.Fatalf("ambiguous id+name must not rebind: %#v", steps)
	}
}

func TestUniqueParallelToolCallsStillPair(t *testing.T) {
	req, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "use tools"}}},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{
				{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup", Arguments: map[string]any{"q": "x"}},
				{Type: protocol.ContentToolCall, ToolCallID: "c2", ToolName: "search", Arguments: map[string]any{"q": "y"}},
			}},
			{Role: protocol.RoleToolResult, ToolCallID: "c1", ToolName: "lookup", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "found"}}},
			{Role: protocol.RoleToolResult, ToolCallID: "c2", ToolName: "search", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "hits"}}},
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "next"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	steps := conversationToolSteps(t, req)
	if len(steps) != 2 {
		t.Fatalf("steps=%#v", steps)
	}
	if steps[0].id != "c1" || steps[0].name != "lookup" || !steps[0].hasResult {
		t.Fatalf("c1=%#v", steps[0])
	}
	if steps[1].id != "c2" || steps[1].name != "search" || !steps[1].hasResult {
		t.Fatalf("c2=%#v", steps[1])
	}
}

type conversationToolStep struct {
	id, name  string
	hasResult bool
}

func conversationToolSteps(t *testing.T, req RunRequest) []conversationToolStep {
	t.Helper()
	turns := ConversationTurnBlobs(req)
	if len(turns) != 1 {
		t.Fatalf("turns=%d", len(turns))
	}
	store := NewBlobStore()
	if err := store.PutConversation(req); err != nil {
		t.Fatal(err)
	}
	agent := mustFields(t, fieldBytes(mustFields(t, turns[0]), 1))
	var steps []conversationToolStep
	for _, field := range agent {
		if field.num != 2 {
			continue
		}
		stepRaw, ok := store.Get(field.bytes)
		if !ok {
			t.Fatal("missing step blob")
		}
		step := mustFields(t, stepRaw)
		toolRaw := fieldBytes(step, 2)
		if len(toolRaw) == 0 {
			continue
		}
		mcp := mustFields(t, fieldBytes(mustFields(t, toolRaw), 15))
		args := mustFields(t, fieldBytes(mcp, 1))
		steps = append(steps, conversationToolStep{
			id:        string(fieldBytes(args, 3)),
			name:      string(fieldBytes(args, 1)),
			hasResult: len(fieldBytes(mcp, 2)) != 0,
		})
	}
	return steps
}

func TestHistoricalImagesStayMarkersAndOmitBytes(t *testing.T) {
	png := "data:image/png;base64,iVBORw0KGgo="
	req, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{
				{Type: protocol.ContentText, Text: "look"},
				{Type: protocol.ContentImage, ImageURL: png},
			}},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "ok"}}},
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "now"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, blob := range append(RootPromptBlobs(req), flattenConversationBlobs(req)...) {
		if bytes.Contains(blob, []byte("iVBORw0KGgo")) || bytes.Contains(blob, []byte{0x89, 'P', 'N', 'G'}) {
			t.Fatalf("historical image bytes replayed: %q", blob)
		}
		if strings.Contains(string(blob), "look") && !strings.Contains(string(blob), "[image") {
			t.Fatalf("expected image marker in %s", blob)
		}
	}
}

func TestEncryptedConversationFailsClosed(t *testing.T) {
	_, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{{
			Role: protocol.RoleUser, ContainsEncryptedContent: true,
			Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "secret"}},
		}}},
	})
	if err == nil {
		t.Fatal("encrypted")
	}
}

func TestToolOnlyContinuationUsesResumeAction(t *testing.T) {
	req, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{Messages: []protocol.Message{
			{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "use"}}},
			{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentToolCall, ToolCallID: "c1", ToolName: "lookup"}}},
			{Role: protocol.RoleToolResult, ToolCallID: "c1", Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "out"}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	action := mustFields(t, encodeConversationAction(req))
	if len(fieldBytes(action, 1)) != 0 {
		t.Fatal("tool continuation must not emit user_message_action")
	}
	if _, ok := fieldBytesPresent(action, 2); !ok {
		t.Fatal("expected resume_action")
	}
	if len(ConversationTurnBlobs(req)) != 1 {
		t.Fatal("tool suffix must remain in history turns")
	}
}

func TestServedBlobBytesMatchAdvertisedDigest(t *testing.T) {
	req, err := CompileRun(protocol.ParsedRequest{
		UpstreamModelID: "gpt-5.4",
		Context: protocol.Context{
			SystemPrompt: []string{"sys"},
			Messages: []protocol.Message{
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "first"}}},
				{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "ok"}}},
				{Role: protocol.RoleUser, Content: []protocol.ContentPart{{Type: protocol.ContentText, Text: "next"}}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := NewBlobStore()
	if err := store.PutConversation(req); err != nil {
		t.Fatal(err)
	}
	state := mustFields(t, encodeConversationState(req))
	for _, field := range state {
		if field.num != 1 && field.num != 8 {
			continue
		}
		got, ok := store.Get(field.bytes)
		if !ok || !bytes.Equal(BlobID(got), field.bytes) {
			t.Fatalf("digest mismatch field=%d ok=%v", field.num, ok)
		}
	}
}

func mustFields(t *testing.T, raw []byte) []protoField {
	t.Helper()
	fields, err := decodeProtoFields(raw)
	if err != nil {
		t.Fatal(err)
	}
	return fields
}

func fieldBytesPresent(fields []protoField, num int) ([]byte, bool) {
	for _, field := range fields {
		if field.num == num && field.wire == 2 {
			return field.bytes, true
		}
	}
	return nil, false
}
