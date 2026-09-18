package storage

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type CleanupRequest struct {
	Percent      int
	Mode         string
	Digest       string
	Now          time.Time
	Rand         io.Reader
	Rename       func(oldpath, newpath string) error
	Remove       func(path string) error
	BeforeLock   func()
	AfterLock    func(*stateLock)
	AfterMove    func(*stateLock, classifiedFile)
	BeforeRename func()
	AfterRename  func(moved int) error
	AfterPersist func(moved int) error
	AfterCommit  func() error
}

type CleanupResult struct {
	OK                   bool   `json:"ok"`
	Mode                 string `json:"mode"`
	Count                int    `json:"count"`
	Bytes                int64  `json:"bytes"`
	FreedBytes           int64  `json:"freedBytes,omitempty"`
	RemovedArchivedBytes int64  `json:"removedArchivedBytes,omitempty"`
	TrashDir             string `json:"trashDir,omitempty"`
	Error                string `json:"error,omitempty"`
	Message              string `json:"message,omitempty"`
	Partial              bool   `json:"partial,omitempty"`
}

func PreviewPercent(codexHome string, percent int) (Preview, error) {
	if percent < 1 || percent > 100 {
		return Preview{}, coded(CodeInvalidPercent, "percent must be 1-100")
	}
	root, err := OpenRoot(codexHome)
	if err != nil {
		return Preview{}, err
	}
	files, walkTrunc, err := listArchived(root)
	if err != nil {
		return Preview{}, err
	}
	if walkTrunc {
		return Preview{}, coded(CodeCleanupFailed, "CODEX_HOME is too large to preview safely")
	}
	refs, err := referencedPaths(root)
	if err != nil {
		return Preview{}, asError(err)
	}
	eligible := excludeRefs(files, refs)
	picked := selectOldestPercent(eligible, percent)
	kind := percentKind(percent)
	return previewFromFiles(kind, picked, percent, false), nil
}

func PreviewPolicyTarget(codexHome string, percent int, reduceToBytes *int64) (Preview, error) {
	root, err := OpenRoot(codexHome)
	if err != nil {
		return Preview{}, err
	}
	files, walkTrunc, err := listArchived(root)
	if err != nil {
		return Preview{}, err
	}
	if walkTrunc {
		return Preview{}, coded(CodeCleanupFailed, "CODEX_HOME is too large to preview safely")
	}
	refs, err := referencedPaths(root)
	if err != nil {
		return Preview{}, asError(err)
	}
	eligible := excludeRefs(files, refs)
	var picked []classifiedFile
	kind := ""
	shownPercent := 0
	if reduceToBytes != nil {
		picked = selectReduceToBytes(eligible, *reduceToBytes)
		kind = reduceKind(*reduceToBytes)
	} else {
		if percent < 1 || percent > 100 {
			return Preview{}, coded(CodeInvalidPercent, "percent must be 1-100")
		}
		picked = selectOldestPercent(eligible, percent)
		kind = percentKind(percent)
		shownPercent = percent
	}
	return previewFromFiles(kind, picked, shownPercent, false), nil
}

