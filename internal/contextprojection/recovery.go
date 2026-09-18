package contextprojection

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/Wibias/Benes/internal/protocol"
)

const (
	RecoveryToolName = "__benes_context_v1"

	MaxReadBytesPerCall           = 128 * 1024
	MaxReadLinesPerCall           = 2000
	MaxGrepOutputBytes            = 64 * 1024
	MaxGrepMatches                = 100
	MaxGrepQueryChars             = 1024
	MaxCallsPerTurn               = 8
	MaxReturnedBytesPerTurn       = 512 * 1024
	MaxInternalModelRoundsPerTurn = 4
	MaxScanBytesPerCall           = 32 * 1024 * 1024
	MaxScanBytesPerTurn           = 128 * 1024 * 1024
	MaxFailOpenRestartsPerTurn    = 1

	grepChunkBytes = 64 * 1024
)

type RecoveryError string

const (
	ErrNotFound     RecoveryError = "not_found"
	ErrStaleCursor  RecoveryError = "stale_cursor"
	ErrInvalidRange RecoveryError = "invalid_range"
	ErrLineLimit    RecoveryError = "line_limit"
	ErrQueryTooLong RecoveryError = "query_too_long"
	ErrEmptyQuery   RecoveryError = "empty_query"
	ErrCallLimit    RecoveryError = "call_limit"
	ErrByteLimit    RecoveryError = "byte_limit"
	ErrScanLimit    RecoveryError = "scan_limit"
	ErrAborted      RecoveryError = "aborted"
)

type ReadResult struct {
	OK         bool
	Error      RecoveryError
	Content    string
	NextCursor string
	EOF        bool
}

type GrepMatch struct {
	Line    int    `json:"line"`
	Preview string `json:"preview"`
	Cursor  string `json:"cursor"`
}

type GrepResult struct {
	OK        bool
	Error     RecoveryError
	Matches   []GrepMatch
	Truncated bool
}

type ReadOptions struct {
	StartLine int
	EndLine   int
	Cursor    string
	MaxBytes  int
	HasLines  bool
}

type cursorState struct {
	ref       string
	offset    int
	endOffset int
}

type RecoverySession struct {
	registry      *Registry
	ctx           context.Context
	cursors       map[string]cursorState
	calls         int
	returnedBytes int
	scannedBytes  int
}

func NewRecoverySession(registry *Registry, ctx context.Context) *RecoverySession {
	return &RecoverySession{
		registry: registry,
		ctx:      ctx,
		cursors:  make(map[string]cursorState),
	}
}

func (s *RecoverySession) Calls() int         { return s.calls }
func (s *RecoverySession) ReturnedBytes() int { return s.returnedBytes }
func (s *RecoverySession) ScannedBytes() int  { return s.scannedBytes }

func RecoveryTool() protocol.Tool {
	return protocol.Tool{
		Name: RecoveryToolName,
		Description: "Read exact text omitted by a Benes context-projection receipt. " +
			"Use only refs that appear in such receipts.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"op":         map[string]any{"type": "string", "enum": []any{"read", "grep"}},
				"ref":        map[string]any{"type": "string"},
				"start_line": map[string]any{"type": "integer", "minimum": 1},
				"end_line":   map[string]any{"type": "integer", "minimum": 1},
				"cursor":     map[string]any{"type": "string"},
				"max_bytes":  map[string]any{"type": "integer", "minimum": 1},
				"query":      map[string]any{"type": "string"},
			},
			"required":             []any{"op", "ref"},
			"additionalProperties": false,
		},
	}
}

func AddRecoveryTool(tools []protocol.Tool) ([]protocol.Tool, error) {
	source := tools
	if source == nil {
		source = []protocol.Tool{}
	}
	for _, tool := range source {
		if recoveryToolCollision(tool) {
			return nil, errToolNameCollision
		}
	}
	out := make([]protocol.Tool, len(source)+1)
	copy(out, source)
	out[len(source)] = RecoveryTool()
	return out, nil
}

