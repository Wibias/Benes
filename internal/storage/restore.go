package storage

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"time"
)

type RestoreResult struct {
	OK              bool   `json:"ok"`
	Count           int    `json:"count"`
	Bytes           int64  `json:"bytes"`
	AlreadyRestored int    `json:"alreadyRestored,omitempty"`
	TotalCount      int    `json:"totalCount,omitempty"`
	TotalBytes      int64  `json:"totalBytes,omitempty"`
	TrashDir        string `json:"trashDir,omitempty"`
	Error           string `json:"error,omitempty"`
	Message         string `json:"message,omitempty"`
	Partial         bool   `json:"partial,omitempty"`
}

const DefaultRestoreTimeout = 10 * time.Minute

func Restore(ctx context.Context, codexHome, id string, timeout time.Duration) (RestoreResult, error) {
	return restore(ctx, codexHome, id, timeout, nil, nil)
}

func restore(ctx context.Context, codexHome, id string, timeout time.Duration, rename func(string, string) error, before func() error) (RestoreResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = DefaultRestoreTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if !validTrashID(id) {
		return RestoreResult{OK: false, Error: CodeInvalidTrash}, coded(CodeInvalidTrash, "trash entry id is missing or invalid")
	}
	root, err := OpenRoot(codexHome)
	if err != nil {
		return RestoreResult{OK: false, Error: CodeRestoreFailed}, err
	}
	man, err := loadManifest(root, id)
	if err != nil {
		e := asError(err)
		return RestoreResult{OK: false, Error: e.Code, Message: e.Message}, e
	}
	trashRel := RelTrashDir(id)
	if man.Mode == ModePermanent && allDeleted(man) {
		return RestoreResult{OK: false, Error: CodeMissingTrash, TrashDir: trashRel}, coded(CodeMissingTrash, "trash entry was not found")
	}
	lock, err := lockState(root)
	if err != nil {
		e := asError(err)
		return RestoreResult{OK: false, Error: e.Code, TrashDir: trashRel}, e
	}
	defer lock.Close()
	man.Status = "restoring"
	if err := writeManifest(root, man); err != nil {
		return RestoreResult{OK: false, Error: CodeFSFailed, TrashDir: trashRel}, coded(CodeFSFailed, "could not persist restore state").withTrash(trashRel)
	}
	thisCount := 0
	thisBytes := int64(0)
	already := 0
	totalBytes := int64(0)
	for _, file := range man.Files {
		totalBytes += file.Bytes
		if file.Phase == "deleted" {
			continue
		}
		if err := ctx.Err(); err != nil {
			man.Status = "partial"
			_ = writeManifest(root, man)
			out := RestoreResult{OK: false, Count: thisCount, Bytes: thisBytes, AlreadyRestored: already, TotalCount: len(man.Files), TotalBytes: totalBytes, TrashDir: trashRel, Partial: true}
			if ctx.Err() == context.DeadlineExceeded {
				out.Error = CodeRestoreWorkerTimeout
				return out, coded(CodeRestoreWorkerTimeout, "restore took too long").withTrash(trashRel).withPartial(thisCount, thisBytes)
			}
			out.Error = CodeRestoreWorkerAborted
			return out, coded(CodeRestoreWorkerAborted, "restore was cancelled").withTrash(trashRel).withPartial(thisCount, thisBytes)
		}
		done, err := restoreOne(root, id, file, rename, before)
		if err != nil {
			man.Status = "partial"
			_ = writeManifest(root, man)
			e := asError(err)
			if e.Code == CodePathEscape {
				e = coded(CodeInvalidTrash, "trash manifest path is invalid")
			}
			out := RestoreResult{OK: false, Count: thisCount, Bytes: thisBytes, AlreadyRestored: already, TotalCount: len(man.Files), TotalBytes: totalBytes, TrashDir: trashRel, Error: e.Code, Message: e.Message, Partial: thisCount > 0 || already > 0}
			return out, e.withTrash(trashRel).withPartial(thisCount, thisBytes)
		}
		if done == "already" {
			already++
			continue
		}
		setPhase(&man, file.RelPath, "restored")
		if err := writeManifest(root, man); err != nil {
			return RestoreResult{OK: false, Count: thisCount, Bytes: thisBytes, AlreadyRestored: already, TrashDir: trashRel, Error: CodeFSFailed, Partial: true}, coded(CodeFSFailed, "could not persist restore progress").withTrash(trashRel)
		}
		thisCount++
		thisBytes += file.Bytes
	}
	if err := lock.RestoreThreads(man.Threads); err != nil {
		e := asError(err)
		if e.Code != CodeCodexBusy {
			e = coded(CodeDBReconcileFailed, "could not restore Codex state database rows")
		}
		man.Status = "partial"
		_ = writeManifest(root, man)
		return RestoreResult{OK: false, Count: thisCount, Bytes: thisBytes, AlreadyRestored: already, TotalCount: len(man.Files), TotalBytes: totalBytes, TrashDir: trashRel, Error: e.Code, Partial: true}, e.withTrash(trashRel).withPartial(thisCount, thisBytes)
	}
	if err := lock.Commit(); err != nil {
		man.Status = "partial"
		_ = writeManifest(root, man)
		return RestoreResult{OK: false, Count: thisCount, Bytes: thisBytes, AlreadyRestored: already, TrashDir: trashRel, Error: CodeDBReconcileFailed, Partial: true}, asError(err).withTrash(trashRel)
	}
	if err := root.removeTreeConfined(path.Join(trashDirName, id)); err != nil {
		return RestoreResult{OK: false, Count: thisCount, Bytes: thisBytes, AlreadyRestored: already, TrashDir: trashRel, Error: CodeFSFailed, Partial: true}, coded(CodeFSFailed, "could not remove completed trash entry").withTrash(trashRel)
	}
	return RestoreResult{OK: true, Count: thisCount, Bytes: thisBytes, AlreadyRestored: already, TotalCount: len(man.Files), TotalBytes: totalBytes}, nil
}