func ExecuteCleanup(codexHome string, req CleanupRequest) (CleanupResult, error) {
	mode := strings.TrimSpace(req.Mode)
	if mode != ModeQuarantine && mode != ModePermanent {
		return CleanupResult{OK: false, Mode: mode, Error: CodeInvalidMode, Message: "mode must be quarantine or permanent"}, coded(CodeInvalidMode, "mode must be quarantine or permanent")
	}
	digest := strings.TrimSpace(req.Digest)
	if digest == "" || len(digest) > maxDigestLen {
		return CleanupResult{OK: false, Mode: mode, Error: CodeInvalidDigest, Message: "preview digest is missing or invalid"}, coded(CodeInvalidDigest, "preview digest is missing or invalid")
	}
	if req.Percent < 1 || req.Percent > 100 {
		return CleanupResult{OK: false, Mode: mode, Error: CodeInvalidPercent, Message: "percent must be 1-100"}, coded(CodeInvalidPercent, "percent must be 1-100")
	}
	root, err := OpenRoot(codexHome)
	if err != nil {
		return CleanupResult{OK: false, Mode: mode, Error: CodeCleanupFailed}, err
	}
	files, walkTrunc, err := listArchived(root)
	if err != nil {
		return CleanupResult{OK: false, Mode: mode, Error: CodeCleanupFailed}, err
	}
	if walkTrunc {
		return CleanupResult{OK: false, Mode: mode, Error: CodeCleanupFailed, Message: "CODEX_HOME is too large to clean safely"}, coded(CodeCleanupFailed, "CODEX_HOME is too large to clean safely")
	}
	if req.BeforeLock != nil {
		req.BeforeLock()
	}
	lock, err := lockState(root)
	if err != nil {
		out := CleanupResult{OK: false, Mode: mode, Error: CodeCodexBusy}
		if asError(err).Code != CodeCodexBusy {
			out.Error = CodeCleanupFailed
		}
		return out, asError(err)
	}
	defer lock.Close()
	refs, err := lock.Refs()
	if err != nil {
		out := CleanupResult{OK: false, Mode: mode, Error: CodeCodexBusy}
		if asError(err).Code != CodeCodexBusy {
			out.Error = CodeCleanupFailed
		}
		return out, asError(err)
	}
	pending, err := pendingRestoreIDs(root)
	if err != nil {
		return CleanupResult{OK: false, Mode: mode, Error: CodeCleanupFailed}, err
	}
	eligible := excludeRefs(files, refs)
	picked := selectOldestPercent(eligible, req.Percent)
	kind := percentKind(req.Percent)
	currentDigest := planDigest(kind, picked)
	if currentDigest == digest {
		if overlapPending(root, picked, pending) {
			return CleanupResult{OK: false, Mode: mode, Error: CodeRestorePendingOverlap}, coded(CodeRestorePendingOverlap, "selected archives overlap an incomplete trash restore")
		}
		return applyCleanup(root, lock, picked, req)
	}
	ignoring := selectOldestPercent(files, req.Percent)
	if planDigest(kind, ignoring) == digest {
		return CleanupResult{OK: false, Mode: mode, Error: CodeReferencedHistory}, coded(CodeReferencedHistory, "selected archives are still referenced")
	}
	return CleanupResult{OK: false, Mode: mode, Error: CodeStalePreview, Message: "archived files changed since preview"}, coded(CodeStalePreview, "archived files changed since preview")
}

