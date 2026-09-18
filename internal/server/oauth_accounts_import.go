package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Wibias/Benes/internal/oauth/accountimport"
)

func (h *handler) serveOAuthAccountsImport(w http.ResponseWriter, r *http.Request) bool {
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	limited := http.MaxBytesReader(w, r.Body, accountimport.MaxRequestBytes)
	var body struct {
		Provider string `json:"provider"`
		Format   string `json:"format"`
		Document any    `json:"document"`
	}
	dec := json.NewDecoder(limited)
	if err := dec.Decode(&body); err != nil {
		if ctx.Err() != nil {
			writeJSON(w, http.StatusRequestTimeout, map[string]any{"code": "import_cancelled"})
			return true
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": "invalid_document"})
		return true
	}
	path := h.oauthAuthStorePath()
	if path == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"code": "persist_failed"})
		return true
	}
	out := accountimport.Import(ctx, body.Provider, body.Format, body.Document, path, nil)
	if !out.OK {
		writeJSON(w, out.Status, map[string]any{"code": out.Code})
		return true
	}
	writeJSON(w, http.StatusOK, out.Result)
	return true
}
