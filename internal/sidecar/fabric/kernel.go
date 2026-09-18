// Package fabric is a local-first hash-chained task kernel.
//
// It is an opt-in sidecar: callers supply the state directory. This package
// does not start with the default listener, does not talk to Codex JSON-RPC
// or the Claude SDK, and does not implement A2A remote-primary clients.
//
// Public HTTP lifecycle (create/start/close/delete) records operator marks.
// Start is EventRunStarted; close is EventRunCompleted and is terminal task
// closure. Neither launches a model, worker, or subprocess. HTTP start/close
// actors are audit attribution only: they must not become CurrentOwner,
// permissions, or fencing identity. StartRun still records a runtime session.
//
// Distinct from lifecycle marks, BeginExecute starts a fenced primary run that
// a server-owned data-plane adapter may drive. Opt-in single-child handoff uses
// ChildRun* events and CAS CommitHandoffFrom; this package still never shells
// out and never dials providers.
package fabric

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const SchemaVersion = "1.0.0"

const (
	EventTaskCreated           = "TaskCreated"
	EventTaskCompleted         = "TaskCompleted"
	EventTaskCancelled         = "TaskCancelled"
	EventTaskRemoved           = "TaskRemoved"
	EventRunStarted            = "RunStarted"
	EventRunCompleted          = "RunCompleted"
	EventRunFailed             = "RunFailed"
	EventRunInterrupted        = "RunInterrupted"
	EventRunCancelled          = "RunCancelled"
	EventRunProgress           = "RunProgress"
	EventChildRunStarted       = "ChildRunStarted"
	EventChildRunCompleted     = "ChildRunCompleted"
	EventChildRunFailed        = "ChildRunFailed"
	EventChildRunCancelled     = "ChildRunCancelled"
	EventChildRunInterrupted   = "ChildRunInterrupted"
	EventApprovalRequested     = "ApprovalRequested"
	EventApprovalResolved      = "ApprovalResolved"
	EventClaimValidated        = "ClaimValidated"
	EventHandoffCommitted      = "HandoffCommitted"
	EventHandoffRolledBack     = "HandoffRolledBack"
	EventHandoffProposed       = "HandoffProposed"
	EventHandoffFailed         = "HandoffFailed"
	EventLeaseLost             = "LeaseLost"
	EventInitialInputReserved  = "InitialInputReserved"
	EventInitialInputSent      = "InitialInputSent"
	EventInitialInputUncertain = "InitialInputUncertain"
	EventInitialInputAbandoned = "InitialInputAbandoned"
	EventWorkspaceRegistered   = "WorkspaceRegistered"
	EventSessionStarted        = "SessionStarted"
	EventSessionClosed         = "SessionClosed"
)

const (
	maxTitleLen = 120
	maxGoalLen  = 200
	maxFieldLen = 500
	maxOwnerLen = 80
)

// Event is the append-only envelope. event_hash is computed and is excluded
// from its own canonical encoding.
type Event struct {
	TaskID            string          `json:"task_id"`
	Sequence          uint64          `json:"sequence"`
	EventID           string          `json:"event_id"`
	EventType         string          `json:"event_type"`
	SchemaVersion     string          `json:"schema_version"`
	OccurredAt        int64           `json:"occurred_at"`
	ActorType         string          `json:"actor_type"`
	ActorID           string          `json:"actor_id"`
	RuntimeSessionID  string          `json:"runtime_session_id"`
	Payload           json.RawMessage `json:"payload"`
	PreviousEventHash string          `json:"previous_event_hash"`
	EventHash         string          `json:"event_hash,omitempty"`
}

var (
	secretRe = regexp.MustCompile(`(?i)(sk-[A-Za-z0-9]{20,}|ocx_admin_[A-Za-z0-9_-]{20,}|Bearer\s+[A-Za-z0-9._-]{20,}|xox[bpoa]-[A-Za-z0-9-]{10,})`)
	homeRe   = regexp.MustCompile(`[A-Za-z]:\\Users\\[^\\]+|/home/[^/]+|/Users/[^/]+`)
)

