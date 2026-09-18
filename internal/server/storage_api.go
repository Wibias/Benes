package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/storage"
)

var (
	errJSONBodyTooLarge   = errors.New("json body exceeds limit")
	errJSONMultipleValues = errors.New("multiple json values")
)

func (h *handler) serveStorageAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/storage" && !strings.HasPrefix(r.URL.Path, "/api/storage/") {
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/api/storage/codex-logs") {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch {
	case r.URL.Path == "/api/storage" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		h.serveStorageReport(w, r)
		return true
	case r.URL.Path == "/api/storage/cleanup/preview" && r.Method == http.MethodPost:
		h.serveStorageCleanupPreview(w, r)
		return true
	case r.URL.Path == "/api/storage/cleanup" && r.Method == http.MethodPost:
		h.serveStorageCleanup(w, r)
		return true
	case r.URL.Path == "/api/storage/trash" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		h.serveStorageTrashList(w, r)
		return true
	case r.URL.Path == "/api/storage/trash/restore" && r.Method == http.MethodPost:
		h.serveStorageRestore(w, r)
		return true
	case r.URL.Path == "/api/storage/cleanup-policy" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		h.serveStoragePolicyGet(w, r)
		return true
	case r.URL.Path == "/api/storage/cleanup-policy" && r.Method == http.MethodPut:
		h.serveStoragePolicyPut(w, r)
		return true
	case r.URL.Path == "/api/storage/cleanup-policy/run" && r.Method == http.MethodPost:
		h.serveStoragePolicyRun(w, r)
		return true
	default:
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost && r.Method != http.MethodPut {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		return false
	}
}

func (h *handler) serveStorageReport(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	home := h.resolveCodexHome()
	if home == "" {
		resolved, err := config.ResolveCodexHome(config.CodexHomeOptions{})
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"codexHome":   "",
				"generatedAt": time.Now().UnixMilli(),
				"total":       map[string]any{"bytes": 0, "fileCount": 0},
				"buckets":     []any{},
				"error":       "scan_failed",
			})
			return
		}
		home = resolved
	}
	report, err := storage.Scan(home)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"codexHome":   home,
			"generatedAt": time.Now().UnixMilli(),
			"total":       map[string]any{"bytes": 0, "fileCount": 0},
			"buckets":     []any{},
			"error":       "scan_failed",
		})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *handler) storageHome() string {
	home := h.resolveCodexHome()
	if home != "" {
		return home
	}
	resolved, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err != nil {
		return ""
	}
	return resolved
}

func (h *handler) engine() *storage.Engine {
	if h.storageEngine == nil {
		h.storageEngine = storage.NewEngine()
	}
	return h.storageEngine
}

func decodeLimitedJSON(r *http.Request, dest any, limit int64) error {
	if limit <= 0 {
		limit = 8 << 10
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(raw)) > limit {
		return errJSONBodyTooLarge
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(dest); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errJSONMultipleValues
		}
		return err
	}
	return nil
}
