package usageledger

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type sourceKind int

const (
	sourceLegacy sourceKind = iota
	sourceSealed
	sourceActive
)

type source struct {
	kind sourceKind
	seq  int64
	path string
	size int64
}

type sealed struct {
	seq  int64
	name string
}

type SnapshotLimits struct {
	MaxBytes int64
	MaxLine  int
}

type Snapshot struct {
	Lines                [][]byte
	HistoryTruncated     bool
	TruncatedPrefixBytes int64
	LogicalBytes         int64
}

// Record is one newline-committed logical ledger row. Offset/End are
// committed-byte offsets across legacy + sealed + active. Empty and
// oversized rows still occupy [Offset, End) so an index cursor can advance.
type Record struct {
	Offset    int64
	End       int64
	Line      []byte
	Oversized bool
}

var testHookAfterListSources func()
var testHookAfterOpenSources func()
var testHookEnumerateProgress func(startOffset, endOffset, scannedBytes int64)

func SetAfterOpenSourcesTestHook(fn func()) {
	testHookAfterOpenSources = fn
}

func SetEnumerateProgressTestHook(fn func(startOffset, endOffset, scannedBytes int64)) {
	testHookEnumerateProgress = fn
}

func (l *Ledger) SnapshotNewest(limits SnapshotLimits) (Snapshot, error) {
	if l == nil {
		return Snapshot{}, fmt.Errorf("ledger is nil")
	}
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = SegmentTargetBytes
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	state, err := l.loadRetentionStateLocked()
	if err != nil {
		return Snapshot{}, err
	}
	opened, err := l.openSourcesRetained(state)
	if err != nil {
		return Snapshot{}, err
	}
	defer closeOpened(opened)
	retainedFrom := state.RetainedFromOffset
	if retainedFrom < 0 {
		retainedFrom = 0
	}
	var retained int64
	for _, src := range opened {
		retained += src.size // committed newline prefix; uncommitted tails are not logical bytes
	}
	end := retainedFrom + retained
	start := retainedFrom
	truncated := retainedFrom > 0
	if retained > limits.MaxBytes {
		start = end - limits.MaxBytes
		truncated = true
	}
	prefix := start
	var lines [][]byte
	offset := retainedFrom
	for i, src := range opened {
		srcEnd := offset + src.size
		if srcEnd <= start {
			offset = srcEnd
			continue
		}
		localStart := int64(0)
		if offset < start {
			localStart = start - offset
		}
		part, extra, err := scanSection(src.file, src.size, localStart, limits.MaxLine)
		if err != nil {
			return Snapshot{}, err
		}
		if i == firstRetainedIndexFrom(opened, retainedFrom, start) && extra > 0 {
			prefix = start + extra
		}
		lines = append(lines, part...)
		offset = srcEnd
	}
	if !truncated {
		prefix = 0
	}
	return Snapshot{
		Lines:                lines,
		HistoryTruncated:     truncated,
		TruncatedPrefixBytes: prefix,
		LogicalBytes:         end,
	}, nil
}

func firstRetainedIndex(opened []openedSource, start int64) int {
	return firstRetainedIndexFrom(opened, 0, start)
}

func firstRetainedIndexFrom(opened []openedSource, base, start int64) int {
	offset := base
	for i, src := range opened {
		srcEnd := offset + src.size
		if srcEnd > start {
			return i
		}
		offset = srcEnd
	}
	return len(opened)
}

func (l *Ledger) Enumerate(fn func(line []byte) error) error {
	_, err := l.EnumerateSnapshot(fn)
	return err
}

func (l *Ledger) EnumerateSnapshot(fn func(line []byte) error) (int64, error) {
	if fn == nil {
		return 0, fmt.Errorf("enumerator is nil")
	}
	return l.EnumerateFromSnapshot(0, func(rec Record) error {
		if rec.Oversized || len(rec.Line) == 0 {
			return nil
		}
		return fn(rec.Line)
	})
}

