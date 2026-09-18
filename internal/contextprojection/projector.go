package contextprojection

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/protocol"
)

const hashChunkBytes = 64 * 1024

type textMetadata struct {
	utf8ByteLength int
	lineCount      int
}

type scanState struct {
	bytes     int
	hashBytes int
}

type fullDuplicateSource struct {
	artifact Artifact
	content  string
}

func projectableToolResultText(message protocol.Message) (string, bool) {
	if len(message.Content) != 1 {
		return "", false
	}
	part := message.Content[0]
	if part.Type != protocol.ContentText {
		return "", false
	}
	return part.Text, true
}

func aborted(ctx context.Context) bool {
	return ctx != nil && ctx.Err() != nil
}

func scanTextMetadata(text string, scan *scanState, maxBytes int, ctx context.Context) (textMetadata, SuspensionReason, bool) {
	utf8ByteLength := 0
	lineCount := 1
	sinceCheck := 0
	for index := 0; index < len(text); {
		if sinceCheck >= 0x4000 {
			if aborted(ctx) {
				return textMetadata{}, ReasonAborted, false
			}
			sinceCheck = 0
		}
		_, size := utf8.DecodeRuneInString(text[index:])
		if scan.bytes+size > maxBytes {
			return textMetadata{}, ReasonPlannerScanLimit, false
		}
		scan.bytes += size
		utf8ByteLength += size
		if text[index] == '\n' {
			lineCount++
		}
		index += size
		sinceCheck += size
	}
	return textMetadata{utf8ByteLength: utf8ByteLength, lineCount: lineCount}, "", true
}

func hashWithBounds(
	content string,
	contentBytes int,
	scan *scanState,
	maxBytes int,
	ctx context.Context,
	injected func(string) string,
) (string, SuspensionReason, bool) {
	if aborted(ctx) {
		return "", ReasonAborted, false
	}
	if scan.bytes+contentBytes > maxBytes {
		return "", ReasonPlannerScanLimit, false
	}
	if injected != nil {
		digest := injected(content)
		scan.bytes += contentBytes
		scan.hashBytes += contentBytes
		return digest, "", true
	}
	hash := sha256.New()
	for offset := 0; offset < len(content); offset += hashChunkBytes {
		if aborted(ctx) {
			return "", ReasonAborted, false
		}
		end := offset + hashChunkBytes
		if end > len(content) {
			end = len(content)
		}
		_, _ = hash.Write([]byte(content[offset:end]))
	}
	scan.bytes += contentBytes
	scan.hashBytes += contentBytes
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil)), "", true
}

func prefixWithinUTF8Bytes(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	used := 0
	end := 0
	for end < len(text) {
		_, size := utf8.DecodeRuneInString(text[end:])
		if used+size > maxBytes {
			break
		}
		used += size
		end += size
	}
	return text[:end]
}

func suffixWithinUTF8Bytes(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	used := 0
	start := len(text)
	for start > 0 {
		_, size := utf8.DecodeLastRuneInString(text[:start])
		if used+size > maxBytes {
			break
		}
		used += size
		start -= size
	}
	return text[start:]
}

func duplicateReceipt(source Artifact) string {
	sourceTool := source.Identity.ToolName
	if source.Identity.ToolNamespace != "" {
		sourceTool = source.Identity.ToolNamespace + "/" + source.Identity.ToolName
	}
	return strings.Join([]string{
		"[Benes exact duplicate v1]",
		"This tool result is exactly identical to the earlier full result for call " + source.Identity.ToolCallID + ".",
		"source_tool: " + sourceTool,
		"source_occurrence: " + strconv.Itoa(source.Identity.OccurrenceOrdinal),
		"The full source remains present earlier in active context.",
	}, "\n")
}

func largeReceipt(artifact Artifact, policy Policy) string {
	head := prefixWithinUTF8Bytes(artifact.Content, policy.PreviewHeadBytes)
	tail := suffixWithinUTF8Bytes(artifact.Content, policy.PreviewTailBytes)
	return strings.Join([]string{
		"[Benes context projection v1]",
		"ref: " + artifact.Ref,
		"full_utf8_bytes: " + strconv.Itoa(artifact.UTF8ByteLength),
		"full_lines: " + strconv.Itoa(artifact.LineCount),
		"preview: exact beginning and ending fragments only",
		"omitted text is not summarized; do not infer its contents",
		"use " + RecoveryToolName + " only if exact omitted evidence is required",
		"",
		head,
		"",
		"[... exact middle omitted ...]",
		"",
		tail,
	}, "\n")
}