func restoreOne(root Root, id string, file ManifestFile, rename func(string, string) error, before func() error) (string, error) {
	rel, err := normalizeRel(file.RelPath)
	if err != nil || !archivedPrefix(rel) {
		return "", coded(CodeInvalidTrash, "trash manifest path is invalid")
	}
	srcRel := path.Join(trashDirName, id, filesDirName, rel)
	srcAbs, _, err := root.walkRel(srcRel, false, true)
	if err != nil && !os.IsNotExist(err) && asError(err).Code != CodePathEscape {
		if !os.IsNotExist(err) && err != errInvalidRel {
			// srcRel/srcAbs were validated by validTrashID, normalizeRel and Root.walkRel confinement.
			// codeql[go/path-injection]
			if _, statErr := os.Lstat(filepath.Join(root.Abs, filepath.FromSlash(srcRel))); statErr != nil && !os.IsNotExist(statErr) {
				return "", err
			}
		}
	}
	srcExists := false
	if srcAbs != "" {
		// srcRel/srcAbs were validated by validTrashID, normalizeRel and Root.walkRel confinement.
		// codeql[go/path-injection]
		if _, statErr := os.Lstat(srcAbs); statErr == nil {
			srcExists = true
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		}
	}
	destAbs, _, destWalkErr := root.walkRel(rel, false, true)
	if destWalkErr != nil && asError(destWalkErr).Code == CodePathEscape {
		return "", destWalkErr
	}
	destExists := false
	if destAbs != "" {
		if _, statErr := os.Lstat(destAbs); statErr == nil {
			destExists = true
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		}
	}
	if file.Phase == "restored" {
		if destExists {
			return "already", nil
		}
		if srcExists {
			return "", coded(CodeMissingTrash, "restore progress says restored but destination is missing")
		}
		return "", coded(CodeMissingTrash, "restored path is missing from both trash and destination")
	}
	if destExists {
		return "", coded(CodeDestExists, "restore destination already exists")
	}
	if !srcExists {
		return "", coded(CodeMissingTrash, "trash source file is missing")
	}
	if err := root.renameConfined(srcRel, rel, rename, before); err != nil {
		code := asError(err).Code
		if code == CodePathEscape {
			return "", coded(CodeInvalidTrash, "trash manifest path is invalid")
		}
		if code == CodeDestExists {
			return "", err
		}
		return "", coded(CodeFSFailed, "filesystem restore failed")
	}
	return "moved", nil
}

func ReconcileTrash(codexHome string) error {
	root, err := OpenRoot(codexHome)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	info, err := os.Lstat(root.TrashDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !isDirNotReparse(info) {
		return nil
	}
	entries, err := os.ReadDir(root.TrashDir())
	if err != nil {
		return err
	}
	var first error
	for _, entry := range entries {
		if !validTrashID(entry.Name()) {
			continue
		}
		if err := reconcileOne(root, entry.Name()); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func reconcileOne(root Root, id string) error {
	man, err := loadManifest(root, id)
	if err != nil {
		return err
	}
	changed := false
	for i, file := range man.Files {
		srcRel := path.Join(trashDirName, id, filesDirName, file.RelPath)
		srcAbs := filepath.Join(root.Abs, filepath.FromSlash(srcRel))
		destAbs := filepath.Join(root.Abs, filepath.FromSlash(file.RelPath))
		_, srcStat := os.Lstat(srcAbs)
		_, destStat := os.Lstat(destAbs)
		srcOK := srcStat == nil
		destOK := destStat == nil
		phase := file.Phase
		if srcOK && (phase == "" || phase == "planned") {
			man.Files[i].Phase = "quarantined"
			changed = true
		}
		if !srcOK && destOK && phase != "restored" && phase != "deleted" {
			man.Files[i].Phase = "restored"
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if man.Status == "moving" || man.Status == "restoring" {
		man.Status = "partial"
	}
	return writeManifest(root, man)
}