func (l *Ledger) EnumerateFromSnapshot(startOffset int64, fn func(Record) error) (int64, error) {
	if l == nil {
		return 0, fmt.Errorf("ledger is nil")
	}
	if fn == nil {
		return 0, fmt.Errorf("enumerator is nil")
	}
	if startOffset < 0 {
		return 0, fmt.Errorf("%w", ErrUnalignedOffset)
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	state, err := l.loadRetentionStateLocked()
	if err != nil {
		return 0, err
	}
	opened, err := l.openSourcesRetained(state)
	if err != nil {
		return 0, err
	}
	defer closeOpened(opened)
	retainedFrom := state.RetainedFromOffset
	if retainedFrom < 0 {
		retainedFrom = 0
	}
	var retained int64
	for _, src := range opened {
		retained += src.size
	}
	logical := retainedFrom + retained
	if hook := testHookAfterOpenSources; hook != nil {
		hook()
	}
	if startOffset < retainedFrom {
		startOffset = retainedFrom
	}
	if startOffset > logical {
		return logical, fmt.Errorf("%w", ErrOffsetBeyondEnd)
	}
	if startOffset == logical {
		if hook := testHookEnumerateProgress; hook != nil {
			hook(startOffset, logical, 0)
		}
		return logical, nil
	}
	var (
		offset       = retainedFrom
		scannedBytes int64
	)
	for _, src := range opened {
		srcEnd := offset + src.size
		if srcEnd <= startOffset || src.size <= 0 {
			offset = srcEnd
			continue
		}
		localStart := int64(0)
		if offset < startOffset {
			localStart = startOffset - offset
		}
		if err := recordBoundary(src.file, localStart); err != nil {
			return logical, err
		}
		section := io.NewSectionReader(src.file, localStart, src.size-localStart)
		reader := bufio.NewReaderSize(section, 64*1024)
		cursor := offset + localStart
		err := scanRecords(reader, defaultMaxLine, func(line []byte, rawBytes int64, oversized bool) error {
			rec := Record{Offset: cursor, End: cursor + rawBytes, Line: line, Oversized: oversized}
			cursor = rec.End
			scannedBytes += rawBytes
			return fn(rec)
		})
		if err != nil {
			return logical, err
		}
		offset = srcEnd
	}
	if hook := testHookEnumerateProgress; hook != nil {
		hook(startOffset, logical, scannedBytes)
	}
	return logical, nil
}

func CommittedBytes(home string) ([]byte, error) {
	l, err := Open(home)
	if err != nil {
		return nil, err
	}
	var buf []byte
	err = l.Enumerate(func(line []byte) error {
		buf = append(buf, line...)
		buf = append(buf, '\n')
		return nil
	})
	return buf, err
}

func (l *Ledger) LogicalSize() (int64, error) {
	ext, err := l.Extents()
	if err != nil {
		return 0, err
	}
	return ext.EndOffset, nil
}

type openedSource struct {
	source
	file *os.File
	info os.FileInfo
}

func (l *Ledger) sealedSegments() ([]sealed, error) {
	dir := l.segmentsPath()
	missing, err := l.ancestorsOK(dir)
	if err != nil {
		return nil, err
	}
	if missing {
		return nil, nil
	}
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if isReparse(info) {
		return nil, errPathEscape
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	state, stateErr := l.loadRetentionStateLocked()
	if stateErr != nil {
		return nil, stateErr
	}
	pending := map[string]bool{}
	for _, rel := range state.PendingDeletes {
		pending[filepath.Base(rel)] = true
	}
	var segs []sealed
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		seq, ok := parseSegmentName(entry.Name())
		if !ok {
			continue
		}
		if pending[entry.Name()] || state.sourceRetired(source{kind: sourceSealed, seq: seq}) {
			continue
		}
		path := filepath.Join(l.segmentsPath(), entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if isReparse(info) {
			return nil, errPathEscape
		}
		segs = append(segs, sealed{seq: seq, name: entry.Name()})
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].seq < segs[j].seq })
	if len(segs) == 0 {
		return segs, nil
	}
	expectFirst := int64(1)
	if state.FirstRetainedSequence > 0 {
		expectFirst = state.FirstRetainedSequence
	}
	if segs[0].seq != expectFirst {
		return nil, fmt.Errorf("%w: first sealed segment is %s", ErrCorruptLedger, segs[0].name)
	}
	for i := 1; i < len(segs); i++ {
		if segs[i].seq != segs[i-1].seq+1 {
			return nil, fmt.Errorf("%w: gap between %s and %s", ErrCorruptLedger, segs[i-1].name, segs[i].name)
		}
	}
	return segs, nil
}

func (l *Ledger) listSources() ([]source, error) {
	var out []source
	legacy := l.legacyPath()
	if size, ok, err := l.listedFile(legacy); err != nil {
		return nil, err
	} else if ok {
		out = append(out, source{kind: sourceLegacy, path: legacy, size: size})
	}
	segs, err := l.sealedSegments()
	if err != nil {
		return nil, err
	}
	for _, seg := range segs {
		path := filepath.Join(l.segmentsPath(), seg.name)
		size, ok, err := l.listedFile(path)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("usage ledger snapshot changed during open")
		}
		out = append(out, source{kind: sourceSealed, seq: seg.seq, path: path, size: size})
	}
	active := l.activePath()
	if size, ok, err := l.listedFile(active); err != nil {
		return nil, err
	} else if ok {
		out = append(out, source{kind: sourceActive, path: active, size: size})
	}
	return out, nil
}

