package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/codexlogguard"
	"github.com/Wibias/Benes/internal/config"
)

func (h *handler) serveStorageCodexLogsAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/storage/codex-logs" && !strings.HasPrefix(r.URL.Path, "/api/storage/codex-logs/") {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	home := h.resolveCodexHome()
	desired := h.logGuardDesiredMode()
	switch {
	case r.URL.Path == "/api/storage/codex-logs" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return true
		}
		writeJSON(w, http.StatusOK, codexlogguard.Inspect(home, desired))
		return true
	case r.URL.Path == "/api/storage/codex-logs/protect" && r.Method == http.MethodPost:
		var body struct {
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return true
		}
		mode := codexlogguard.Mode(strings.TrimSpace(body.Mode))
		if mode != codexlogguard.ModeCompat && mode != codexlogguard.ModeQuiet {
			writeError(w, http.StatusBadRequest, "invalid_body", "mode must be compat or quiet")
			return true
		}
		if err := h.persistLogGuardMode(mode); err != nil {
			writeError(w, http.StatusInternalServerError, "config_write_failed", "mode could not be stored")
			return true
		}
		result := codexlogguard.Protect(home, mode)
		status := http.StatusOK
		if !result.OK {
			status = http.StatusConflict
		}
		writeJSON(w, status, result)
		return true
	case r.URL.Path == "/api/storage/codex-logs/unprotect" && r.Method == http.MethodPost:
		_ = h.persistLogGuardMode(codexlogguard.ModeOff)
		result := codexlogguard.Protect(home, codexlogguard.ModeOff)
		status := http.StatusOK
		if !result.OK {
			status = http.StatusConflict
		}
		writeJSON(w, status, result)
		return true
	case r.URL.Path == "/api/storage/codex-logs/repair" && r.Method == http.MethodPost:
		result := codexlogguard.Protect(home, desired)
		status := http.StatusOK
		if !result.OK {
			status = http.StatusConflict
		}
		writeJSON(w, status, result)
		return true
	case r.URL.Path == "/api/storage/codex-logs/compact" && r.Method == http.MethodPost:
		result := codexlogguard.Compact(home)
		status := http.StatusOK
		if !result.OK {
			status = http.StatusConflict
		}
		writeJSON(w, status, result)
		return true
	default:
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		return false
	}
}

func (h *handler) logGuardDesiredMode() codexlogguard.Mode {
	root := h.loadConfigRoot()
	raw, _ := root["codexLogGuard"].(map[string]any)
	mode, _ := raw["mode"].(string)
	if mode == "compat" || mode == "quiet" {
		return codexlogguard.Mode(mode)
	}
	return codexlogguard.ModeOff
}

func (h *handler) persistLogGuardMode(mode codexlogguard.Mode) error {
	if strings.TrimSpace(h.configPath) == "" {
		return nil
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(map[string]any{"mode": mode})
	if err != nil {
		return err
	}
	if err := tx.Set(config.JSONPath("codexLogGuard"), raw); err != nil {
		return err
	}
	_, err = tx.Commit()
	return err
}