var deniedPayloadKeys = map[string]struct{}{
	"prompt":          {},
	"prompts":         {},
	"secret":          {},
	"secrets":         {},
	"api_key":         {},
	"apikey":          {},
	"authorization":   {},
	"password":        {},
	"transcript":      {},
	"transcripts":     {},
	"tool_transcript": {},
	"access_token":    {},
	"refresh_token":   {},
	"id_token":        {},
	"bearer":          {},
	"input_text":      {},
	"full_input":      {},
	"tool_result":     {},
	"messages":        {},
	"instruction":     {},
	"instructions":    {},
	"child_output":    {},
	"output_text":     {},
	"subagent_result": {},
}

func canonicalBytes(ev Event) ([]byte, error) {
	cp := ev
	cp.EventHash = ""
	b, err := json.Marshal(cp)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if m, ok := v.(map[string]any); ok {
		delete(m, "event_hash")
	}
	return marshalCanonical(v)
}

func marshalCanonical(v any) ([]byte, error) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			kb, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			vb, err := marshalCanonical(t[k])
			if err != nil {
				return nil, err
			}
			b.Write(kb)
			b.WriteByte(':')
			b.Write(vb)
		}
		b.WriteByte('}')
		return []byte(b.String()), nil
	case []any:
		var b strings.Builder
		b.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			ib, err := marshalCanonical(item)
			if err != nil {
				return nil, err
			}
			b.Write(ib)
		}
		b.WriteByte(']')
		return []byte(b.String()), nil
	case json.Number:
		return []byte(t.String()), nil
	default:
		return json.Marshal(t)
	}
}

// HashEvent computes the sha256 of the canonical encoding with event_hash excluded.
func HashEvent(ev Event) (string, error) {
	cb, err := canonicalBytes(ev)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(cb)
	return hex.EncodeToString(sum[:]), nil
}

func sanitizePayload(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return json.RawMessage("null")
	}
	cleaned := sanitizeValue(v, "")
	b, err := json.Marshal(cleaned)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

func sanitizeValue(v any, key string) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			lk := strings.ToLower(strings.ReplaceAll(k, "-", "_"))
			if _, denied := deniedPayloadKeys[lk]; denied {
				continue
			}
			if lk == "token" || lk == "tokens" {
				switch child.(type) {
				case float64, json.Number, int, int64:
					out[k] = child
				}
				continue
			}
			if cleaned := sanitizeValue(child, lk); cleaned != nil {
				out[k] = cleaned
			}
		}
		return out
	case []any:
		if len(t) > 32 {
			t = t[:32]
		}
		out := make([]any, 0, len(t))
		for _, item := range t {
			if cleaned := sanitizeValue(item, key); cleaned != nil {
				out = append(out, cleaned)
			}
		}
		return out
	case string:
		return sanitizeString(t, key)
	case float64, json.Number, bool, nil, int, int64:
		return t
	default:
		return nil
	}
}

func sanitizeString(s, key string) any {
	if key == "goal" && looksLikePrompt(s) {
		return nil
	}
	s = secretRe.ReplaceAllString(s, "[redacted]")
	s = homeRe.ReplaceAllString(s, "~")
	limit := maxFieldLen
	if key == "title" {
		limit = maxTitleLen
	}
	if key == "goal" {
		limit = maxGoalLen
	}
	if len(s) > limit {
		s = s[:limit]
	}
	return s
}

func looksLikePrompt(s string) bool {
	if len(s) > maxGoalLen {
		return true
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "you are ") || strings.Contains(lower, "system:") {
		return true
	}
	return secretRe.MatchString(s)
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func validateIdentity(value, field string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !utf8.ValidString(value) {
		return "", InvalidTask(field + " is not valid UTF-8")
	}
	if utf8.RuneCountInString(value) > maxOwnerLen {
		return "", InvalidTask(field + " exceeds 80 characters")
	}
	if secretRe.MatchString(value) || homeRe.MatchString(value) {
		return "", InvalidTask(field + " is not allowed")
	}
	return value, nil
}

func payloadMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return map[string]any{}
	}
	return m
}

func payloadString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func validTaskID(id string) error {
	if id == "" || len(id) > 80 {
		return fmt.Errorf("invalid task id")
	}
	for _, r := range id {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if !ok {
			return fmt.Errorf("invalid task id")
		}
	}
	return nil
}
