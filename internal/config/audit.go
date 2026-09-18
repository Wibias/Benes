package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

const (
	SourceCLI         = "cli"
	SourceAPI         = "api"
	SourceGUI         = "gui"
	SourceInternal    = "internal"
	SourceIntegration = "integration"

	maxMutationRecords  = 200
	maxMutationLogBytes = 256 << 10
	maxAuditValueBytes  = 1024
	redactedSentinel    = `"[redacted]"`
)

type MutationSource struct {
	Class  string `json:"class"`
	Detail string `json:"detail,omitempty"`
}

type MutationChange struct {
	Path   string          `json:"path"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

type MutationRecord struct {
	Timestamp string           `json:"timestamp"`
	Source    MutationSource   `json:"source"`
	Revision  Revision         `json:"revision,omitempty"`
	Changes   []MutationChange `json:"changes"`
}

var (
	secretKeyPattern   = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|id[_-]?token|client[_-]?secret|authorization|password|secret|credential|cookie)`)
	secretValuePattern = regexp.MustCompile(`(?i)(\bsk-[A-Za-z0-9_-]{8,}|Bearer\s+\S+|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}|xox[baprs]-|ghp_[A-Za-z0-9]{20,})`)
)

func (tx *Transaction) SetSource(source MutationSource) {
	if tx == nil {
		return
	}
	tx.source = source
}

func MutationLogPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "config-mutations.json")
}

func ListMutations(configPath string, limit int) ([]MutationRecord, error) {
	records, err := readMutationLog(MutationLogPath(configPath))
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > maxMutationRecords {
		limit = maxMutationRecords
	}
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func (tx *Transaction) persistAudit(next map[string]json.RawMessage, revision Revision) error {
	if tx == nil || tx.store == nil {
		return fmt.Errorf("config transaction is nil")
	}
	record := tx.buildRecord(next, revision)
	if record == nil {
		return nil
	}
	return appendMutationRecord(MutationLogPath(tx.store.path), *record)
}

func (tx *Transaction) buildRecord(next map[string]json.RawMessage, revision Revision) *MutationRecord {
	source := tx.source
	if strings.TrimSpace(source.Class) == "" {
		source.Class = SourceInternal
	}
	source.Class = boundAuditString(source.Class, 32)
	source.Detail = boundAuditString(source.Detail, 128)
	changes := make([]MutationChange, 0, len(tx.mutations))
	seen := map[string]struct{}{}
	for _, mutation := range tx.mutations {
		path := formatPath(mutation.path)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		before, beforeOK, _ := lookupRaw(tx.baseline.root, mutation.path)
		after, afterOK, _ := lookupRaw(next, mutation.path)
		if sameJSONValue(before, beforeOK, after, afterOK) {
			continue
		}
		change := MutationChange{Path: path}
		if beforeOK {
			change.Before = redactAuditValue(before)
		}
		if afterOK {
			change.After = redactAuditValue(after)
		}
		changes = append(changes, change)
	}
	if len(changes) == 0 {
		return nil
	}
	return &MutationRecord{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Source:    source,
		Revision:  revision,
		Changes:   changes,
	}
}

func appendMutationRecord(path string, record MutationRecord) error {
	records, err := readMutationLog(path)
	if err != nil {
		return err
	}
	records = append([]MutationRecord{record}, records...)
	if len(records) > maxMutationRecords {
		records = records[:maxMutationRecords]
	}
	encoded, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config mutation log: %w", err)
	}
	encoded = append(encoded, '\n')
	for int64(len(encoded)) > maxMutationLogBytes && len(records) > 1 {
		records = records[:len(records)-1]
		encoded, err = json.MarshalIndent(records, "", "  ")
		if err != nil {
			return fmt.Errorf("encode config mutation log: %w", err)
		}
		encoded = append(encoded, '\n')
	}
	if int64(len(encoded)) > maxMutationLogBytes {
		return fmt.Errorf("config mutation log exceeds byte bound")
	}
	return atomicfile.Write(path, encoded, atomicfile.Options{Mode: 0o600})
}

func readMutationLog(path string) ([]MutationRecord, error) {
	data, err := atomicfile.ReadBounded(path, maxMutationLogBytes+1)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read config mutation log: %w", err)
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var records []MutationRecord
	if err := json.Unmarshal(trimmed, &records); err != nil {
		return nil, fmt.Errorf("decode config mutation log: %w", err)
	}
	return records, nil
}

func redactAuditValue(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return json.RawMessage(redactedSentinel)
	}
	redacted := redactJSON(value, false)
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return json.RawMessage(redactedSentinel)
	}
	if len(encoded) > maxAuditValueBytes {
		return json.RawMessage(`"[truncated]"`)
	}
	return encoded
}

func redactJSON(value any, secret bool) any {
	if secret {
		return "[redacted]"
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[key] = redactJSON(child, secretKeyPattern.MatchString(key))
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = redactJSON(child, false)
		}
		return out
	case string:
		if secretValuePattern.MatchString(typed) {
			return "[redacted]"
		}
		return typed
	default:
		return value
	}
}

func boundAuditString(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}

func compactJSONEqual(a, b map[string]json.RawMessage) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(left, right)
}
