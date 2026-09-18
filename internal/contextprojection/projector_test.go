package contextprojection

import (
	"context"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/protocol"
)

func testDuplicatePolicy() Policy {
	return Policy{
		SchemaVersion:       1,
		Variant:             VariantDuplicateV1,
		PolicyID:            "test-duplicate-v1",
		DuplicateMinBytes:   1,
		LargeMinBytes:       96 * 1024,
		PreviewHeadBytes:    8 * 1024,
		PreviewTailBytes:    8 * 1024,
		MaxScannedUTF8Bytes: 64 * 1024 * 1024,
	}
}

func testRecoveryPolicy() Policy {
	return Policy{
		SchemaVersion:       1,
		Variant:             VariantRecoveryV1,
		PolicyID:            "test-recovery-v1",
		DuplicateMinBytes:   1,
		LargeMinBytes:       16,
		PreviewHeadBytes:    5,
		PreviewTailBytes:    5,
		MaxScannedUTF8Bytes: 64 * 1024 * 1024,
	}
}

func toolResult(content string, mutate func(*protocol.Message)) protocol.Message {
	msg := protocol.Message{
		Role:          protocol.RoleToolResult,
		ToolCallID:    "call-1",
		ToolName:      "exec",
		ToolNamespace: "tools",
		Content:       []protocol.ContentPart{{Type: protocol.ContentText, Text: content}},
		Timestamp:     1,
	}
	if mutate != nil {
		mutate(&msg)
	}
	return msg
}

func developer() protocol.Message {
	return protocol.Message{
		Role:      protocol.RoleDeveloper,
		Content:   []protocol.ContentPart{{Type: protocol.ContentText, Text: "developer guidance"}},
		Timestamp: 1,
	}
}

func ctxOf(messages ...protocol.Message) protocol.Context {
	return protocol.Context{Messages: messages}
}

func toolText(msg protocol.Message) string {
	if len(msg.Content) == 0 {
		return ""
	}
	return msg.Content[0].Text
}

func TestPlanCollapsesLaterExactDuplicate(t *testing.T) {
	full := "same exact tool output"
	canonical := ctxOf(toolResult(full, nil), toolResult(full, func(m *protocol.Message) { m.Timestamp = 2 }))
	orig := canonical.Messages[0].Content
	plan := Plan(canonical, testDuplicatePolicy(), PlanOptions{})
	projected := Apply(canonical, plan)

	if plan.Eligibility.State != EligibilityEligible {
		t.Fatalf("eligibility=%v", plan.Eligibility)
	}
	if len(plan.Replacements) != 1 || plan.Replacements[0].Kind != KindDuplicate || plan.Replacements[0].MessageIndex != 1 {
		t.Fatalf("replacements=%v", plan.Replacements)
	}
	if toolText(projected.Messages[0]) != full {
		t.Fatalf("earlier source mutated: %q", toolText(projected.Messages[0]))
	}
	if !strings.Contains(toolText(projected.Messages[1]), "Benes exact duplicate v1") {
		t.Fatalf("missing duplicate receipt: %q", toolText(projected.Messages[1]))
	}
	if canonical.Messages[0].Content[0].Text != full || &canonical.Messages[0].Content[0] != &orig[0] {
		t.Fatal("canonical context was mutated")
	}
	projected.Messages[0].Content[0].Text = "mutated-copy"
	if canonical.Messages[0].Content[0].Text != full {
		t.Fatal("mutating apply result leaked into caller context")
	}
}

func TestPlanRequiresStrictEqualityAfterHashBucket(t *testing.T) {
	plan := Plan(
		ctxOf(toolResult("alpha", nil), toolResult("alphb", func(m *protocol.Message) { m.Timestamp = 2 })),
		testDuplicatePolicy(),
		PlanOptions{HashContent: func(string) string { return "forced-collision" }},
	)
	if len(plan.Replacements) != 0 {
		t.Fatalf("hash equality alone must not collapse: %v", plan.Replacements)
	}
}