type collisionError string

func (e collisionError) Error() string { return string(e) }

const errToolNameCollision collisionError = "tool_name_collision"

func recoveryToolCollision(tool protocol.Tool) bool {
	if tool.Name == RecoveryToolName {
		return true
	}
	if tool.Namespace == "" {
		return false
	}
	logical := tool.Namespace + "__" + tool.Name
	slash := tool.Namespace + "/" + tool.Name
	return logical == RecoveryToolName || slash == RecoveryToolName
}

func (s *RecoverySession) beginCall() RecoveryError {
	if aborted(s.ctx) {
		return ErrAborted
	}
	if s.calls >= MaxCallsPerTurn {
		return ErrCallLimit
	}
	if s.returnedBytes >= MaxReturnedBytesPerTurn {
		return ErrByteLimit
	}
	s.calls++
	return ""
}

func (s *RecoverySession) scanUnit(bytes int, callBytes *int) RecoveryError {
	if aborted(s.ctx) {
		return ErrAborted
	}
	if *callBytes+bytes > MaxScanBytesPerCall {
		return ErrScanLimit
	}
	if s.scannedBytes+bytes > MaxScanBytesPerTurn {
		return ErrScanLimit
	}
	*callBytes += bytes
	s.scannedBytes += bytes
	return ""
}

func (s *RecoverySession) newCursor(state cursorState) string {
	buf := make([]byte, 12)
	for {
		if _, err := rand.Read(buf); err != nil {
			panic("contextprojection: crypto/rand unavailable")
		}
		cursor := "cur_" + base64.RawURLEncoding.EncodeToString(buf)
		if _, exists := s.cursors[cursor]; exists {
			continue
		}
		s.cursors[cursor] = state
		return cursor
	}
}

func (s *RecoverySession) lineRange(artifact Artifact, startLine, endLine int, callBytes *int) (start, end int, err RecoveryError) {
	if startLine < 1 || endLine < startLine {
		return 0, 0, ErrInvalidRange
	}
	if endLine-startLine+1 > MaxReadLinesPerCall {
		return 0, 0, ErrLineLimit
	}
	text := artifact.Content
	line := 1
	start = -1
	if startLine == 1 {
		start = 0
	}
	end = len(text)
	for index := 0; index < len(text); {
		_, size := utf8.DecodeRuneInString(text[index:])
		if limited := s.scanUnit(size, callBytes); limited != "" {
			return 0, 0, limited
		}
		if text[index] == '\n' {
			line++
			if line == startLine {
				start = index + 1
			}
			if line == endLine+1 {
				end = index + 1
				break
			}
		}
		index += size
	}
	if start < 0 {
		return 0, 0, ErrInvalidRange
	}
	return start, end, ""
}

type exactSlice struct {
	content    string
	nextOffset int
	bytes      int
}

func (s *RecoverySession) sliceExact(text string, start, end, maxBytes int, callBytes *int) (exactSlice, RecoveryError) {
	remainingOuter := MaxReturnedBytesPerTurn - s.returnedBytes
	if remainingOuter <= 0 {
		return exactSlice{}, ErrByteLimit
	}
	if maxBytes < 1 {
		maxBytes = 1
	}
	capBytes := maxBytes
	if capBytes > MaxReadBytesPerCall {
		capBytes = MaxReadBytesPerCall
	}
	if capBytes > remainingOuter {
		capBytes = remainingOuter
	}
	bytes := 0
	nextOffset := start
	for index := start; index < end; {
		_, size := utf8.DecodeRuneInString(text[index:])
		if bytes+size > capBytes {
			break
		}
		if limited := s.scanUnit(size, callBytes); limited != "" {
			return exactSlice{}, limited
		}
		bytes += size
		index += size
		nextOffset = index
	}
	if nextOffset == start && start < end {
		return exactSlice{}, ErrByteLimit
	}
	return exactSlice{content: text[start:nextOffset], nextOffset: nextOffset, bytes: bytes}, ""
}