func applyCleanup(root Root, lock *stateLock, files []classifiedFile, req CleanupRequest) (CleanupResult, error) {
	mode := req.Mode
	if len(files) == 0 {
		if err := lock.Commit(); err != nil {
			return CleanupResult{OK: false, Mode: mode, Error: CodeDBReconcileFailed}, err
		}
		return CleanupResult{OK: true, Mode: mode, Count: 0, Bytes: 0}, nil
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	id, _, err := createTrashDir(root, now, req.Rand)
	if err != nil {
		return CleanupResult{OK: false, Mode: mode, Error: CodeFSFailed}, coded(CodeFSFailed, "could not create trash directory")
	}
	trashRel := RelTrashDir(id)
	authorized, err := lock.SnapshotThreads(files)
	if err != nil {
		out := CleanupResult{OK: false, Mode: mode, Error: CodeDBReconcileFailed, TrashDir: trashRel}
		if asError(err).Code == CodeCodexBusy {
			out.Error = CodeCodexBusy
		}
		return out, asError(err).withTrash(trashRel)
	}
	if req.AfterLock != nil {
		req.AfterLock(lock)
	}
	man := plannedManifest(id, mode, now, files)
	man.Threads = authorized
	if err := writeManifest(root, man); err != nil {
		_ = root.removeTreeConfined(path.Join(trashDirName, id))
		return CleanupResult{OK: false, Mode: mode, Error: CodeFSFailed}, coded(CodeFSFailed, "could not persist planned trash manifest")
	}
	moved := make([]classifiedFile, 0, len(files))
	movedBytes := int64(0)
	seenPhys := map[fileID]bool{}
	blocked := false
	for i, file := range files {
		liveRefs, err := lock.Refs()
		if err != nil {
			man.Status = "partial"
			man.Threads = authorized
			_ = writeManifest(root, man)
			return CleanupResult{OK: false, Mode: mode, Count: len(moved), Bytes: movedBytes, TrashDir: trashRel, Error: CodeCodexBusy, Partial: len(moved) > 0}, asError(err).withTrash(trashRel).withPartial(len(moved), movedBytes)
		}
		if liveRefs.HasFile(file) {
			blocked = true
			break
		}
		if err := moveOne(root, id, file, req.Rename, req.BeforeRename, lock); err != nil {
			man.Status = "partial"
			_ = writeManifest(root, man)
			wrapped := asError(err)
			if wrapped.Code != CodeStalePreview && wrapped.Code != CodePathEscape && wrapped.Code != CodeReferencedHistory {
				wrapped = coded(CodeFSFailed, "filesystem cleanup failed")
			}
			partial := CleanupResult{OK: false, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, TrashDir: trashRel, Error: wrapped.Code, Message: wrapped.Message, Partial: true}
			return partial, wrapped.withTrash(trashRel).withPartial(len(moved), movedBytes)
		}
		if req.AfterRename != nil {
			if err := req.AfterRename(i + 1); err != nil {
				man.Status = "partial"
				_ = writeManifest(root, man)
				return CleanupResult{OK: false, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, TrashDir: trashRel, Error: CodeFSFailed, Partial: true}, coded(CodeFSFailed, "could not persist trash progress").withTrash(trashRel).withPartial(len(moved), movedBytes)
			}
		}
		setPhase(&man, file.RelPath, "quarantined")
		moved = append(moved, file)
		if file.ID.OK {
			if !seenPhys[file.ID] {
				seenPhys[file.ID] = true
				movedBytes += file.Bytes
			}
		} else {
			movedBytes += file.Bytes
		}
		if err := writeManifest(root, man); err != nil {
			man.Status = "partial"
			return CleanupResult{OK: false, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, TrashDir: trashRel, Error: CodeFSFailed, Partial: true}, coded(CodeFSFailed, "could not persist trash progress").withTrash(trashRel).withPartial(len(moved), movedBytes)
		}
		if req.AfterPersist != nil {
			if err := req.AfterPersist(i + 1); err != nil {
				man.Status = "partial"
				_ = writeManifest(root, man)
				return CleanupResult{OK: false, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, TrashDir: trashRel, Error: CodeFSFailed, Partial: true}, coded(CodeFSFailed, "could not persist trash progress").withTrash(trashRel).withPartial(len(moved), movedBytes)
			}
		}
		if req.AfterMove != nil {
			req.AfterMove(lock, file)
		}
	}
	movedIDs := threadIDsFor(authorized, moved)
	if err := lock.DeleteThreadIDs(movedIDs); err != nil {
		man.Status = "partial"
		man.Threads = authorized
		_ = writeManifest(root, man)
		out := CleanupResult{OK: false, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, TrashDir: trashRel, Error: CodeDBReconcileFailed, Partial: true}
		if asError(err).Code == CodeCodexBusy {
			out.Error = CodeCodexBusy
		}
		return out, asError(err).withTrash(trashRel).withPartial(len(moved), movedBytes)
	}
	if err := lock.Commit(); err != nil {
		man.Status = "partial"
		_ = writeManifest(root, man)
		return CleanupResult{OK: false, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, TrashDir: trashRel, Error: CodeDBReconcileFailed, Partial: true}, asError(err).withTrash(trashRel).withPartial(len(moved), movedBytes)
	}
	if req.AfterCommit != nil {
		if err := req.AfterCommit(); err != nil {
			return CleanupResult{OK: false, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, TrashDir: trashRel, Error: CodeFSFailed, Partial: true}, coded(CodeFSFailed, "could not persist trash manifest").withTrash(trashRel).withPartial(len(moved), movedBytes)
		}
	}
	man.Status = "complete"
	if blocked || len(moved) != len(files) {
		man.Status = "partial"
	}
	if err := writeManifest(root, man); err != nil {
		return CleanupResult{OK: false, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, TrashDir: trashRel, Error: CodeFSFailed, Partial: true}, coded(CodeFSFailed, "could not persist trash manifest").withTrash(trashRel).withPartial(len(moved), movedBytes)
	}
	if blocked {
		return CleanupResult{OK: false, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, TrashDir: trashRel, Error: CodeReferencedHistory, Partial: len(moved) > 0}, coded(CodeReferencedHistory, "selected archives are still referenced").withTrash(trashRel).withPartial(len(moved), movedBytes)
	}
	if mode == ModePermanent {
		freed, err := permanentDelete(root, id, &man, moved, req.Remove)
		if err != nil {
			man.Mode = ModePermanent
			man.Status = "partial"
			_ = writeManifest(root, man)
			return CleanupResult{OK: false, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, FreedBytes: freed, TrashDir: trashRel, Error: CodeFSFailed, Partial: true}, coded(CodeFSFailed, "permanent delete failed").withTrash(trashRel).withPartial(len(moved), movedBytes)
		}
		if allDeleted(man) {
			if err := root.removeTreeConfined(path.Join(trashDirName, id)); err != nil {
				return CleanupResult{OK: false, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, FreedBytes: freed, TrashDir: trashRel, Error: CodeFSFailed, Partial: true}, err
			}
			return CleanupResult{OK: true, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, FreedBytes: freed}, nil
		}
		_ = writeManifest(root, man)
		return CleanupResult{OK: false, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, FreedBytes: freed, TrashDir: trashRel, Error: CodeFSFailed, Partial: true}, coded(CodeFSFailed, "permanent delete incomplete").withTrash(trashRel)
	}
	return CleanupResult{OK: true, Mode: mode, Count: len(moved), Bytes: movedBytes, RemovedArchivedBytes: movedBytes, TrashDir: trashRel}, nil
}

func moveOne(root Root, id string, file classifiedFile, rename func(string, string) error, before func(), lock *stateLock) error {
	abs, _, info, err := root.ConfineExisting(file.RelPath, false)
	if err != nil {
		return err
	}
	if isDirNotReparse(info) {
		return coded(CodeStalePreview, "candidate is now a directory")
	}
	if fileModeBits(info) != file.Mode || info.Size() != file.Bytes {
		return coded(CodeStalePreview, "candidate changed since preview")
	}
	curID := readFileID(abs, info)
	if file.ID.OK && curID.OK && !file.ID.Equal(curID) {
		return coded(CodeStalePreview, "candidate was replaced")
	}
	destRel := path.Join(trashDirName, id, filesDirName, file.RelPath)
	return root.renameConfined(file.RelPath, destRel, rename, func() error {
		if before != nil {
			before()
		}
		if lock != nil {
			refs, err := lock.Refs()
			if err != nil {
				return err
			}
			if refs.HasFile(file) {
				return coded(CodeReferencedHistory, "selected archives are still referenced")
			}
		}
		return nil
	})
}

func permanentDelete(root Root, id string, man *Manifest, files []classifiedFile, remove func(string) error) (int64, error) {
	freed := int64(0)
	deletedNames := map[fileID]int{}
	for _, file := range files {
		rel := path.Join(trashDirName, id, filesDirName, file.RelPath)
		info, err := func() (os.FileInfo, error) {
			abs, _, inf, e := root.ConfineExisting(rel, false)
			_ = abs
			return inf, e
		}()
		if err != nil {
			if os.IsNotExist(err) {
				setPhase(man, file.RelPath, "deleted")
				continue
			}
			return freed, err
		}
		nlink := uint32(1)
		idv := readFileID(filepath.Join(root.Abs, filepath.FromSlash(rel)), info)
		if idv.OK {
			nlink = idv.Nlink
		}
		if err := root.removeObjectConfined(rel, remove); err != nil {
			return freed, err
		}
		setPhase(man, file.RelPath, "deleted")
		if idv.OK {
			deletedNames[idv]++
			if nlink <= uint32(deletedNames[idv]) {
				freed += file.Bytes
			}
		} else if nlink <= 1 {
			freed += file.Bytes
		}
	}
	return freed, writeManifest(root, *man)
}

func allDeleted(man Manifest) bool {
	for _, file := range man.Files {
		if file.Phase != "deleted" {
			return false
		}
	}
	return len(man.Files) > 0
}

func plannedManifest(id, mode string, now time.Time, files []classifiedFile) Manifest {
	out := Manifest{
		ID:            id,
		Epoch:         id,
		Mode:          mode,
		QuarantinedAt: now.UnixMilli(),
		Status:        "moving",
		FileCount:     len(files),
		Bytes:         candidateBytes(files),
	}
	for _, file := range files {
		out.Files = append(out.Files, ManifestFile{
			RelPath: file.RelPath,
			Bytes:   file.Bytes,
			MtimeMs: file.MtimeMs,
			Mode:    file.Mode,
			Phase:   "planned",
			Nlink:   file.ID.Nlink,
		})
	}
	return out
}

func setPhase(man *Manifest, rel, phase string) {
	for i := range man.Files {
		if man.Files[i].RelPath == rel {
			man.Files[i].Phase = phase
			return
		}
	}
}

func threadIDsFor(rows []map[string]any, moved []classifiedFile) []string {
	want := map[string]bool{}
	for _, file := range moved {
		want[file.RelPath] = true
		want[filepath.ToSlash(file.AbsPath)] = true
	}
	var ids []string
	for _, row := range rows {
		id, _ := row["id"].(string)
		if strings.TrimSpace(id) == "" {
			continue
		}
		match := false
		for _, value := range row {
			text := sqliteText(value)
			if text == "" {
				continue
			}
			if want[text] || want[filepath.ToSlash(text)] {
				match = true
			}
		}
		if match {
			ids = append(ids, id)
		}
	}
	return ids
}

func excludeRefs(files []classifiedFile, refs RefSet) []classifiedFile {
	out := make([]classifiedFile, 0, len(files))
	for _, file := range files {
		if refs.HasFile(file) {
			continue
		}
		out = append(out, file)
	}
	return out
}

func overlapPending(root Root, files []classifiedFile, pending map[string]bool) bool {
	if len(pending) == 0 {
		return false
	}
	want := map[string]bool{}
	for _, file := range files {
		want[file.RelPath] = true
	}
	for id := range pending {
		man, err := loadManifest(root, id)
		if err != nil {
			continue
		}
		for _, file := range man.Files {
			if want[file.RelPath] {
				return true
			}
		}
	}
	return false
}

func percentKind(percent int) string {
	return path.Join("percent", strconv.Itoa(percent))
}

func reduceKind(n int64) string {
	return path.Join("reduce", strconv.FormatInt(n, 10))
}