func TestPlanDoesNotDedupAcrossNamespaceToolOrError(t *testing.T) {
	value := "identical"
	namespace := Plan(ctxOf(toolResult(value, nil), toolResult(value, func(m *protocol.Message) {
		m.ToolNamespace = "other"
		m.Timestamp = 2
	})), testDuplicatePolicy(), PlanOptions{})
	name := Plan(ctxOf(toolResult(value, nil), toolResult(value, func(m *protocol.Message) {
		m.ToolName = "read_file"
		m.Timestamp = 2
	})), testDuplicatePolicy(), PlanOptions{})
	errState := Plan(ctxOf(toolResult(value, nil), toolResult(value, func(m *protocol.Message) {
		m.IsError = true
		m.Timestamp = 2
	})), testDuplicatePolicy(), PlanOptions{})
	if len(namespace.Replacements) != 0 || len(name.Replacements) != 0 || len(errState.Replacements) != 0 {
		t.Fatalf("cross-bucket collapse ns=%d name=%d err=%d", len(namespace.Replacements), len(name.Replacements), len(errState.Replacements))
	}
}

func TestPlanDoesNotUseLargeResultAsDuplicateSource(t *testing.T) {
	full := strings.Repeat("0123456789abcdef", 8)
	plan := Plan(ctxOf(toolResult(full, nil), toolResult(full, func(m *protocol.Message) { m.Timestamp = 2 })), testRecoveryPolicy(), PlanOptions{})
	if len(plan.Replacements) != 2 || plan.Replacements[0].Kind != KindLarge || plan.Replacements[1].Kind != KindLarge {
		t.Fatalf("kinds=%v", plan.Replacements)
	}
}

