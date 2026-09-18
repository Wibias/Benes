package usageledger

import "errors"

var (
	ErrCorruptLedger   = errors.New("usage ledger sealed sequence is incomplete")
	ErrUnalignedOffset = errors.New("usage ledger start offset is not a committed record boundary")
	ErrOffsetBeyondEnd = errors.New("usage ledger start offset is beyond committed size")
	ErrStalePreview    = errors.New("usage retention preview is stale")
	ErrInvalidPolicy   = errors.New("usage retention policy is invalid")
	ErrRetentionBusy   = errors.New("usage retention is already running")
	ErrAgeUnknown      = errors.New("usage retention age is unknown")
	errPathEscape      = errors.New("usage ledger path escapes BENES_HOME or follows a reparse point")
)

const (
	maxSnapshotRetries = 8
	CodeStalePreview   = "stale_preview"
	CodeInvalidPolicy  = "invalid_policy"
	CodeAgeUnknown     = "age_unknown"
	CodeRetentionBusy  = "retention_busy"
	CodeFSFailed       = "fs_failed"
	CodePathEscape     = "path_escape"
)