func duplicateBucketKey(namespace, toolName string, isError bool, digest string) string {
	return strings.Join([]string{namespace, toolName, strconv.FormatBool(isError), digest}, "\x00")
}

func suspendedPlan(policy Policy, metrics Metrics, scan scanState, started time.Time, reason SuspensionReason) PlanResult {
	metrics.PlanningMs = time.Since(started).Seconds() * 1000
	metrics.PlannerScannedBytes = scan.bytes
	metrics.HashScannedBytes = scan.hashBytes
	metrics.SuspensionReason = reason
	return PlanResult{
		Registry:     NewRegistry(),
		Replacements: nil,
		Metrics:      metrics,
		Eligibility:  Eligibility{State: EligibilitySuspended, Reason: reason},
	}
}

func Plan(canonical protocol.Context, policy Policy, options PlanOptions) PlanResult {
	started := time.Now()
	metrics := emptyMetrics(policy)
	scan := scanState{}
	registry := NewRegistry()
	replacements := make([]Replacement, 0)
	occurrenceCounts := map[string]int{}
	fullDuplicateSources := map[string][]fullDuplicateSource{}
	ctx := options.Ctx

	if aborted(ctx) {
		return suspendedPlan(policy, metrics, scan, started, ReasonAborted)
	}

	for messageIndex, message := range canonical.Messages {
		if aborted(ctx) {
			return suspendedPlan(policy, metrics, scan, started, ReasonAborted)
		}
		if message.Role != protocol.RoleToolResult {
			continue
		}
		if !ValidIdentityInput(message.ToolCallID, message.ToolName) {
			continue
		}

		identityKey := IdentityKey(message.ToolCallID, message.ToolNamespace, message.ToolName)
		occurrenceOrdinal := occurrenceCounts[identityKey]
		occurrenceCounts[identityKey] = occurrenceOrdinal + 1

		if message.ContainsEncryptedContent {
			continue
		}
		projectable, ok := projectableToolResultText(message)
		if !ok {
			continue
		}

		metadata, reason, ok := scanTextMetadata(projectable, &scan, policy.MaxScannedUTF8Bytes, ctx)
		if !ok {
			return suspendedPlan(policy, metrics, scan, started, reason)
		}
		metrics.Candidates++

		identity := Identity{
			ToolCallID:        message.ToolCallID,
			ToolNamespace:     message.ToolNamespace,
			ToolName:          message.ToolName,
			OccurrenceOrdinal: occurrenceOrdinal,
		}

		var digest string
		var duplicateKey string
		var duplicateSource *Artifact
		duplicateEligible := metadata.utf8ByteLength >= policy.DuplicateMinBytes

		if duplicateEligible {
			metrics.DuplicateCandidates++
			hashed, reason, ok := hashWithBounds(
				projectable,
				metadata.utf8ByteLength,
				&scan,
				policy.MaxScannedUTF8Bytes,
				ctx,
				options.HashContent,
			)
			if !ok {
				return suspendedPlan(policy, metrics, scan, started, reason)
			}
			digest = hashed
			duplicateKey = duplicateBucketKey(message.ToolNamespace, message.ToolName, message.IsError, digest)
			for _, source := range fullDuplicateSources[duplicateKey] {
				if source.content == projectable {
					artifact := source.artifact
					duplicateSource = &artifact
					break
				}
			}
		}

		if duplicateSource != nil {
			projectedContent := duplicateReceipt(*duplicateSource)
			projectedBytes := len(projectedContent)
			replacements = append(replacements, Replacement{
				MessageIndex:     messageIndex,
				Kind:             KindDuplicate,
				ProjectedContent: projectedContent,
				OriginalBytes:    metadata.utf8ByteLength,
				ProjectedBytes:   projectedBytes,
			})
			metrics.DuplicateProjected++
			metrics.OriginalBytes += metadata.utf8ByteLength
			metrics.ProjectedBytes += projectedBytes
			continue
		}

		largeEligible := policy.Variant == VariantRecoveryV1 &&
			!message.IsError &&
			metadata.utf8ByteLength >= policy.LargeMinBytes

		if largeEligible {
			metrics.LargeCandidates++
			artifact := registry.Register(
				identity,
				messageIndex,
				projectable,
				metadata.utf8ByteLength,
				metadata.lineCount,
				message.IsError,
			)
			projectedContent := largeReceipt(artifact, policy)
			projectedBytes := len(projectedContent)
			replacements = append(replacements, Replacement{
				MessageIndex:     messageIndex,
				Kind:             KindLarge,
				Ref:              artifact.Ref,
				ProjectedContent: projectedContent,
				OriginalBytes:    metadata.utf8ByteLength,
				ProjectedBytes:   projectedBytes,
			})
			metrics.LargeProjected++
			metrics.OriginalBytes += metadata.utf8ByteLength
			metrics.ProjectedBytes += projectedBytes
			continue
		}

		if duplicateEligible && duplicateKey != "" {
			artifact := registry.Register(
				identity,
				messageIndex,
				projectable,
				metadata.utf8ByteLength,
				metadata.lineCount,
				message.IsError,
			)
			fullDuplicateSources[duplicateKey] = append(fullDuplicateSources[duplicateKey], fullDuplicateSource{
				artifact: artifact,
				content:  projectable,
			})
		}
	}

	metrics.PlanningMs = time.Since(started).Seconds() * 1000
	metrics.PlannerScannedBytes = scan.bytes
	metrics.HashScannedBytes = scan.hashBytes
	return PlanResult{
		Registry:     registry,
		Replacements: replacements,
		Metrics:      metrics,
		Eligibility:  Eligibility{State: EligibilityEligible},
	}
}

