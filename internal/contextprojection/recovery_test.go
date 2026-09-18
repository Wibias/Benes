package contextprojection

import (
	"encoding/base64"
	"strconv"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func registryWith(content string) (*Registry, Artifact) {
	registry := NewRegistry()
	lineCount := 1
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			lineCount++
		}
	}
	artifact := registry.Register(
		Identity{ToolCallID: "call-1", ToolNamespace: "tools", ToolName: "exec", OccurrenceOrdinal: 0},
		0,
		content,
		len(content),
		lineCount,
		false,
	)
	return registry, artifact
}

func TestRecoveryToolNameAndAdvertisement(t *testing.T) {
	if RecoveryToolName != "__benes_context_v1" {
		t.Fatalf("name=%q", RecoveryToolName)
	}
	tool := RecoveryTool()
	if tool.Name != RecoveryToolName {
		t.Fatalf("tool name=%q", tool.Name)
	}
	tools := []protocol.Tool{{Name: "read", Description: "read", Parameters: map[string]any{"type": "object"}}}
	added, err := AddRecoveryTool(tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 {
		t.Fatal("caller tools mutated")
	}
	if added[len(added)-1].Name != RecoveryToolName {
		t.Fatal("recovery tool not appended")
	}
	_, err = AddRecoveryTool(append(tools, protocol.Tool{Name: RecoveryToolName, Description: "client", Parameters: map[string]any{"type": "object"}}))
	if err == nil || err.Error() != "tool_name_collision" {
		t.Fatalf("collision=%v", err)
	}
}

func TestReadPagesLineRangeWithoutSplittingAstralUnicode(t *testing.T) {
	source := "one\r\ntwo😀two\n" + strings.Repeat("x", 200) + "\nlast"
	registry, artifact := registryWith(source)
	session := NewRecoverySession(registry, nil)
	first := session.Read(artifact.Ref, ReadOptions{StartLine: 2, EndLine: 4, MaxBytes: 18, HasLines: true})
	if !first.OK {
		t.Fatalf("first=%+v", first)
	}
	if !strings.HasPrefix(first.Content, "two😀two\n") {
		t.Fatalf("content=%q", first.Content)
	}
	if !strings.HasPrefix(first.NextCursor, "cur_") {
		t.Fatalf("cursor=%q", first.NextCursor)
	}
	nonceText := strings.TrimPrefix(first.NextCursor, "cur_")
	nonce, err := base64.RawURLEncoding.DecodeString(nonceText)
	if err != nil || len(nonceText) != 16 || len(nonce) != 12 {
		t.Fatalf("cursor=%q nonce_len=%d decoded_len=%d err=%v", first.NextCursor, len(nonceText), len(nonce), err)
	}
	second := session.Read(artifact.Ref, ReadOptions{Cursor: first.NextCursor, MaxBytes: MaxReadBytesPerCall})
	if !second.OK {
		t.Fatalf("second=%+v", second)
	}
	if first.Content+second.Content != source[strings.Index(source, "two😀two"):] {
		t.Fatal("cursor paging did not rebuild the exact remainder")
	}
	if !second.EOF {
		t.Fatal("expected eof")
	}
}

func TestOneMegabyteReadHitsOuterByteBudget(t *testing.T) {
	source := "A😀" + strings.Repeat("z", 1024*1024) + "END"
	registry, artifact := registryWith(source)
	session := NewRecoverySession(registry, nil)
	var cursor string
	var rebuilt string
	for len(rebuilt) < MaxReturnedBytesPerTurn {
		opts := ReadOptions{MaxBytes: MaxReadBytesPerCall}
		if cursor != "" {
			opts.Cursor = cursor
		} else {
			opts.StartLine = 1
			opts.EndLine = 1
			opts.HasLines = true
		}
		result := session.Read(artifact.Ref, opts)
		if !result.OK {
			t.Fatalf("read=%+v", result)
		}
		rebuilt += result.Content
		cursor = result.NextCursor
		if cursor == "" {
			break
		}
	}
	if !strings.HasPrefix(source, rebuilt) {
		t.Fatal("rebuilt prefix mismatch")
	}
	if len(rebuilt) != MaxReturnedBytesPerTurn {
		t.Fatalf("rebuilt=%d want %d", len(rebuilt), MaxReturnedBytesPerTurn)
	}
	if cursor == "" {
		t.Fatal("expected leftover cursor")
	}
	got := session.Read(artifact.Ref, ReadOptions{Cursor: cursor, MaxBytes: 1})
	if got.OK || got.Error != ErrByteLimit {
		t.Fatalf("got=%+v", got)
	}
}

func TestLiteralGrepIsBoundedAndNonRegex(t *testing.T) {
	lines := make([]string, MaxGrepMatches+20)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i) + " [a.*b] needle"
	}
	source := strings.Join(lines, "\n")
	registry, artifact := registryWith(source)
	session := NewRecoverySession(registry, nil)
	grep := session.Grep(artifact.Ref, "[a.*b]")
	if !grep.OK {
		t.Fatalf("grep=%+v", grep)
	}
	if len(grep.Matches) != MaxGrepMatches || !grep.Truncated {
		t.Fatalf("matches=%d truncated=%v", len(grep.Matches), grep.Truncated)
	}
	exact := session.Read(artifact.Ref, ReadOptions{Cursor: grep.Matches[0].Cursor, MaxBytes: 64})
	if !exact.OK || !strings.Contains(exact.Content, "[a.*b]") {
		t.Fatalf("exact=%+v", exact)
	}
	tooLong := session.Grep(artifact.Ref, strings.Repeat("x", MaxGrepQueryChars+1))
	if tooLong.OK || tooLong.Error != ErrQueryTooLong {
		t.Fatalf("tooLong=%+v", tooLong)
	}
}

