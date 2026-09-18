package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/Wibias/Benes/internal/usageledger"
)

var errNotFile = errors.New("usage log is not a file")

const (
	maxLineBytes = 1 << 20
)

type ReadLimits struct {
	MaxBytes int64
	MaxLine  int
}

type fileSnapshot struct {
	Entries              []entry
	HistoryTruncated     bool
	TruncatedPrefixBytes int64
	SnapshotBytes        int64
	SnapshotWindowStart  *int64
	SnapshotWindowEnd    *int64
}

func defaultReadLimits() ReadLimits {
	return ReadLimits{MaxBytes: maxReadBytes, MaxLine: maxLineBytes}
}

func (l ReadLimits) resolved() ReadLimits {
	if l.MaxBytes <= 0 {
		l.MaxBytes = maxReadBytes
	}
	if l.MaxLine <= 0 {
		l.MaxLine = maxLineBytes
	}
	return l
}

func readEntries(path string) ([]entry, bool, error) {
	snap, err := readFileSnapshot(path, defaultReadLimits())
	return snap.Entries, snap.HistoryTruncated, err
}

func summarizeHomeQueryLimited(home string, q Query, table *Table, limits ReadLimits) (Summary, error) {
	if _, err := resolveWindow(q); err != nil {
		return emptySummary(q.Range, q.Surface, q.Now), err
	}
	snap, err := readHomeSnapshot(home, limits)
	if err != nil {
		return emptySummary(q.Range, q.Surface, q.Now), err
	}
	out := SummarizeQuery(snap.Entries, q, table)
	applySnapshot(&out, snap)
	return out, nil
}

func readHomeSnapshot(home string, limits ReadLimits) (fileSnapshot, error) {
	limits = limits.resolved()
	home = strings.TrimSpace(home)
	if home == "" {
		return fileSnapshot{}, nil
	}
	ledger, err := usageledger.Open(home)
	if err != nil {
		return fileSnapshot{}, err
	}
	raw, err := ledger.SnapshotNewest(usageledger.SnapshotLimits{MaxBytes: limits.MaxBytes, MaxLine: limits.MaxLine})
	if err != nil {
		return fileSnapshot{}, err
	}
	entries, windowStart, windowEnd := parseRawLines(raw.Lines)
	return fileSnapshot{
		Entries:              entries,
		HistoryTruncated:     raw.HistoryTruncated,
		TruncatedPrefixBytes: raw.TruncatedPrefixBytes,
		SnapshotBytes:        raw.LogicalBytes,
		SnapshotWindowStart:  windowStart,
		SnapshotWindowEnd:    windowEnd,
	}, nil
}

func parseRawLines(lines [][]byte) ([]entry, *int64, *int64) {
	var (
		entries     []entry
		windowStart *int64
		windowEnd   *int64
	)
	for _, line := range lines {
		var item entry
		if json.Unmarshal(line, &item) != nil || !validPersistedEntry(item) {
			continue
		}
		entries = append(entries, item)
		if windowStart == nil || item.Timestamp < *windowStart {
			ts := item.Timestamp
			windowStart = &ts
		}
		if windowEnd == nil || item.Timestamp > *windowEnd {
			ts := item.Timestamp
			windowEnd = &ts
		}
	}
	return entries, windowStart, windowEnd
}

func readFileSnapshot(path string, limits ReadLimits) (fileSnapshot, error) {
	limits = limits.resolved()
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileSnapshot{}, nil
		}
		return fileSnapshot{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fileSnapshot{}, err
	}
	if info.IsDir() {
		return fileSnapshot{}, errNotFile
	}
	size := info.Size()
	snap := fileSnapshot{SnapshotBytes: size}
	if size <= 0 {
		return snap, nil
	}
	start := int64(0)
	if size > limits.MaxBytes {
		start = size - limits.MaxBytes
		snap.HistoryTruncated = true
		snap.TruncatedPrefixBytes = start
	}
	section := io.NewSectionReader(file, start, size-start)
	reader := bufio.NewReaderSize(section, 64*1024)
	if start > 0 {
		var prev [1]byte
		n, err := file.ReadAt(prev[:], start-1)
		aligned := err == nil && n == 1 && prev[0] == '\n'
		if !aligned {
			discarded, err := discardPartialLine(reader)
			snap.TruncatedPrefixBytes = start + discarded
			if err != nil && err != io.EOF {
				return snap, err
			}
		}
	}
	entries, windowStart, windowEnd, err := scanEntries(reader, limits.MaxLine)
	if err != nil {
		return snap, err
	}
	snap.Entries = entries
	snap.SnapshotWindowStart = windowStart
	snap.SnapshotWindowEnd = windowEnd
	return snap, nil
}

func discardPartialLine(reader *bufio.Reader) (int64, error) {
	var n int64
	for {
		fragment, err := reader.ReadSlice('\n')
		n += int64(len(fragment))
		if err == bufio.ErrBufferFull {
			continue
		}
		return n, err
	}
}

func scanEntries(reader *bufio.Reader, maxLine int) ([]entry, *int64, *int64, error) {
	var (
		entries     []entry
		windowStart *int64
		windowEnd   *int64
		buf         []byte
		oversized   bool
	)
	for {
		fragment, err := reader.ReadSlice('\n')
		hasNewline := len(fragment) > 0 && fragment[len(fragment)-1] == '\n'
		payload := fragment
		if hasNewline {
			payload = fragment[:len(fragment)-1]
			if len(payload) > 0 && payload[len(payload)-1] == '\r' {
				payload = payload[:len(payload)-1]
			}
		}
		if len(payload) > 0 && !oversized {
			if len(buf)+len(payload) > maxLine {
				oversized = true
				buf = buf[:0]
			} else {
				buf = append(buf, payload...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err == io.EOF {
			return entries, windowStart, windowEnd, nil
		}
		if err != nil {
			return entries, windowStart, windowEnd, err
		}
		if !hasNewline {
			return entries, windowStart, windowEnd, nil
		}
		line := strings.TrimSpace(string(buf))
		buf = buf[:0]
		skip := oversized
		oversized = false
		if skip || line == "" {
			continue
		}
		var item entry
		if json.Unmarshal([]byte(line), &item) != nil || !validPersistedEntry(item) {
			continue
		}
		entries = append(entries, item)
		if windowStart == nil || item.Timestamp < *windowStart {
			ts := item.Timestamp
			windowStart = &ts
		}
		if windowEnd == nil || item.Timestamp > *windowEnd {
			ts := item.Timestamp
			windowEnd = &ts
		}
	}
}

func validPersistedEntry(item entry) bool {
	return item.Timestamp > 0
}
