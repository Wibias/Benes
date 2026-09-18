package storage

import (
	"context"
	"crypto/rand"
	"io"
	"time"
)

type Engine struct {
	Lock           *Coordinator
	Now            func() time.Time
	Location       *time.Location
	Rand           io.Reader
	RestoreTimeout time.Duration
	Rename         func(oldpath, newpath string) error
	Remove         func(path string) error
}

func NewEngine() *Engine {
	return &Engine{
		Lock:           NewCoordinator(),
		Now:            time.Now,
		Location:       time.Local,
		Rand:           rand.Reader,
		RestoreTimeout: DefaultRestoreTimeout,
	}
}

func (e *Engine) now() time.Time {
	if e != nil && e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func (e *Engine) loc() *time.Location {
	if e != nil && e.Location != nil {
		return e.Location
	}
	return time.Local
}

func (e *Engine) acquire(op string) error {
	if e == nil || e.Lock == nil {
		return nil
	}
	if !e.Lock.TryAcquire(op) {
		if e.Lock.Holder() == "restore" && op == "cleanup" {
			return coded(CodeRestorePendingOverlap, "a restore is in progress")
		}
		return coded(CodeStorageMutationBusy, "another storage cleanup or restore is in progress")
	}
	return nil
}

func (e *Engine) Preview(codexHome string, percent int) (Preview, error) {
	return PreviewPercent(codexHome, percent)
}

func (e *Engine) Cleanup(codexHome string, req CleanupRequest) (CleanupResult, error) {
	if err := e.acquire("cleanup"); err != nil {
		codedErr := asError(err)
		return CleanupResult{OK: false, Mode: req.Mode, Error: codedErr.Code, Message: codedErr.Message}, codedErr
	}
	defer e.Lock.Release()
	if req.Now.IsZero() {
		req.Now = e.now()
	}
	if req.Rand == nil {
		req.Rand = e.Rand
	}
	if req.Rename == nil {
		req.Rename = e.Rename
	}
	if req.Remove == nil {
		req.Remove = e.Remove
	}
	return ExecuteCleanup(codexHome, req)
}

func (e *Engine) RunPolicy(codexHome string, policy Policy) (Policy, JobOutcome, error) {
	out := PolicyPublic(policy)
	if !policy.Enabled {
		outcome := JobOutcome{OK: true, Skipped: SkipDisabled}
		return stampJob(out, e.now(), e.loc(), outcome), outcome, nil
	}
	archived, _, truncated, err := ArchivedBytes(codexHome)
	if err != nil {
		outcome := JobOutcome{OK: false, Error: CodeCleanupFailed}
		return stampJob(out, e.now(), e.loc(), outcome), outcome, err
	}
	if truncated {
		outcome := JobOutcome{OK: false, Error: CodeCleanupFailed}
		return stampJob(out, e.now(), e.loc(), outcome), outcome, coded(CodeCleanupFailed, "CODEX_HOME is too large to clean safely")
	}
	if archived <= policy.Trigger.ArchivedBytesOver {
		outcome := JobOutcome{OK: true, Skipped: SkipUnderThreshold}
		return stampJob(out, e.now(), e.loc(), outcome), outcome, nil
	}
	preview, err := PreviewPolicyTarget(codexHome, percentOf(policy), policy.Target.ReduceToBytes)
	if err != nil {
		codedErr := asError(err)
		outcome := JobOutcome{OK: false, Error: codedErr.Code}
		if codedErr.Code == CodeCodexBusy {
			outcome.Deferred = CodeCodexBusy
			outcome.OK = true
			outcome.Error = ""
		}
		return stampJob(out, e.now(), e.loc(), outcome), outcome, err
	}
	if preview.Count == 0 {
		outcome := JobOutcome{OK: true, Skipped: SkipNothingSelected}
		return stampJob(out, e.now(), e.loc(), outcome), outcome, nil
	}
	if err := e.acquire("cleanup"); err != nil {
		codedErr := asError(err)
		outcome := JobOutcome{OK: false, Deferred: codedErr.Code}
		if codedErr.Code == CodeStorageMutationBusy || codedErr.Code == CodeRestorePendingOverlap {
			outcome.OK = true
			outcome.Error = ""
			outcome.Deferred = CodeStorageMutationBusy
		}
		return stampJob(out, e.now(), e.loc(), outcome), outcome, codedErr
	}
	defer e.Lock.Release()
	result, err := executePreviewPlan(codexHome, preview, policy, e)
	if err != nil {
		codedErr := asError(err)
		outcome := JobOutcome{OK: false, Error: codedErr.Code, Mode: policy.Mode, FreedBytes: result.FreedBytes, Removed: result.Count}
		if codedErr.Code == CodeCodexBusy {
			outcome.Deferred = CodeCodexBusy
			outcome.OK = true
			outcome.Error = ""
		}
		return stampJob(out, e.now(), e.loc(), outcome), outcome, codedErr
	}
	outcome := JobOutcome{OK: true, Mode: policy.Mode, FreedBytes: result.FreedBytes, Removed: result.Count}
	stamped := stampJob(out, e.now(), e.loc(), outcome)
	if result.Count > 0 {
		stamped.LastRun = &PolicyLastRun{At: e.now().UnixMilli(), FreedBytes: result.FreedBytes, Removed: result.Count}
	}
	return stamped, outcome, nil
}

func executePreviewPlan(codexHome string, preview Preview, policy Policy, e *Engine) (CleanupResult, error) {
	root, err := OpenRoot(codexHome)
	if err != nil {
		return CleanupResult{}, err
	}
	files, _, err := listArchived(root)
	if err != nil {
		return CleanupResult{}, err
	}
	lock, err := lockState(root)
	if err != nil {
		return CleanupResult{}, asError(err)
	}
	defer lock.Close()
	refs, err := lock.Refs()
	if err != nil {
		return CleanupResult{}, asError(err)
	}
	eligible := excludeRefs(files, refs)
	var picked []classifiedFile
	kind := ""
	if policy.Target.ReduceToBytes != nil {
		picked = selectReduceToBytes(eligible, *policy.Target.ReduceToBytes)
		kind = reduceKind(*policy.Target.ReduceToBytes)
	} else {
		pct := percentOf(policy)
		picked = selectOldestPercent(eligible, pct)
		kind = percentKind(pct)
	}
	if planDigest(kind, picked) != preview.Digest {
		ignoring := picked
		if policy.Target.ReduceToBytes != nil {
			ignoring = selectReduceToBytes(files, *policy.Target.ReduceToBytes)
		} else {
			ignoring = selectOldestPercent(files, percentOf(policy))
		}
		if planDigest(kind, ignoring) == preview.Digest {
			return CleanupResult{OK: false, Mode: policy.Mode, Error: CodeReferencedHistory}, coded(CodeReferencedHistory, "selected archives are still referenced")
		}
		return CleanupResult{OK: false, Mode: policy.Mode, Error: CodeStalePreview}, coded(CodeStalePreview, "archived files changed since preview")
	}
	req := CleanupRequest{Percent: preview.Percent, Mode: policy.Mode, Digest: preview.Digest, Now: e.now(), Rand: e.Rand, Rename: e.Rename, Remove: e.Remove}
	return applyCleanup(root, lock, picked, req)
}

func percentOf(policy Policy) int {
	if policy.Target.RemoveOldestPercent != nil {
		return *policy.Target.RemoveOldestPercent
	}
	return 0
}

func stampJob(policy Policy, now time.Time, loc *time.Location, outcome JobOutcome) Policy {
	job := PolicyJob{Status: JobIdle, FinishedAt: now.UnixMilli(), LastOutcome: &outcome}
	if !outcome.OK && outcome.Error != "" {
		job.LastError = outcome.Error
	}
	if policy.Job != nil {
		job.StartedAt = policy.Job.StartedAt
		job.Reason = policy.Job.Reason
	}
	policy.Job = &job
	policy.NextRun = NextRunAt(policy, now, loc)
	return policy
}

func (e *Engine) Restore(ctx context.Context, codexHome, id string) (RestoreResult, error) {
	if err := e.acquire("restore"); err != nil {
		codedErr := asError(err)
		return RestoreResult{OK: false, Error: codedErr.Code, Message: codedErr.Message}, codedErr
	}
	defer e.Lock.Release()
	timeout := e.RestoreTimeout
	if timeout <= 0 {
		timeout = DefaultRestoreTimeout
	}
	return Restore(ctx, codexHome, id, timeout)
}

func (e *Engine) ListTrash(codexHome string) (TrashList, error) {
	return ListTrash(codexHome)
}

func (e *Engine) Reconcile(codexHome string) error {
	return ReconcileTrash(codexHome)
}

func ClearStaleRunning(policy Policy, now time.Time) Policy {
	if policy.Job != nil && policy.Job.Status == JobRunning {
		outcome := JobOutcome{OK: false, Error: CodeRestoreWorkerAborted}
		policy.Job.Status = JobIdle
		policy.Job.FinishedAt = now.UnixMilli()
		policy.Job.LastError = CodeRestoreWorkerAborted
		policy.Job.LastOutcome = &outcome
	}
	policy.NextRun = NextRunAt(policy, now, time.Local)
	return policy
}
