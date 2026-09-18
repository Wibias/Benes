package storage

const (
	CodeScanFailed            = "scan_failed"
	CodeCodexBusy             = "codex_busy"
	CodeStalePreview          = "stale_preview"
	CodeRestorePendingOverlap = "restore_pending_overlap"
	CodeReferencedHistory     = "referenced_history"
	CodeInvalidDigest         = "invalid_digest"
	CodeInvalidMode           = "invalid_mode"
	CodeInvalidPercent        = "invalid_percent"
	CodeFSFailed              = "fs_failed"
	CodeDBReconcileFailed     = "db_reconcile_failed"
	CodeCleanupFailed         = "cleanup_failed"
	CodeInvalidTrash          = "invalid_trash"
	CodeMissingTrash          = "missing_trash"
	CodeDestExists            = "dest_exists"
	CodeStorageMutationBusy   = "storage_mutation_busy"
	CodeRestoreFailed         = "restore_failed"
	CodeRestoreWorkerTimeout  = "restore_worker_timeout"
	CodeRestoreWorkerAborted  = "restore_worker_aborted"
	CodeInvalidPolicy         = "invalid_policy"
	CodeAlreadyRunning        = "already_running"
	CodePathEscape            = "path_escape"
)

const (
	ModeQuarantine = "quarantine"
	ModePermanent  = "permanent"
)

const (
	ScheduleStartup = "startup"
	ScheduleDaily   = "daily"
	ScheduleWeekly  = "weekly"
	ScheduleManual  = "manual"
)

const (
	JobIdle    = "idle"
	JobRunning = "running"
)

const (
	SkipDisabled        = "disabled"
	SkipUnderThreshold  = "under_threshold"
	SkipNothingSelected = "nothing_selected"
	SkipManual          = "manual"
)

const (
	trashDirName     = ".trash"
	manifestName     = "manifest.json"
	journalName      = "journal.jsonl"
	pendingName      = "pending.json"
	filesDirName     = "files"
	restoreJournal   = "restore-journal.jsonl"
	maxWalkFiles     = 250_000
	maxLargest       = 5
	maxPreviewJSON   = 256
	maxTrashList     = 500
	maxManifestBytes = 1 << 20
	maxDigestLen     = 128
	maxTrashIDLen    = 80
	maxRelPathLen    = 4 << 10
)
