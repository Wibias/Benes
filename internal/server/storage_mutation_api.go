package server

import (
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/storage"
)

func (h *handler) serveStorageCleanupPreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Percent int `json:"percent"`
	}
	if err := decodeLimitedJSON(r, &body, 4<<10); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": storage.CodeInvalidPercent, "message": "invalid JSON body"})
		return
	}
	home := h.storageHome()
	if home == "" {
		writeJSON(w, http.StatusOK, map[string]any{"error": storage.CodeCleanupFailed, "message": "CODEX_HOME is unavailable"})
		return
	}
	preview, err := h.engine().Preview(home, body.Percent)
	if err != nil {
		e := storageErr(err)
		writeJSON(w, statusFor(e.Code), map[string]any{"error": e.Code, "message": e.Message})
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (h *handler) serveStorageCleanup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Percent int    `json:"percent"`
		Mode    string `json:"mode"`
		Digest  string `json:"digest"`
	}
	if err := decodeLimitedJSON(r, &body, 8<<10); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": storage.CodeInvalidDigest, "message": "invalid JSON body"})
		return
	}
	home := h.storageHome()
	if home == "" {
		writeJSON(w, http.StatusOK, storage.CleanupResult{OK: false, Mode: body.Mode, Error: storage.CodeCleanupFailed, Message: "CODEX_HOME is unavailable"})
		return
	}
	result, err := h.engine().Cleanup(home, storage.CleanupRequest{
		Percent: body.Percent,
		Mode:    strings.TrimSpace(body.Mode),
		Digest:  strings.TrimSpace(body.Digest),
	})
	if err != nil {
		status := statusFor(storageErr(err).Code)
		if result.Error == "" {
			e := storageErr(err)
			result.OK = false
			result.Error = e.Code
			result.Message = e.Message
			result.TrashDir = e.TrashDir
			result.Partial = e.Partial
			if e.Count > 0 {
				result.Count = e.Count
				result.Bytes = e.Bytes
			}
		}
		writeJSON(w, status, result)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *handler) serveStorageTrashList(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	home := h.storageHome()
	if home == "" {
		writeJSON(w, http.StatusOK, map[string]any{"entries": []any{}})
		return
	}
	if recErr := h.engine().Reconcile(home); recErr != nil {
		list, err := h.engine().ListTrash(home)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"entries": []any{}, "recoveryNeeded": []any{map[string]any{"status": "unreadable", "error": storage.CodeFSFailed}}})
			return
		}
		if len(list.RecoveryNeeded) == 0 {
			list.RecoveryNeeded = []storage.TrashRecovery{{Status: "incomplete", Error: storage.CodeFSFailed}}
		}
		writeJSON(w, http.StatusOK, list)
		return
	}
	list, err := h.engine().ListTrash(home)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"entries": []any{}, "recoveryNeeded": []any{map[string]any{"status": "unreadable", "error": storage.CodeFSFailed}}})
		return
	}
	if list.Entries == nil {
		list.Entries = []storage.TrashEntry{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (h *handler) serveStorageRestore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
	}
	if err := decodeLimitedJSON(r, &body, 4<<10); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": storage.CodeInvalidTrash, "message": "invalid JSON body"})
		return
	}
	home := h.storageHome()
	if home == "" {
		writeJSON(w, http.StatusOK, storage.RestoreResult{OK: false, Error: storage.CodeRestoreFailed, Message: "CODEX_HOME is unavailable"})
		return
	}
	result, err := h.engine().Restore(r.Context(), home, strings.TrimSpace(body.ID))
	if err != nil {
		if result.Error == "" {
			e := storageErr(err)
			result.OK = false
			result.Error = e.Code
			result.Message = e.Message
			result.TrashDir = e.TrashDir
			result.Partial = e.Partial
		}
		writeJSON(w, statusFor(result.Error), result)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func storageErr(err error) *storage.Error {
	if e, ok := err.(*storage.Error); ok && e != nil {
		return e
	}
	return &storage.Error{Code: storage.CodeCleanupFailed, Message: "storage operation failed"}
}

func statusFor(code string) int {
	switch code {
	case storage.CodeInvalidDigest, storage.CodeInvalidMode, storage.CodeInvalidPercent, storage.CodeInvalidTrash, storage.CodeInvalidPolicy, storage.CodePathEscape:
		return http.StatusBadRequest
	case storage.CodeCodexBusy, storage.CodeStorageMutationBusy, storage.CodeRestorePendingOverlap, storage.CodeAlreadyRunning:
		return http.StatusConflict
	case storage.CodeMissingTrash:
		return http.StatusNotFound
	case storage.CodeStalePreview, storage.CodeReferencedHistory, storage.CodeDestExists:
		return http.StatusConflict
	default:
		return http.StatusOK
	}
}
