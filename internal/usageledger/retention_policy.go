package usageledger

import (
	"encoding/json"
	"fmt"
)

// RetentionPolicy is an opt-in durable retention bound. Zero MaxBytes / MaxAge
// means that dimension is disabled. Default is fully disabled.
type RetentionPolicy struct {
	MaxBytes int64 `json:"maxBytes"`
	MaxAgeMs int64 `json:"maxAgeMs"`
}

func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{}
}

func NormalizeRetentionPolicy(p RetentionPolicy) (RetentionPolicy, error) {
	if p.MaxBytes < 0 {
		return RetentionPolicy{}, fmt.Errorf("%w: maxBytes must be >= 0", ErrInvalidPolicy)
	}
	if p.MaxAgeMs < 0 {
		return RetentionPolicy{}, fmt.Errorf("%w: maxAgeMs must be >= 0", ErrInvalidPolicy)
	}
	return p, nil
}

func ParseRetentionPolicy(raw []byte) (RetentionPolicy, error) {
	if len(raw) == 0 {
		return DefaultRetentionPolicy(), nil
	}
	var p RetentionPolicy
	if err := json.Unmarshal(raw, &p); err != nil {
		return RetentionPolicy{}, fmt.Errorf("%w: invalid JSON", ErrInvalidPolicy)
	}
	return NormalizeRetentionPolicy(p)
}

func (p RetentionPolicy) Enabled() bool {
	return p.MaxBytes > 0 || p.MaxAgeMs > 0
}

type RetentionSourceInfo struct {
	Kind      string `json:"kind"`
	Sequence  int64  `json:"sequence,omitempty"`
	RelPath   string `json:"relPath"`
	Bytes     int64  `json:"bytes"`
	Start     int64  `json:"startOffset"`
	End       int64  `json:"endOffset"`
	NewestMs  int64  `json:"newestTimestampMs,omitempty"`
	AgeKnown  bool   `json:"ageKnown"`
	Retirable bool   `json:"retirable"`
}

type RetentionPreview struct {
	OK               bool                  `json:"ok"`
	Digest           string                `json:"digest"`
	Generation       int64                 `json:"generation"`
	Policy           RetentionPolicy       `json:"policy"`
	Extents          Extents               `json:"extents"`
	Sources          []RetentionSourceInfo `json:"sources"`
	DeleteRelPaths   []string              `json:"deleteRelPaths"`
	DeleteBytes      int64                 `json:"deleteBytes"`
	NextRetainedFrom int64                 `json:"nextRetainedFromOffset"`
	NextFirstSeq     int64                 `json:"nextFirstRetainedSequence"`
	NextLegacyRet    bool                  `json:"nextLegacyRetired"`
	HistoryTruncated bool                  `json:"historyTruncated"`
	Message          string                `json:"message,omitempty"`
	Error            string                `json:"error,omitempty"`
}

type RetentionRunResult struct {
	OK               bool            `json:"ok"`
	Policy           RetentionPolicy `json:"policy"`
	DeletedRelPaths  []string        `json:"deletedRelPaths"`
	DeletedBytes     int64           `json:"deletedBytes"`
	Extents          Extents         `json:"extents"`
	Generation       int64           `json:"generation"`
	HistoryTruncated bool            `json:"historyTruncated"`
	Error            string          `json:"error,omitempty"`
	Message          string          `json:"message,omitempty"`
}

type RetentionStatus struct {
	Policy           RetentionPolicy `json:"policy"`
	State            RetentionState  `json:"state"`
	Extents          Extents         `json:"extents"`
	HistoryTruncated bool            `json:"historyTruncated"`
}

func (l *Ledger) RetentionStatus(policy RetentionPolicy) (RetentionStatus, error) {
	if l == nil {
		return RetentionStatus{}, fmt.Errorf("ledger is nil")
	}
	policy, err := NormalizeRetentionPolicy(policy)
	if err != nil {
		return RetentionStatus{}, err
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	state, err := l.loadRetentionStateLocked()
	if err != nil {
		return RetentionStatus{}, err
	}
	ext, err := l.extentsLocked()
	if err != nil {
		return RetentionStatus{}, err
	}
	return RetentionStatus{
		Policy:           policy,
		State:            state,
		Extents:          ext,
		HistoryTruncated: ext.RetainedFromOffset > 0,
	}, nil
}
