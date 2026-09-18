package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/export"
	"github.com/Wibias/Benes/internal/integrations"
)

func (h *handler) serveClientIntegrationsAPI(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path != "/api/client-integrations" && path != "/api/client-integrations/journal" && path != "/api/client-integrations/restore" && !strings.HasPrefix(path, "/api/client-integrations/") {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch {
	case path == "/api/client-integrations":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return true
		}
		h.serveClientIntegrationsList(w)
		return true
	case path == "/api/client-integrations/journal":
		if r.Method != http.MethodGet {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		h.serveClientIntegrationsJournal(w, r)
		return true
	case path == "/api/client-integrations/restore":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		h.serveClientIntegrationsRestore(w, r)
		return true
	default:
		clientID := strings.TrimPrefix(path, "/api/client-integrations/")
		if clientID == "" || strings.Contains(clientID, "/") {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			if r.Method == http.MethodHead {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				return true
			}
			h.serveClientIntegrationStatus(w, clientID)
			return true
		case http.MethodPut:
			h.serveClientIntegrationToggle(w, r, clientID)
			return true
		default:
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
	}
}

func (h *handler) integrationInput(clientID string) (integrations.Input, bool) {
	home := h.benesHome()
	if home == "" {
		if resolved, err := config.ResolvePaths(config.PathOptions{}); err == nil {
			home = resolved.Home
		}
	}
	in := integrations.Input{
		ClientID: clientID,
		Home:     home,
		Store:    integrations.NewStore(filepath.Join(home, "integrations")),
		Models:   h.exportModels(),
		Hostname: "127.0.0.1",
		BaseURL:  "http://127.0.0.1:23100/v1",
	}
	if strings.TrimSpace(h.configPath) != "" {
		if disk, err := config.LoadDiskConfig(h.configPath, 0); err == nil {
			if listener, err := config.ProjectListener(disk); err == nil {
				in.Hostname = listener.Hostname
				in.BaseURL = "http://" + listener.Hostname + ":" + strconv.Itoa(listener.Port) + "/v1"
			}
		}
	}
	return in, home != ""
}

func (h *handler) serveClientIntegrationsList(w http.ResponseWriter) {
	rows := make([]map[string]any, 0, len(export.ClientIDs))
	for _, id := range export.ClientIDs {
		in, ok := h.integrationInput(id)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
			return
		}
		status := integrations.Status(in)
		rows = append(rows, integrationStatusMap(status))
	}
	writeJSON(w, http.StatusOK, map[string]any{"clients": rows})
}

func (h *handler) serveClientIntegrationStatus(w http.ResponseWriter, clientID string) {
	if !knownIntegrationClient(clientID) {
		writeError(w, http.StatusBadRequest, "unknown_client", "unknown client")
		return
	}
	in, ok := h.integrationInput(clientID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	writeJSON(w, http.StatusOK, integrationStatusMap(integrations.Status(in)))
}

func (h *handler) serveClientIntegrationToggle(w http.ResponseWriter, r *http.Request, clientID string) {
	if !knownIntegrationClient(clientID) {
		writeError(w, http.StatusBadRequest, "unknown_client", "unknown client")
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || body.Enabled == nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "enabled must be a boolean")
		return
	}
	in, ok := h.integrationInput(clientID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	var result integrations.Outcome
	if *body.Enabled {
		result = integrations.Apply(in)
	} else {
		result = integrations.Disable(in)
	}
	writeIntegrationOutcome(w, result)
}

func (h *handler) serveClientIntegrationsJournal(w http.ResponseWriter, r *http.Request) {
	in, ok := h.integrationInput("opencode")
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	client := strings.TrimSpace(r.URL.Query().Get("client"))
	if client != "" && !knownIntegrationClient(client) {
		writeError(w, http.StatusBadRequest, "unknown_client", "unknown client")
		return
	}
	ops := in.Store.ListOperations(client)
	newest := map[string]string{}
	for _, op := range ops {
		if _, seen := newest[op.ClientID]; !seen {
			newest[op.ClientID] = op.OpID
		}
	}
	rows := make([]map[string]any, 0, len(ops))
	for _, op := range ops {
		kind, _, _ := in.Store.ReadSnapshot(op)
		current := optionalFileText(op.ConfigPath)
		undoable := kind != "expired" && newest[op.ClientID] == op.OpID && integrations.MatchesResult(op, current)
		rows = append(rows, map[string]any{
			"opId":       op.OpID,
			"clientId":   op.ClientID,
			"kind":       op.Kind,
			"at":         op.At,
			"configPath": op.ConfigPath,
			"snapshot":   kind,
			"undoable":   undoable,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"operations": rows})
}

func (h *handler) serveClientIntegrationsRestore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OpID         string `json:"opId"`
		ConfirmDrift bool   `json:"confirmDrift"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || strings.TrimSpace(body.OpID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_op_id", "opId must be a non-empty string")
		return
	}
	in, ok := h.integrationInput("opencode")
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	in.OpID = strings.TrimSpace(body.OpID)
	in.ConfirmDrift = body.ConfirmDrift
	writeIntegrationOutcome(w, integrations.Restore(in))
}

func writeIntegrationOutcome(w http.ResponseWriter, result integrations.Outcome) {
	if result.OK {
		writeJSON(w, http.StatusOK, result)
		return
	}
	status := http.StatusBadRequest
	code := "integration_mutation_failed"
	switch result.Reason {
	case "not_installed":
		status = http.StatusNotFound
		code = "not_installed"
	case "conflict", "drift_requires_confirm":
		status = http.StatusConflict
		code = result.Reason
	case "snapshot_expired":
		status = http.StatusGone
		code = "snapshot_expired"
	case "not_found":
		status = http.StatusNotFound
		code = "integration_operation_not_found"
	case "unsafe", "non_loopback", "write_failed":
		status = http.StatusBadRequest
		code = result.Reason
	}
	writeJSON(w, status, map[string]any{
		"error":    result.Message,
		"code":     code,
		"ok":       false,
		"clientId": result.ClientID,
		"state":    result.State,
		"reason":   result.Reason,
		"message":  result.Message,
	})
}

func integrationStatusMap(status integrations.Outcome) map[string]any {
	return map[string]any{
		"clientId":      status.ClientID,
		"state":         status.State,
		"installed":     status.Installed,
		"configPath":    status.ConfigPath,
		"detectDir":     status.DetectDir,
		"appliedAt":     status.AppliedAt,
		"lastOpId":      status.LastOpID,
		"reason":        status.Reason,
		"snapshotCount": status.SnapshotCount,
	}
}

func knownIntegrationClient(id string) bool {
	for _, item := range export.ClientIDs {
		if item == id {
			return true
		}
	}
	return false
}

func optionalFileText(path string) *string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(raw)
	return &text
}