func TestLargeProjectionKeepsHeadTailAndStableReceipt(t *testing.T) {
	full := "A😀BC-" + strings.Repeat("middle-", 8) + "-XY😀Z"
	canonical := ctxOf(toolResult(full, nil))
	first := Plan(canonical, testRecoveryPolicy(), PlanOptions{})
	second := Plan(canonical, testRecoveryPolicy(), PlanOptions{})
	if len(first.Replacements) != 1 || first.Replacements[0].Kind != KindLarge {
		t.Fatalf("replacement=%v", first.Replacements)
	}
	ref := first.Replacements[0].Ref
	if !strings.HasPrefix(ref, "ctx_") || len(ref) != 4+24 {
		t.Fatalf("ref=%q", ref)
	}
	for _, ch := range ref[4:] {
		if !((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_') {
			t.Fatalf("ref charset: %q", ref)
		}
	}
	if first.Replacements[0].ProjectedContent != second.Replacements[0].ProjectedContent {
		t.Fatal("receipt is not deterministic")
	}
	content := first.Replacements[0].ProjectedContent
	if !strings.Contains(content, "A😀") || !strings.Contains(content, "😀Z") || !strings.Contains(content, "omitted text is not summarized") {
		t.Fatalf("receipt=%q", content)
	}
	if first.Replacements[0].OriginalBytes != len(full) {
		t.Fatalf("originalBytes=%d want %d", first.Replacements[0].OriginalBytes, len(full))
	}
}

func TestStableRefsIgnoreUnrelatedMessageIndex(t *testing.T) {
	full := strings.Repeat("0123456789abcdef", 8)
	without := Plan(ctxOf(toolResult(full, nil)), testRecoveryPolicy(), PlanOptions{})
	with := Plan(ctxOf(developer(), toolResult(full, nil)), testRecoveryPolicy(), PlanOptions{})
	if without.Replacements[0].Ref != with.Replacements[0].Ref {
		t.Fatalf("%q vs %q", without.Replacements[0].Ref, with.Replacements[0].Ref)
	}
}

func TestOccurrenceOrdinalDistinguishesRepeatedCalls(t *testing.T) {
	plan := Plan(ctxOf(
		toolResult(strings.Repeat("A", 64), nil),
		toolResult(strings.Repeat("B", 64), func(m *protocol.Message) { m.Timestamp = 2 }),
	), testRecoveryPolicy(), PlanOptions{})
	if len(plan.Replacements) != 2 || plan.Replacements[0].Ref == plan.Replacements[1].Ref {
		t.Fatalf("replacements=%v", plan.Replacements)
	}
}

func TestDuplicateReceiptNamesEarlierOccurrence(t *testing.T) {
	plan := Plan(ctxOf(
		toolResult(strings.Repeat("A", 64), nil),
		toolResult(strings.Repeat("B", 64), func(m *protocol.Message) { m.Timestamp = 2 }),
		toolResult(strings.Repeat("B", 64), func(m *protocol.Message) { m.Timestamp = 3 }),
	), testDuplicatePolicy(), PlanOptions{})
	var duplicate *Replacement
	for i := range plan.Replacements {
		if plan.Replacements[i].MessageIndex == 2 {
			duplicate = &plan.Replacements[i]
		}
	}
	if duplicate == nil || duplicate.Kind != KindDuplicate {
		t.Fatalf("missing duplicate: %v", plan.Replacements)
	}
	if !strings.Contains(duplicate.ProjectedContent, "source_tool: tools/exec") ||
		!strings.Contains(duplicate.ProjectedContent, "source_occurrence: 1") {
		t.Fatalf("receipt=%q", duplicate.ProjectedContent)
	}
}

func TestRecoveryLeavesErrorEncryptedNonTextAndInvalidIdentityFull(t *testing.T) {
	large := strings.Repeat("x", 128)
	plan := Plan(ctxOf(
		toolResult(large, func(m *protocol.Message) { m.IsError = true }),
		toolResult(large, func(m *protocol.Message) {
			m.ContainsEncryptedContent = true
			m.Timestamp = 2
		}),
		func() protocol.Message {
			msg := toolResult(large, func(m *protocol.Message) { m.Timestamp = 3 })
			msg.Content = []protocol.ContentPart{
				{Type: protocol.ContentText, Text: large},
				{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,xx"},
			}
			return msg
		}(),
		toolResult(large, func(m *protocol.Message) {
			m.ToolCallID = " "
			m.Timestamp = 4
		}),
		toolResult(large, func(m *protocol.Message) {
			m.ToolName = ""
			m.Timestamp = 5
		}),
	), testRecoveryPolicy(), PlanOptions{})
	if len(plan.Replacements) != 0 || plan.Metrics.LargeCandidates != 0 {
		t.Fatalf("replacements=%d largeCandidates=%d", len(plan.Replacements), plan.Metrics.LargeCandidates)
	}
}

func TestPlannerScanBudgetFailsOpen(t *testing.T) {
	policy := testRecoveryPolicy()
	policy.MaxScannedUTF8Bytes = 32
	plan := Plan(ctxOf(
		toolResult(strings.Repeat("a", 24), nil),
		toolResult(strings.Repeat("b", 24), func(m *protocol.Message) { m.Timestamp = 2 }),
	), policy, PlanOptions{})
	if plan.Eligibility.State != EligibilitySuspended || plan.Eligibility.Reason != ReasonPlannerScanLimit {
		t.Fatalf("eligibility=%v", plan.Eligibility)
	}
	if len(plan.Replacements) != 0 {
		t.Fatalf("partial replacements=%v", plan.Replacements)
	}
}

func TestAbortedContextDoesNotProject(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan := Plan(ctxOf(toolResult(strings.Repeat("a", 128), nil)), testRecoveryPolicy(), PlanOptions{Ctx: ctx})
	if plan.Eligibility.State != EligibilitySuspended || plan.Eligibility.Reason != ReasonAborted {
		t.Fatalf("eligibility=%v", plan.Eligibility)
	}
	if len(plan.Replacements) != 0 {
		t.Fatalf("replacements=%v", plan.Replacements)
	}
}

func TestOneLineMegabyteResult(t *testing.T) {
	full := strings.Repeat("q", 1024*1024)
	canonical := ctxOf(toolResult(full, nil))
	plan := Plan(canonical, testRecoveryPolicy(), PlanOptions{})
	projected := Apply(canonical, plan)
	if len(plan.Replacements) != 1 {
		t.Fatalf("replacements=%d", len(plan.Replacements))
	}
	if toolText(canonical.Messages[0]) != full {
		t.Fatal("canonical mutated")
	}
	if toolText(projected.Messages[0]) == full || !strings.Contains(toolText(projected.Messages[0]), "full_lines: 1") {
		t.Fatalf("projected=%q", toolText(projected.Messages[0])[:80])
	}
}

func TestTenMegabyteOneLineResult(t *testing.T) {
	full := strings.Repeat("q", 10*1024*1024)
	canonical := ctxOf(toolResult(full, nil))
	plan := Plan(canonical, testRecoveryPolicy(), PlanOptions{})
	projected := Apply(canonical, plan)
	if plan.Eligibility.State != EligibilityEligible || len(plan.Replacements) != 1 {
		t.Fatalf("plan state=%v replacements=%d", plan.Eligibility.State, len(plan.Replacements))
	}
	if toolText(canonical.Messages[0]) != full {
		t.Fatal("canonical mutated")
	}
	if toolText(projected.Messages[0]) == full {
		t.Fatal("expected large projection")
	}
}

func TestProjectsSingleTextPart(t *testing.T) {
	full := strings.Repeat("Z", 32*1024)
	canonical := ctxOf(toolResult(full, nil))
	plan := Plan(canonical, testRecoveryPolicy(), PlanOptions{})
	if plan.Eligibility.State != EligibilityEligible || plan.Metrics.LargeProjected != 1 || plan.Replacements[0].Kind != KindLarge {
		t.Fatalf("plan=%v metrics=%v", plan.Replacements, plan.Metrics)
	}
	projected := Apply(canonical, plan)
	if !strings.Contains(toolText(projected.Messages[0]), "[Benes context projection v1]") {
		t.Fatalf("projected=%q", toolText(projected.Messages[0]))
	}
	if toolText(canonical.Messages[0]) != full {
		t.Fatal("canonical mutated")
	}
}

func TestLeavesMixedImageAndTextUnprojected(t *testing.T) {
	full := strings.Repeat("Z", 32*1024)
	msg := toolResult(full, nil)
	msg.Content = []protocol.ContentPart{
		{Type: protocol.ContentText, Text: full},
		{Type: protocol.ContentImage, ImageURL: "data:image/png;base64,xx"},
	}
	plan := Plan(ctxOf(msg), testRecoveryPolicy(), PlanOptions{})
	if len(plan.Replacements) != 0 || plan.Metrics.LargeProjected != 0 {
		t.Fatalf("replacements=%d largeProjected=%d", len(plan.Replacements), plan.Metrics.LargeProjected)
	}
}

func TestIgnoresEmptyAndPlaceholderResults(t *testing.T) {
	empty := Plan(ctxOf(toolResult("", nil)), testRecoveryPolicy(), PlanOptions{})
	live := testRecoveryPolicy()
	live.LargeMinBytes = 96 * 1024
	placeholder := Plan(ctxOf(toolResult("[file: inventory.log]", nil)), live, PlanOptions{})
	if len(empty.Replacements) != 0 || len(placeholder.Replacements) != 0 {
		t.Fatalf("empty=%d placeholder=%d", len(empty.Replacements), len(placeholder.Replacements))
	}
	if empty.Metrics.LargeCandidates != 0 || placeholder.Metrics.LargeCandidates != 0 {
		t.Fatalf("largeCandidates empty=%d placeholder=%d", empty.Metrics.LargeCandidates, placeholder.Metrics.LargeCandidates)
	}
}

func TestErrorResultsNeverLargeProject(t *testing.T) {
	full := strings.Repeat("e", 128)
	plan := Plan(ctxOf(toolResult(full, func(m *protocol.Message) { m.IsError = true })), testRecoveryPolicy(), PlanOptions{})
	if plan.Metrics.LargeCandidates != 0 || len(plan.Replacements) != 0 {
		t.Fatalf("error was large-projected: %+v", plan)
	}
}

func TestApplyAlwaysReturnsIndependentCopy(t *testing.T) {
	canonical := ctxOf(toolResult("tiny", nil))
	plan := Plan(canonical, testDuplicatePolicy(), PlanOptions{})
	projected := Apply(canonical, plan)
	if len(projected.Messages) != 1 {
		t.Fatal("missing messages")
	}
	projected.Messages[0].Content[0].Text = "changed"
	if canonical.Messages[0].Content[0].Text != "tiny" {
		t.Fatal("apply did not deep-copy")
	}
}