func (l *Ledger) listedFile(path string) (int64, bool, error) {
	missing, err := l.ancestorsOK(path)
	if err != nil {
		return 0, false, err
	}
	if missing {
		return 0, false, nil
	}
	size, err := fileSize(path)
	if err != nil {
		return 0, false, err
	}
	if !fileExists(path) {
		return 0, false, nil
	}
	return size, true, nil
}

func (l *Ledger) openSources() ([]openedSource, error) {
	var lastErr error
	for attempt := 0; attempt < maxSnapshotRetries; attempt++ {
		listed, err := l.listSources()
		if err != nil {
			return nil, err
		}
		if hook := testHookAfterListSources; hook != nil {
			hook()
		}
		opened, err := l.openListed(listed)
		if err != nil {
			return nil, err
		}
		listed2, err := l.listSources()
		if err != nil {
			closeOpened(opened)
			return nil, err
		}
		if snapshotCovers(opened, listed2) {
			return opened, nil
		}
		closeOpened(opened)
		lastErr = fmt.Errorf("usage ledger snapshot changed during open")
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("usage ledger snapshot did not stabilize")
	}
	return nil, lastErr
}

func (l *Ledger) openListed(listed []source) ([]openedSource, error) {
	var opened []openedSource
	for _, src := range listed {
		missing, err := l.ancestorsOK(src.path)
		if err != nil {
			closeOpened(opened)
			return nil, err
		}
		if missing {
			continue
		}
		if err := l.insideHome(src.path); err != nil {
			closeOpened(opened)
			return nil, err
		}
		if err := l.rejectLeafReparse(src.path); err != nil {
			closeOpened(opened)
			return nil, err
		}
		file, err := openLedgerFile(src.path, openRead)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			closeOpened(opened)
			return nil, err
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			closeOpened(opened)
			return nil, err
		}
		if info.IsDir() {
			_ = file.Close()
			closeOpened(opened)
			return nil, fmt.Errorf("%s is a directory", src.path)
		}
		dup := false
		for _, existing := range opened {
			if os.SameFile(existing.info, info) {
				dup = true
				break
			}
		}
		if dup {
			_ = file.Close()
			continue
		}
		committed, err := committedPrefix(file, info.Size())
		if err != nil {
			_ = file.Close()
			closeOpened(opened)
			return nil, err
		}
		src.size = committed
		opened = append(opened, openedSource{source: src, file: file, info: info})
	}
	return opened, nil
}

func snapshotCovers(opened []openedSource, listed []source) bool {
	for _, src := range listed {
		info, err := os.Lstat(src.path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false
		}
		if info.IsDir() || isReparse(info) {
			return false
		}
		found := false
		for _, existing := range opened {
			if os.SameFile(existing.info, info) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func closeOpened(opened []openedSource) {
	for _, src := range opened {
		if src.file != nil {
			_ = src.file.Close()
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func (l *Ledger) listSourcesRetained(state RetentionState) ([]source, error) {
	listed, err := l.listSources()
	if err != nil {
		return nil, err
	}
	out := make([]source, 0, len(listed))
	pending := map[string]bool{}
	for _, rel := range state.PendingDeletes {
		pending[filepath.ToSlash(rel)] = true
	}
	for _, src := range listed {
		if state.sourceRetired(src) {
			continue
		}
		rel, relErr := pendingRelForSource(src)
		if relErr == nil && pending[rel] {
			continue
		}
		out = append(out, src)
	}
	return out, nil
}

func (l *Ledger) openSourcesRetained(state RetentionState) ([]openedSource, error) {
	var lastErr error
	for attempt := 0; attempt < maxSnapshotRetries; attempt++ {
		listed, err := l.listSourcesRetained(state)
		if err != nil {
			return nil, err
		}
		if hook := testHookAfterListSources; hook != nil {
			hook()
		}
		opened, err := l.openListed(listed)
		if err != nil {
			return nil, err
		}
		listed2, err := l.listSourcesRetained(state)
		if err != nil {
			closeOpened(opened)
			return nil, err
		}
		if snapshotCovers(opened, listed2) {
			return opened, nil
		}
		closeOpened(opened)
		lastErr = fmt.Errorf("usage ledger snapshot changed during open")
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("usage ledger snapshot did not stabilize")
	}
	return nil, lastErr
}
