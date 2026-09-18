package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentials"
)

type credentialLister interface {
	List() ([]credentials.Ref, error)
}

func credentialsAPIPath(path string) bool {
	switch path {
	case "/api/credentials", "/api/credentials/export", "/api/credentials/history", "/api/credentials/backup":
		return true
	default:
		return false
	}
}

func (h *handler) serveCredentialsAPI(w http.ResponseWriter, r *http.Request) bool {
	if !credentialsAPIPath(r.URL.Path) {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.URL.Path != "/api/credentials" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		writeError(w, http.StatusForbidden, "secrets_export_disabled", "credential export is disabled")
		return true
	}
	if r.Method == http.MethodPost {
		if h.credentials == nil {
			writeError(w, http.StatusServiceUnavailable, "store_unavailable", "credential store is unavailable")
			return true
		}
		var body struct {
			ID     string `json:"id"`
			Secret string `json:"secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "invalid body")
			return true
		}
		ref, err := h.credentials.Put(body.ID, []byte(body.Secret))
		if err != nil {
			writeError(w, http.StatusBadRequest, "store_failed", "credential was not stored")
			return true
		}
		if err := h.publishCredentialRef(ref); err != nil {
			writeError(w, http.StatusInternalServerError, "config_failed", "credential ref was not published")
			return true
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": ref.ID, "source": ref.Source})
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	lister, ok := h.credentials.(credentialLister)
	if !ok || lister == nil {
		writeJSON(w, http.StatusOK, map[string]any{"credentials": []any{}})
		return true
	}
	refs, err := lister.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "credential list unavailable")
		return true
	}
	out := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		out = append(out, map[string]any{
			"id":        ref.ID,
			"source":    ref.Source,
			"available": true,
		})
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (h *handler) publishCredentialRef(ref credentials.Ref) error {
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return nil
	}
	body, err := json.Marshal(ref)
	if err != nil {
		return err
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		return err
	}
	tx.SetSource(config.MutationSource{Class: config.SourceAPI, Detail: "/api/credentials"})
	if err := tx.Set(config.JSONPath("providers", ref.ID, "credentialRef"), body); err != nil {
		return err
	}
	_, err = tx.Commit()
	return err
}

func (h *handler) unpublishCredentialRef(id string) error {
	if h == nil || strings.TrimSpace(h.configPath) == "" || strings.TrimSpace(id) == "" {
		return nil
	}
	tx, err := config.NewTransactionStore(h.configPath, 0).Begin()
	if err != nil {
		return err
	}
	tx.SetSource(config.MutationSource{Class: config.SourceAPI, Detail: "/api/credentials"})
	if err := tx.Delete(config.JSONPath("providers", id, "credentialRef")); err != nil {
		return err
	}
	_, err = tx.Commit()
	return err
}