func (s *RecoverySession) Read(ref string, options ReadOptions) ReadResult {
	if begin := s.beginCall(); begin != "" {
		return ReadResult{Error: begin}
	}
	artifact, ok := s.registry.Get(ref)
	if !ok {
		return ReadResult{Error: ErrNotFound}
	}
	callBytes := 0
	var start, end int
	switch {
	case options.Cursor != "":
		cursor, exists := s.cursors[options.Cursor]
		if !exists || cursor.ref != ref {
			return ReadResult{Error: ErrStaleCursor}
		}
		delete(s.cursors, options.Cursor)
		start = cursor.offset
		end = cursor.endOffset
	case options.HasLines || options.StartLine != 0 || options.EndLine != 0:
		startLine := options.StartLine
		if startLine == 0 {
			startLine = 1
		}
		endLine := options.EndLine
		if endLine == 0 {
			endLine = startLine
		}
		var err RecoveryError
		start, end, err = s.lineRange(artifact, startLine, endLine, &callBytes)
		if err != "" {
			return ReadResult{Error: err}
		}
	default:
		start = 0
		end = len(artifact.Content)
	}
	maxBytes := options.MaxBytes
	if maxBytes == 0 {
		maxBytes = MaxReadBytesPerCall
	}
	sliced, err := s.sliceExact(artifact.Content, start, end, maxBytes, &callBytes)
	if err != "" {
		return ReadResult{Error: err}
	}
	s.returnedBytes += sliced.bytes
	eof := sliced.nextOffset >= end
	result := ReadResult{OK: true, Content: sliced.content, EOF: eof}
	if !eof {
		result.NextCursor = s.newCursor(cursorState{ref: ref, offset: sliced.nextOffset, endOffset: end})
	}
	return result
}

func safePreviewBoundaryStart(text string, index int) int {
	if index <= 0 {
		return 0
	}
	for index > 0 && !utf8.RuneStart(text[index]) {
		index--
	}
	return index
}

func safePreviewBoundaryEnd(text string, index int) int {
	if index >= len(text) {
		return len(text)
	}
	for index < len(text) && !utf8.RuneStart(text[index]) {
		index++
	}
	return index
}

