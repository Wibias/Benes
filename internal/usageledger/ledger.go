package usageledger

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	DirName            = "usage"
	ActiveFile         = "active.jsonl"
	SegmentsDir        = "segments"
	LegacyFile         = "usage.jsonl"
	SegmentTargetBytes = 64 << 20
	seqWidth           = 8
	segmentSuffix      = ".jsonl"
)

type Ledger struct {
	home   string
	target int64
	mu     sync.RWMutex
}

type SegmentMeta struct {
	Sequence int64
	Name     string
	Bytes    int64
}

type Status struct {
	SegmentTargetBytes int64
	LogicalBytes       int64
	LegacyBytes        int64
	HasLegacy          bool
	ActiveBytes        int64
	HasActive          bool
	SealedCount        int
	Sealed             []SegmentMeta
}

func Open(home string) (*Ledger, error) {
	return OpenWithTarget(home, SegmentTargetBytes)
}

func OpenWithTarget(home string, target int64) (*Ledger, error) {
	home = strings.TrimSpace(home)
	if home == "" {
		return nil, fmt.Errorf("home is required")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	if target <= 0 {
		target = SegmentTargetBytes
	}
	return &Ledger{home: abs, target: target}, nil
}

func HomeFromPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.EqualFold(filepath.Base(path), LegacyFile) {
		return filepath.Dir(path)
	}
	return path
}

func (l *Ledger) Home() string {
	if l == nil {
		return ""
	}
	return l.home
}

func (l *Ledger) TargetBytes() int64 {
	if l == nil {
		return SegmentTargetBytes
	}
	return l.target
}

func (l *Ledger) legacyPath() string {
	return filepath.Join(l.home, LegacyFile)
}

func (l *Ledger) dir() string {
	return filepath.Join(l.home, DirName)
}

func (l *Ledger) activePath() string {
	return filepath.Join(l.dir(), ActiveFile)
}

func (l *Ledger) segmentsPath() string {
	return filepath.Join(l.dir(), SegmentsDir)
}

func (l *Ledger) segmentPath(seq int64) string {
	return filepath.Join(l.segmentsPath(), formatSegmentName(seq))
}

func formatSegmentName(seq int64) string {
	return fmt.Sprintf("%0*d%s", seqWidth, seq, segmentSuffix)
}

func parseSegmentName(name string) (int64, bool) {
	if len(name) != seqWidth+len(segmentSuffix) || !strings.HasSuffix(name, segmentSuffix) {
		return 0, false
	}
	num := name[:seqWidth]
	for i := 0; i < len(num); i++ {
		if num[i] < '0' || num[i] > '9' {
			return 0, false
		}
	}
	seq, err := strconv.ParseInt(num, 10, 64)
	if err != nil || seq < 1 {
		return 0, false
	}
	return seq, true
}

func (l *Ledger) Status() (Status, error) {
	if l == nil {
		return Status{}, fmt.Errorf("ledger is nil")
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	state, err := l.loadRetentionStateLocked()
	if err != nil {
		return Status{}, err
	}
	opened, err := l.openSourcesRetained(state)
	if err != nil {
		return Status{}, err
	}
	defer closeOpened(opened)
	out := Status{SegmentTargetBytes: l.target, Sealed: []SegmentMeta{}}
	for _, src := range opened {
		out.LogicalBytes += src.size
		switch src.kind {
		case sourceLegacy:
			out.HasLegacy = true
			out.LegacyBytes = src.size
		case sourceSealed:
			out.SealedCount++
			out.Sealed = append(out.Sealed, SegmentMeta{Sequence: src.seq, Name: filepath.Base(src.path), Bytes: src.size})
		case sourceActive:
			out.HasActive = true
			out.ActiveBytes = src.size
		}
	}
	return out, nil
}

func fileSize(path string) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if info.IsDir() {
		return 0, fmt.Errorf("%s is a directory", path)
	}
	if isReparse(info) {
		return 0, errPathEscape
	}
	return info.Size(), nil
}