func Apply(canonical protocol.Context, plan PlanResult) protocol.Context {
	out := cloneContext(canonical)
	if len(plan.Replacements) == 0 {
		return out
	}
	byIndex := make(map[int]Replacement, len(plan.Replacements))
	for _, replacement := range plan.Replacements {
		byIndex[replacement.MessageIndex] = replacement
	}
	for index := range out.Messages {
		replacement, ok := byIndex[index]
		if !ok || out.Messages[index].Role != protocol.RoleToolResult {
			continue
		}
		msg := &out.Messages[index]
		if len(msg.Content) == 1 && msg.Content[0].Type == protocol.ContentText {
			msg.Content[0].Text = replacement.ProjectedContent
			continue
		}
		msg.Content = []protocol.ContentPart{{
			Type: protocol.ContentText,
			Text: replacement.ProjectedContent,
		}}
	}
	return out
}

func ObserveShadow(canonical protocol.Context, options PlanOptions) Metrics {
	plan := Plan(canonical, ShadowPolicy(), options)
	metrics := plan.Metrics
	metrics.Variant = MetricsShadow
	return metrics
}

func cloneContext(in protocol.Context) protocol.Context {
	out := protocol.Context{
		SystemPrompt: append([]string(nil), in.SystemPrompt...),
		Messages:     make([]protocol.Message, len(in.Messages)),
		Tools:        make([]protocol.Tool, len(in.Tools)),
	}
	for i, message := range in.Messages {
		out.Messages[i] = cloneMessage(message)
	}
	for i, tool := range in.Tools {
		cloned := tool
		cloned.Parameters = cloneAnyMap(tool.Parameters)
		if tool.Strict != nil {
			value := *tool.Strict
			cloned.Strict = &value
		}
		out.Tools[i] = cloned
	}
	return out
}

func cloneMessage(in protocol.Message) protocol.Message {
	out := in
	if in.Phase != nil {
		phase := *in.Phase
		out.Phase = &phase
	}
	out.Content = make([]protocol.ContentPart, len(in.Content))
	for i, part := range in.Content {
		out.Content[i] = cloneContentPart(part)
	}
	return out
}

func cloneContentPart(in protocol.ContentPart) protocol.ContentPart {
	out := in
	out.Redacted = append([]string(nil), in.Redacted...)
	out.Arguments = cloneAnyMap(in.Arguments)
	out.ProviderMetadata = protocol.CloneProviderOpaqueMetadata(in.ProviderMetadata)
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneAny(item)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}