func (s *RecoverySession) Grep(ref, query string) GrepResult {
	if begin := s.beginCall(); begin != "" {
		return GrepResult{Error: begin}
	}
	artifact, ok := s.registry.Get(ref)
	if !ok {
		return GrepResult{Error: ErrNotFound}
	}
	if query == "" {
		return GrepResult{Error: ErrEmptyQuery}
	}
	if utf8.RuneCountInString(query) > MaxGrepQueryChars {
		return GrepResult{Error: ErrQueryTooLong}
	}

	text := artifact.Content
	callBytes := 0
	matches := make([]GrepMatch, 0)
	outputBytes := 0
	line := 1
	chunkStart := 0
	truncated := false

	for chunkStart < len(text) {
		if aborted(s.ctx) {
			return GrepResult{Error: ErrAborted}
		}
		chunkEnd := chunkStart + grepChunkBytes
		if chunkEnd > len(text) {
			chunkEnd = len(text)
		}
		searchEnd := chunkEnd + len(query) - 1
		if searchEnd < chunkEnd {
			searchEnd = chunkEnd
		}
		if searchEnd > len(text) {
			searchEnd = len(text)
		}
		scanIndex := chunkStart
		for scanIndex < searchEnd {
			_, size := utf8.DecodeRuneInString(text[scanIndex:])
			if limited := s.scanUnit(size, &callBytes); limited != "" {
				if limited == ErrScanLimit {
					return GrepResult{OK: true, Matches: matches, Truncated: true}
				}
				return GrepResult{Error: limited}
			}
			scanIndex += size
		}

		searchWindow := text[chunkStart:searchEnd]
		from := 0
		lineCursor := chunkStart
		for from < len(searchWindow) {
			relativeFound := strings.Index(searchWindow[from:], query)
			if relativeFound < 0 {
				break
			}
			relativeFound += from
			found := chunkStart + relativeFound
			if found >= chunkEnd {
				break
			}
			for i := lineCursor; i < found; i++ {
				if text[i] == '\n' {
					line++
				}
			}
			lineCursor = found
			previewStart := safePreviewBoundaryStart(text, found-48)
			if previewStart < 0 {
				previewStart = 0
			}
			previewEnd := safePreviewBoundaryEnd(text, found+len(query)+48)
			if previewEnd > len(text) {
				previewEnd = len(text)
			}
			match := GrepMatch{
				Line:    line,
				Preview: text[previewStart:previewEnd],
				Cursor:  s.newCursor(cursorState{ref: ref, offset: found, endOffset: len(text)}),
			}
			encoded, _ := json.Marshal(match)
			encodedBytes := len(encoded)
			if outputBytes+encodedBytes > MaxGrepOutputBytes ||
				s.returnedBytes+outputBytes+encodedBytes > MaxReturnedBytesPerTurn {
				delete(s.cursors, match.Cursor)
				truncated = true
				break
			}
			matches = append(matches, match)
			outputBytes += encodedBytes
			if len(matches) >= MaxGrepMatches {
				truncated = true
				break
			}
			step := len(query)
			if step < 1 {
				step = 1
			}
			from = relativeFound + step
		}
		if truncated {
			break
		}
		for i := lineCursor; i < chunkEnd; i++ {
			if text[i] == '\n' {
				line++
			}
		}
		chunkStart = chunkEnd
	}
	s.returnedBytes += outputBytes
	return GrepResult{OK: true, Matches: matches, Truncated: truncated}
}

type recoveryCall struct {
	Op        string `json:"op"`
	Ref       string `json:"ref"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Cursor    string `json:"cursor"`
	MaxBytes  int    `json:"max_bytes"`
	Query     string `json:"query"`
}

// HandleCall executes one advertised recovery-tool invocation and returns JSON
// the model can read. failOpen is true when a hard budget was hit and the
// caller should restart once with the unprojected canonical context.
func (s *RecoverySession) HandleCall(arguments string) (payload string, failOpen bool) {
	var call recoveryCall
	if json.Unmarshal([]byte(arguments), &call) != nil {
		return recoveryErrorJSON("invalid_arguments"), false
	}
	switch strings.ToLower(strings.TrimSpace(call.Op)) {
	case "read":
		result := s.Read(call.Ref, ReadOptions{
			StartLine: call.StartLine,
			EndLine:   call.EndLine,
			Cursor:    call.Cursor,
			MaxBytes:  call.MaxBytes,
			HasLines:  call.StartLine != 0 || call.EndLine != 0,
		})
		if result.Error != "" {
			return recoveryErrorJSON(string(result.Error)), recoveryFailOpen(result.Error)
		}
		body := map[string]any{"ok": true, "content": result.Content, "eof": result.EOF}
		if result.NextCursor != "" {
			body["next_cursor"] = result.NextCursor
		}
		return mustJSON(body), false
	case "grep":
		result := s.Grep(call.Ref, call.Query)
		if result.Error != "" {
			return recoveryErrorJSON(string(result.Error)), recoveryFailOpen(result.Error)
		}
		return mustJSON(map[string]any{
			"ok":        true,
			"matches":   result.Matches,
			"truncated": result.Truncated,
		}), false
	default:
		return recoveryErrorJSON("invalid_arguments"), false
	}
}

func recoveryFailOpen(err RecoveryError) bool {
	switch err {
	case ErrCallLimit, ErrByteLimit, ErrScanLimit, ErrAborted:
		return true
	default:
		return false
	}
}

func recoveryErrorJSON(code string) string {
	return mustJSON(map[string]any{"ok": false, "error": code})
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `{"ok":false,"error":"encode"}`
	}
	return string(encoded)
}