func TestCursorsAreRequestLocalAndCallBudgetsIndependent(t *testing.T) {
	registry, artifact := registryWith("hello world")
	firstSession := NewRecoverySession(registry, nil)
	first := firstSession.Read(artifact.Ref, ReadOptions{MaxBytes: 5})
	if !first.OK {
		t.Fatalf("first=%+v", first)
	}
	secondSession := NewRecoverySession(registry, nil)
	stale := secondSession.Read(artifact.Ref, ReadOptions{Cursor: first.NextCursor, MaxBytes: 5})
	if stale.OK || stale.Error != ErrStaleCursor {
		t.Fatalf("stale=%+v", stale)
	}

	budget := NewRecoverySession(registry, nil)
	for i := 0; i < MaxCallsPerTurn; i++ {
		got := budget.Read(artifact.Ref, ReadOptions{StartLine: 1, EndLine: 1, MaxBytes: 1, HasLines: true})
		if !got.OK {
			t.Fatalf("call %d: %+v", i, got)
		}
	}
	limited := budget.Read(artifact.Ref, ReadOptions{StartLine: 1, EndLine: 1, MaxBytes: 1, HasLines: true})
	if limited.OK || limited.Error != ErrCallLimit {
		t.Fatalf("limited=%+v", limited)
	}
}

func TestHandleCallReadsExactOmittedText(t *testing.T) {
	registry, artifact := registryWith("hello-recovery")
	session := NewRecoverySession(registry, nil)
	payload, failOpen := session.HandleCall(`{"op":"read","ref":"` + artifact.Ref + `"}`)
	if failOpen || !strings.Contains(payload, `"ok":true`) || !strings.Contains(payload, "hello-recovery") {
		t.Fatalf("payload=%s failOpen=%v", payload, failOpen)
	}
	missing, failOpen := session.HandleCall(`{"op":"read","ref":"nope"}`)
	if failOpen || !strings.Contains(missing, `"error":"not_found"`) {
		t.Fatalf("missing=%s failOpen=%v", missing, failOpen)
	}
}
