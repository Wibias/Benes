package usageledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	RetentionStateVersion  = 1
	RetentionStateName     = "retention.json"
	maxRetentionStateBytes = 1 << 20
)

// RetentionState is the crash-safe durable watermark for prefix pruning.
// Readers ignore retired sources when this state proves they are retired,
// even if pendingDeletes files still exist after a crash.
type RetentionState struct {
	Version               int      `json:"version"`
	RetainedFromOffset    int64    `json:"retainedFromOffset"`
	FirstRetainedSequence int64    `json:"firstRetainedSequence"`
	SequenceHighWater     int64    `json:"sequenceHighWater"`
	LegacyRetired         bool     `json:"legacyRetired"`
	Generation            int64    `json:"generation"`
	PendingDeletes        []string `json:"pendingDeletes,omitempty"`
}

func (l *Ledger) retentionStatePath() string {
	return filepath.Join(l.dir(), RetentionStateName)
}

func defaultRetentionState() RetentionState {
	return RetentionState{
		Version:               RetentionStateVersion,
		RetainedFromOffset:    0,
		FirstRetainedSequence: 0,
		SequenceHighWater:     0,
		LegacyRetired:         false,
		Generation:            0,
		PendingDeletes:        nil,
	}
}

func (l *Ledger) loadRetentionStateLocked() (RetentionState, error) {
	path := l.retentionStatePath()
	missing, err := l.ancestorsOK(path)
	if err != nil {
		return RetentionState{}, err
	}
	if missing {
		return defaultRetentionState(), nil
	}
	if err := l.rejectLeafReparse(path); err != nil {
		if os.IsNotExist(err) {
			return defaultRetentionState(), nil
		}
		return RetentionState{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultRetentionState(), nil
		}
		return RetentionState{}, err
	}
	if isReparse(info) {
		return RetentionState{}, errPathEscape
	}
	if info.IsDir() {
		return RetentionState{}, fmt.Errorf("retention state is a directory")
	}
	if info.Size() > maxRetentionStateBytes {
		return RetentionState{}, fmt.Errorf("retention state is too large")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultRetentionState(), nil
		}
		return RetentionState{}, err
	}
	if len(raw) == 0 {
		return defaultRetentionState(), nil
	}
	var state RetentionState
	if err := json.Unmarshal(raw, &state); err != nil {
		return RetentionState{}, fmt.Errorf("retention state is malformed: %w", err)
	}
	if state.Version == 0 {
		state.Version = RetentionStateVersion
	}
	if state.Version != RetentionStateVersion {
		return RetentionState{}, fmt.Errorf("unsupported retention state version %d", state.Version)
	}
	if state.RetainedFromOffset < 0 || state.FirstRetainedSequence < 0 || state.SequenceHighWater < 0 || state.Generation < 0 {
		return RetentionState{}, fmt.Errorf("retention state has negative fields")
	}
	if state.PendingDeletes == nil {
		state.PendingDeletes = []string{}
	}
	for _, rel := range state.PendingDeletes {
		if err := validatePendingRel(rel); err != nil {
			return RetentionState{}, err
		}
	}
	return state, nil
}

func validatePendingRel(rel string) error {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return fmt.Errorf("pending delete path is empty")
	}
	if filepath.IsAbs(rel) {
		return fmt.Errorf("pending delete path must be relative")
	}
	clean := filepath.ToSlash(filepath.Clean(rel))
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "\\") {
		return fmt.Errorf("pending delete path escapes home")
	}
	switch {
	case clean == LegacyFile:
		return nil
	case strings.HasPrefix(clean, DirName+"/"+SegmentsDir+"/"):
		base := filepath.Base(clean)
		if _, ok := parseSegmentName(base); !ok {
			return fmt.Errorf("pending delete segment name is invalid")
		}
		return nil
	default:
		return fmt.Errorf("pending delete path is not a whole ledger source")
	}
}

func (l *Ledger) writeRetentionStateLocked(state RetentionState) error {
	state.Version = RetentionStateVersion
	if state.PendingDeletes == nil {
		state.PendingDeletes = []string{}
	}
	for _, rel := range state.PendingDeletes {
		if err := validatePendingRel(rel); err != nil {
			return err
		}
	}
	if err := l.ensureLedgerDirs(); err != nil {
		return err
	}
	path := l.retentionStatePath()
	if err := l.insideHome(path); err != nil {
		return err
	}
	if err := l.refuseReparseAncestors(path); err != nil {
		return err
	}
	if err := l.rejectLeafReparse(path); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	parent := filepath.Dir(path)
	tmp, err := os.CreateTemp(parent, ".retention-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpName)
	}()
	if err := l.insideHome(tmpName); err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	_ = tmp.Chmod(0o600)
	if err := tmp.Close(); err != nil {
		return err
	}
	closed = true
	if err := l.refuseReparseAncestors(parent); err != nil {
		return err
	}
	if err := replaceFile(tmpName, path); err != nil {
		return err
	}
	tmpName = ""
	return l.refuseReparseAncestors(path)
}

func (l *Ledger) LoadRetentionState() (RetentionState, error) {
	if l == nil {
		return RetentionState{}, fmt.Errorf("ledger is nil")
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loadRetentionStateLocked()
}

func pendingRelForSource(src source) (string, error) {
	switch src.kind {
	case sourceLegacy:
		return LegacyFile, nil
	case sourceSealed:
		return filepath.ToSlash(filepath.Join(DirName, SegmentsDir, formatSegmentName(src.seq))), nil
	default:
		return "", fmt.Errorf("active source cannot be pending-deleted")
	}
}

func (s RetentionState) sourceRetired(src source) bool {
	switch src.kind {
	case sourceLegacy:
		return s.LegacyRetired
	case sourceSealed:
		if s.FirstRetainedSequence <= 0 {
			return false
		}
		return src.seq < s.FirstRetainedSequence
	case sourceActive:
		return false
	default:
		return false
	}
}
