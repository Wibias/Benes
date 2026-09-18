package compaction

import (
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	EnvelopePrefix = "benes1:"

	// CompactPrompt mirrors codex-rs core/templates/compact/prompt.md.
	CompactPrompt = `You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task.

Include:
- Current progress and key decisions made
- Important context, constraints, or user preferences
- What remains to be done (clear next steps)
- Any critical data, examples, or references needed to continue

Be concise, structured, and focused on helping the next LLM seamlessly continue the work.`

	// SummaryPrefix mirrors codex-rs core/templates/compact/summary_prefix.md.
	SummaryPrefix = "Another language model started to solve this problem and produced a summary of its thinking process. You also have access to the state of the tools that were used by that language model. Use this to build on the work that has already been done and avoid duplicating work. Here is the summary produced by the other language model, use the information in this summary to assist with your own analysis:"

	OpaqueNote = "[earlier conversation was compacted; the summary is stored in a format this model cannot read]"

	// CompactV1RetainedCharBudget approximates codex-rs COMPACT_USER_MESSAGE_MAX_TOKENS = 20k tokens at ~4 chars/token.
	CompactV1RetainedCharBudget = 20_000 * 4

	CompactResponseMaxBytes = 32 << 20
)

var ErrCompactResponseTooLarge = errors.New("compact response exceeded 32 MiB")

func EncodeSummary(summary string) string {
	return EnvelopePrefix + base64.StdEncoding.EncodeToString([]byte(summary))
}

func DecodeSummary(encrypted string) (string, bool) {
	if !strings.HasPrefix(encrypted, EnvelopePrefix) {
		return "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(encrypted[len(EnvelopePrefix):])
	if err != nil {
		return "", false
	}
	if !utf8.Valid(decoded) {
		return "", false
	}
	return string(decoded), true
}

func ItemToText(encrypted string) string {
	if decoded, ok := DecodeSummary(encrypted); ok {
		return SummaryPrefix + "\n\n" + decoded
	}
	return OpaqueNote
}
