package usageledger

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (l *Ledger) PreviewRetention(policy RetentionPolicy, now time.Time) (RetentionPreview, error) {
	if l == nil {
		return RetentionPreview{}, fmt.Errorf("ledger is nil")
	}
	policy, err := NormalizeRetentionPolicy(policy)
	if err != nil {
		return RetentionPreview{OK: false, Error: CodeInvalidPolicy, Message: err.Error()}, err
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.previewRetentionLocked(policy, now)
}

func (l *Ledger) RunRetention(policy RetentionPolicy, digest string, now time.Time) (RetentionRunResult, error) {
	if l == nil {
		return RetentionRunResult{}, fmt.Errorf("ledger is nil")
	}
	policy, err := NormalizeRetentionPolicy(policy)
	if err != nil {
		return RetentionRunResult{OK: false, Error: CodeInvalidPolicy, Message: err.Error()}, err
	}
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return RetentionRunResult{OK: false, Error: CodeStalePreview, Message: "preview digest is required"}, ErrStalePreview
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.IsZero() {
		now = time.Now()
	}
	preview, err := l.previewRetentionLocked(policy, now)
	if err != nil {
		return RetentionRunResult{OK: false, Policy: policy, Error: preview.Error, Message: err.Error()}, err
	}
	if preview.Digest != digest {
		return RetentionRunResult{OK: false, Policy: policy, Error: CodeStalePreview, Message: "ledger or policy changed since preview"}, ErrStalePreview
	}
	if len(preview.DeleteRelPaths) == 0 {
		ext, err := l.extentsLocked()
		if err != nil {
			return RetentionRunResult{OK: false, Policy: policy, Error: CodeFSFailed, Message: err.Error()}, err
		}
		return RetentionRunResult{
			OK:               true,
			Policy:           policy,
			DeletedRelPaths:  []string{},
			Extents:          ext,
			Generation:       preview.Generation,
			HistoryTruncated: ext.RetainedFromOffset > 0,
		}, nil
	}
	state, err := l.loadRetentionStateLocked()
	if err != nil {
		return RetentionRunResult{OK: false, Policy: policy, Error: CodeFSFailed, Message: err.Error()}, err
	}
	next := state
	next.RetainedFromOffset = preview.NextRetainedFrom
	next.FirstRetainedSequence = preview.NextFirstSeq
	next.LegacyRetired = preview.NextLegacyRet
	next.Generation = state.Generation + 1
	next.PendingDeletes = append([]string{}, preview.DeleteRelPaths...)
	if segs, err := l.sealedSegments(); err == nil && len(segs) > 0 {
		if segs[len(segs)-1].seq > next.SequenceHighWater {
			next.SequenceHighWater = segs[len(segs)-1].seq
		}
	}
	// Crash order: commit logical state, then physical delete.
	if err := l.writeRetentionStateLocked(next); err != nil {
		return RetentionRunResult{OK: false, Policy: policy, Error: CodeFSFailed, Message: err.Error()}, err
	}
	pending := append([]string{}, next.PendingDeletes...)
	deleted := make([]string, 0, len(pending))
	var deletedBytes int64
	for i, rel := range pending {
		abs := filepath.Join(l.home, filepath.FromSlash(rel))
		if err := l.insideHome(abs); err != nil {
			return RetentionRunResult{OK: false, Policy: policy, Error: CodePathEscape, Message: err.Error(), DeletedRelPaths: deleted, DeletedBytes: deletedBytes}, err
		}
		if err := l.refuseReparseAncestors(abs); err != nil {
			return RetentionRunResult{OK: false, Policy: policy, Error: CodePathEscape, Message: err.Error(), DeletedRelPaths: deleted, DeletedBytes: deletedBytes}, err
		}
		if err := l.rejectLeafReparse(abs); err != nil && !os.IsNotExist(err) {
			return RetentionRunResult{OK: false, Policy: policy, Error: CodePathEscape, Message: err.Error(), DeletedRelPaths: deleted, DeletedBytes: deletedBytes}, err
		}
		info, statErr := os.Lstat(abs)
		if statErr == nil && !info.IsDir() && !isReparse(info) {
			deletedBytes += info.Size()
		}
		if remErr := os.Remove(abs); remErr != nil && !os.IsNotExist(remErr) {
			return RetentionRunResult{OK: false, Policy: policy, Error: CodeFSFailed, Message: remErr.Error(), DeletedRelPaths: deleted, DeletedBytes: deletedBytes}, remErr
		}
		deleted = append(deleted, rel)
		// Drop completed pending entries as we go so a mid-delete crash
		// still leaves only remaining leftovers.
		cleared := next
		cleared.PendingDeletes = append([]string{}, pending[i+1:]...)
		if err := l.writeRetentionStateLocked(cleared); err != nil {
			return RetentionRunResult{OK: false, Policy: policy, Error: CodeFSFailed, Message: err.Error(), DeletedRelPaths: deleted, DeletedBytes: deletedBytes}, err
		}
		next = cleared
	}
	ext, err := l.extentsLocked()
	if err != nil {
		return RetentionRunResult{OK: false, Policy: policy, Error: CodeFSFailed, Message: err.Error(), DeletedRelPaths: deleted, DeletedBytes: deletedBytes}, err
	}
	return RetentionRunResult{
		OK:               true,
		Policy:           policy,
		DeletedRelPaths:  deleted,
		DeletedBytes:     deletedBytes,
		Extents:          ext,
		Generation:       next.Generation,
		HistoryTruncated: ext.RetainedFromOffset > 0,
	}, nil
}

func (l *Ledger) CleanupPendingDeletes() error {
	if l == nil {
		return fmt.Errorf("ledger is nil")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	state, err := l.loadRetentionStateLocked()
	if err != nil {
		return err
	}
	if len(state.PendingDeletes) == 0 {
		return nil
	}
	remain := make([]string, 0, len(state.PendingDeletes))
	for _, rel := range state.PendingDeletes {
		abs := filepath.Join(l.home, filepath.FromSlash(rel))
		if err := l.insideHome(abs); err != nil {
			return err
		}
		if err := l.rejectLeafReparse(abs); err != nil && !os.IsNotExist(err) {
			return err
		}
		if remErr := os.Remove(abs); remErr != nil && !os.IsNotExist(remErr) {
			remain = append(remain, rel)
			continue
		}
	}
	state.PendingDeletes = remain
	return l.writeRetentionStateLocked(state)
}

func (l *Ledger) previewRetentionLocked(policy RetentionPolicy, now time.Time) (RetentionPreview, error) {
	if now.IsZero() {
		now = time.Now()
	}
	state, err := l.loadRetentionStateLocked()
	if err != nil {
		return RetentionPreview{OK: false, Error: CodeFSFailed, Message: err.Error()}, err
	}
	listed, err := l.listSourcesRetained(state)
	if err != nil {
		return RetentionPreview{OK: false, Error: CodeFSFailed, Message: err.Error()}, err
	}
	infos := make([]RetentionSourceInfo, 0, len(listed))
	offset := state.RetainedFromOffset
	if offset < 0 {
		offset = 0
	}
	for _, src := range listed {
		info := RetentionSourceInfo{
			Kind:      sourceKindName(src.kind),
			Sequence:  src.seq,
			Bytes:     src.size,
			Start:     offset,
			End:       offset + src.size,
			Retirable: src.kind != sourceActive,
		}
		rel, relErr := pendingRelForSource(src)
		if relErr != nil {
			info.RelPath = filepath.ToSlash(strings.TrimPrefix(src.path, l.home+string(filepath.Separator)))
		} else {
			info.RelPath = rel
		}
		if src.kind != sourceActive {
			newest, known, ageErr := l.newestTimestampInSource(src)
			if ageErr != nil {
				return RetentionPreview{OK: false, Error: CodeFSFailed, Message: ageErr.Error()}, ageErr
			}
			info.NewestMs = newest
			info.AgeKnown = known
		} else {
			info.AgeKnown = true
		}
		infos = append(infos, info)
		offset += src.size
	}
	ext := Extents{
		RetainedFromOffset: state.RetainedFromOffset,
		EndOffset:          offset,
		RetainedBytes:      offset - state.RetainedFromOffset,
	}
	out := RetentionPreview{
		OK:               true,
		Generation:       state.Generation,
		Policy:           policy,
		Extents:          ext,
		Sources:          infos,
		DeleteRelPaths:   []string{},
		NextRetainedFrom: state.RetainedFromOffset,
		NextFirstSeq:     state.FirstRetainedSequence,
		NextLegacyRet:    state.LegacyRetired,
		HistoryTruncated: state.RetainedFromOffset > 0,
	}
	if !policy.Enabled() {
		out.Digest = retentionDigest(policy, state, infos, out.DeleteRelPaths)
		return out, nil
	}

	cutoffBytes := int64(-1)
	if policy.MaxBytes > 0 {
		if ext.RetainedBytes > policy.MaxBytes {
			cutoffBytes = ext.EndOffset - policy.MaxBytes
		}
	}
	cutoffAge := int64(-1)
	ageOnly := policy.MaxBytes <= 0 && policy.MaxAgeMs > 0
	if policy.MaxAgeMs > 0 {
		cutoffAge = now.UnixMilli() - policy.MaxAgeMs
		if cutoffAge < 0 {
			cutoffAge = 0
		}
	}

	// Age-only: unknown age in any retirable prefix candidate blocks the run.
	if ageOnly {
		for _, info := range infos {
			if !info.Retirable {
				continue
			}
			if !info.AgeKnown {
				err := fmt.Errorf("%w: source %s has unknown age", ErrAgeUnknown, info.RelPath)
				return RetentionPreview{OK: false, Policy: policy, Error: CodeAgeUnknown, Message: err.Error(), Extents: ext, Sources: infos, Generation: state.Generation}, err
			}
		}
	}

	deleteIdx := 0
	for i, info := range infos {
		if !info.Retirable {
			break
		}
		retire := false
		if cutoffBytes >= 0 && info.End <= cutoffBytes {
			retire = true
		}
		if cutoffAge >= 0 && info.AgeKnown && info.NewestMs > 0 && info.NewestMs < cutoffAge {
			// Whole source newest is older than cutoff: safe to retire.
			retire = true
		}
		if cutoffAge >= 0 && !info.AgeKnown && policy.MaxBytes > 0 {
			// Combined: unknown age cannot force age prune; size may still.
			// leave retire as size-decided only
		}
		if !retire {
			break
		}
		deleteIdx = i + 1
	}

	if deleteIdx == 0 {
		out.Digest = retentionDigest(policy, state, infos, out.DeleteRelPaths)
		return out, nil
	}

	var deleteBytes int64
	paths := make([]string, 0, deleteIdx)
	nextFrom := state.RetainedFromOffset
	nextFirst := state.FirstRetainedSequence
	nextLegacy := state.LegacyRetired
	for i := 0; i < deleteIdx; i++ {
		info := infos[i]
		paths = append(paths, info.RelPath)
		deleteBytes += info.Bytes
		nextFrom = info.End
		if info.Kind == "legacy" {
			nextLegacy = true
		}
		if info.Kind == "sealed" {
			nextFirst = info.Sequence + 1
		}
	}
	out.DeleteRelPaths = paths
	out.DeleteBytes = deleteBytes
	out.NextRetainedFrom = nextFrom
	out.NextFirstSeq = nextFirst
	out.NextLegacyRet = nextLegacy
	out.HistoryTruncated = nextFrom > 0
	out.Digest = retentionDigest(policy, state, infos, paths)
	return out, nil
}

func sourceKindName(k sourceKind) string {
	switch k {
	case sourceLegacy:
		return "legacy"
	case sourceSealed:
		return "sealed"
	case sourceActive:
		return "active"
	default:
		return "unknown"
	}
}

func retentionDigest(policy RetentionPolicy, state RetentionState, sources []RetentionSourceInfo, deletes []string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, "usage-retention-v1\n")
	_, _ = io.WriteString(h, "maxBytes="+strconv.FormatInt(policy.MaxBytes, 10)+"\n")
	_, _ = io.WriteString(h, "maxAgeMs="+strconv.FormatInt(policy.MaxAgeMs, 10)+"\n")
	_, _ = io.WriteString(h, "generation="+strconv.FormatInt(state.Generation, 10)+"\n")
	_, _ = io.WriteString(h, "retainedFrom="+strconv.FormatInt(state.RetainedFromOffset, 10)+"\n")
	_, _ = io.WriteString(h, "firstSeq="+strconv.FormatInt(state.FirstRetainedSequence, 10)+"\n")
	_, _ = io.WriteString(h, "legacyRetired="+strconv.FormatBool(state.LegacyRetired)+"\n")
	for _, src := range sources {
		_, _ = io.WriteString(h, src.Kind+"|"+src.RelPath+"|"+strconv.FormatInt(src.Bytes, 10)+"|"+strconv.FormatInt(src.Start, 10)+"|"+strconv.FormatInt(src.End, 10)+"\n")
	}
	for _, d := range deletes {
		_, _ = io.WriteString(h, "delete="+d+"\n")
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:])
}

type tsProbe struct {
	Timestamp int64 `json:"timestamp"`
}

func (l *Ledger) newestTimestampInSource(src source) (newest int64, known bool, err error) {
	if err := l.rejectLeafReparse(src.path); err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	file, err := openLedgerFile(src.path, openRead)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64*1024)
	known = true
	sawRow := false
	err = scanRecords(reader, defaultMaxLine, func(line []byte, rawBytes int64, oversized bool) error {
		_ = rawBytes
		if oversized || len(line) == 0 {
			known = false
			return nil
		}
		var probe tsProbe
		if json.Unmarshal(line, &probe) != nil {
			known = false
			return nil
		}
		sawRow = true
		if probe.Timestamp <= 0 {
			known = false
			return nil
		}
		if probe.Timestamp > newest {
			newest = probe.Timestamp
		}
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	if !sawRow {
		return 0, true, nil
	}
	return newest, known, nil
}
