package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/sidecar/fabric"
)

const (
	fabricKindLifecycle = "lifecycle"
	fabricCapLifecycle  = "lifecycle"
	fabricCapExecution  = "execution"
	fabricCapHandoffAPI = "single_child_handoff"
)

func (h *handler) serveFabricAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/fabric/status" && !strings.HasPrefix(r.URL.Path, "/api/fabric/") {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.URL.Path == "/api/fabric/status" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		enabled, err := h.fabricEnabledState()
		if err != nil {
			writeFabricConfigError(w)
			return true
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return true
		}
		writeJSON(w, http.StatusOK, fabricStatusBody(enabled))
		return true
	}
	enabled, err := h.fabricEnabledState()
	if err != nil {
		writeFabricConfigError(w)
		return true
	}
	if !enabled {
		writeError(w, http.StatusConflict, "fabric_disabled", "fabric is disabled")
		return true
	}
	repo, err := h.fabricRepo()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "fabric_unreadable", "fabric store could not be opened")
		return true
	}
	switch {
	case r.URL.Path == "/api/fabric/tasks" && r.Method == http.MethodGet:
		list, err := repo.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "fabric_unreadable", "fabric tasks could not be listed")
			return true
		}
		if list == nil {
			list = []fabric.TaskSummary{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"tasks": list})
		return true
	case r.URL.Path == "/api/fabric/tasks" && r.Method == http.MethodPost:
		var body struct {
			Title string `json:"title"`
			Goal  string `json:"goal"`
		}
		if err := decodeExactJSON(r.Body, 16<<10, &body); err != nil {
			status, code, message, _ := fabricJSONError(err)
			writeError(w, status, code, message)
			return true
		}
		id, err := repo.Create(fabric.CreateTaskInput{Title: body.Title, Goal: body.Goal})
		if err != nil {
			writeFabricError(w, err)
			return true
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "success": true})
		return true
	case strings.HasPrefix(r.URL.Path, "/api/fabric/tasks/"):
		rest := strings.TrimPrefix(r.URL.Path, "/api/fabric/tasks/")
		id, action, _ := strings.Cut(rest, "/")
		if id == "" {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		if strings.HasPrefix(action, "runs/") {
			return h.serveFabricRunResult(w, r, id, strings.TrimPrefix(action, "runs/"))
		}
		if strings.Contains(action, "/") {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		if action == "" {
			switch r.Method {
			case http.MethodGet:
				detail, err := repo.Get(id)
				if err != nil {
					writeFabricError(w, err)
					return true
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"summary":    detail.Summary,
					"projection": detail.Projection,
					"timeline":   detail.Timeline,
				})
				return true
			case http.MethodDelete:
				if err := repo.Delete(id); err != nil {
					writeFabricError(w, err)
					return true
				}
				writeJSON(w, http.StatusOK, map[string]any{"success": true})
				return true
			default:
				writeError(w, http.StatusNotFound, "not_found", "route not found")
				return true
			}
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		switch action {
		case "start":
			// Lifecycle mark only — does not launch a model or worker.
			owner, ok := readFabricOwner(w, r)
			if !ok {
				return true
			}
			if err := repo.MarkStarted(id, owner); err != nil {
				writeFabricError(w, err)
				return true
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": true})
			return true
		case "close":
			// Lifecycle mark only — terminal task closure, not worker stop.
			owner, ok := readFabricOwner(w, r)
			if !ok {
				return true
			}
			if err := repo.MarkClosed(id, owner); err != nil {
				writeFabricError(w, err)
				return true
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": true})
			return true
		case "execute":
			return h.serveFabricExecute(w, r, repo, id)
		case "cancel":
			identity, precondition, err := readFabricCancel(w, r, repo, id)
			if err != nil {
				return true
			}
			if h.fabricRuntime != nil {
				if precondition {
					detail, getErr := repo.Get(id)
					if getErr == nil {
						run := fabricActiveIdentity(detail)
						if run.RunID != "" {
							cancelClaim := fabric.RunIdentity{
								TaskID: id,
								RunID:  run.RunID,
								Owner:  identity.ExpectedOwner,
								Fence:  identity.ExpectedFencingToken,
							}
							switch h.fabricRuntime.cancelExpected(id, cancelClaim) {
							case fabricCancelOK:
								writeJSON(w, http.StatusOK, map[string]any{"success": true})
								return true
							case fabricCancelPersistFailed:
								// Dual storage failure: live cancel ran but durable
								// terminal could not persist. Prefer 5xx persistence
								// semantics — never success and never fencing 409.
								writeError(w, http.StatusInternalServerError, "fabric_unreadable", "fabric request could not be completed")
								return true
							case fabricCancelFencing:
								if h.fabricRuntime.hasActive(id, run.RunID) {
									writeError(w, http.StatusConflict, "invalid_transition", "fencing mismatch")
									return true
								}
							default:
								if h.fabricRuntime.hasActive(id, run.RunID) {
									writeError(w, http.StatusConflict, "invalid_transition", "fencing mismatch")
									return true
								}
							}
						}
					}
				} else {
					switch h.fabricRuntime.cancelCurrent(id) {
					case fabricCancelOK:
						writeJSON(w, http.StatusOK, map[string]any{"success": true})
						return true
					case fabricCancelPersistFailed:
						writeError(w, http.StatusInternalServerError, "fabric_unreadable", "fabric request could not be completed")
						return true
					}
				}
			}
			if err := repo.CancelTask(id, identity); err != nil {
				writeFabricError(w, err)
				return true
			}
			writeJSON(w, http.StatusOK, map[string]any{"success": true})
			return true
		default:
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

func (h *handler) serveFabricExecute(w http.ResponseWriter, r *http.Request, repo *fabric.Repo, taskID string) bool {
	var body struct {
		Owner      string `json:"owner"`
		Model      string `json:"model"`
		Input      string `json:"input"`
		Delegation *struct {
			Model string `json:"model"`
		} `json:"delegation"`
	}
	err := decodeExactJSON(r.Body, 1<<20, &body)
	if err != nil && !errors.Is(err, io.EOF) {
		status, code, message, _ := fabricJSONError(err)
		writeError(w, status, code, message)
		return true
	}
	owner := strings.TrimSpace(body.Owner)
	if owner == "" {
		owner = "operator"
	}
	model := strings.TrimSpace(body.Model)
	if model == "" {
		writeError(w, http.StatusBadRequest, "invalid_task", "model is required")
		return true
	}
	delegationModel := ""
	if body.Delegation != nil {
		delegationModel = strings.TrimSpace(body.Delegation.Model)
		if delegationModel == "" {
			writeError(w, http.StatusBadRequest, "invalid_task", "delegation.model is required when delegation is set")
			return true
		}
	}
	if h.fabricRuntime == nil {
		writeError(w, http.StatusInternalServerError, "fabric_unreadable", "fabric runtime unavailable")
		return true
	}
	// Parse/admission uses the request; worker must NOT inherit r.Context().
	_ = r.Context()
	result, handle, err := h.fabricRuntime.execute(repo, taskID, owner, model, body.Input, delegationModel)
	if err != nil {
		writeFabricError(w, err)
		return true
	}
	// Safe IDs/status only — never echo prompts or model outputs.
	writeJSON(w, http.StatusAccepted, map[string]any{
		"success":      true,
		"runId":        result.RunID,
		"taskId":       result.TaskID,
		"owner":        result.Owner,
		"fencingToken": result.Fence,
		"status":       result.Status,
		"model":        result.Model,
		"resultHandle": handle,
	})
	return true
}

func fabricActiveIdentity(detail *fabric.TaskDetail) fabric.RunIdentity {
	if detail == nil {
		return fabric.RunIdentity{}
	}
	var id fabric.RunIdentity
	for _, ev := range detail.Events {
		pl := map[string]any{}
		if len(ev.Payload) > 0 {
			_ = json.Unmarshal(ev.Payload, &pl)
		}
		switch ev.EventType {
		case fabric.EventRunStarted:
			runID, _ := pl["run_id"].(string)
			owner, _ := pl["owner"].(string)
			if owner == "" {
				owner = ev.RuntimeSessionID
			}
			fence := 0
			switch v := pl["fencing_token"].(type) {
			case float64:
				fence = int(v)
			}
			id = fabric.RunIdentity{RunID: runID, TaskID: ev.TaskID, Owner: owner, Fence: fence}
		case fabric.EventChildRunStarted:
			owner, _ := pl["owner"].(string)
			fence := 0
			switch v := pl["fencing_token"].(type) {
			case float64:
				fence = int(v)
			}
			if id.RunID != "" && owner != "" && fence > 0 {
				id.Owner = owner
				id.Fence = fence
			}
		case fabric.EventHandoffCommitted:
			owner, _ := pl["new_owner"].(string)
			if owner == "" {
				owner, _ = pl["newOwner"].(string)
			}
			fence := 0
			switch v := pl["fencing_token"].(type) {
			case float64:
				fence = int(v)
			}
			if id.RunID != "" && owner != "" && fence > 0 {
				id.Owner = owner
				id.Fence = fence
			}
		case fabric.EventChildRunCompleted, fabric.EventChildRunFailed, fabric.EventChildRunCancelled, fabric.EventChildRunInterrupted:
			owner, _ := pl["owner"].(string)
			_ = owner
		case fabric.EventRunCompleted, fabric.EventRunFailed, fabric.EventRunCancelled, fabric.EventRunInterrupted:
			id = fabric.RunIdentity{}
		}
	}
	// Prefer projection current owner/fence when a primary run is active (covers return-to-primary).
	if id.RunID != "" && detail.Projection.CurrentOwner != "" && detail.Projection.FencingToken > 0 {
		id.Owner = detail.Projection.CurrentOwner
		id.Fence = detail.Projection.FencingToken
	}
	return id
}

func fabricStatusBody(enabled bool) map[string]any {
	return map[string]any{
		"enabled":       enabled,
		"kind":          fabricKindLifecycle,
		"capabilities":  []string{fabricCapLifecycle, fabricCapExecution, fabricCapHandoffAPI},
		"schemaVersion": fabric.SchemaVersion,
	}
}

func readFabricOwner(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Owner string `json:"owner"`
	}
	err := decodeExactJSON(r.Body, 1<<16, &body)
	if err != nil && !errors.Is(err, io.EOF) {
		status, code, message, _ := fabricJSONError(err)
		writeError(w, status, code, message)
		return "", false
	}
	owner := strings.TrimSpace(body.Owner)
	if owner == "" {
		owner = "operator"
	}
	return owner, true
}

func (h *handler) serveFabricRunResult(w http.ResponseWriter, r *http.Request, taskID, rest string) bool {
	runID, suffix, _ := strings.Cut(rest, "/")
	if runID == "" || suffix != "result" || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if h.fabricRuntime == nil {
		writeError(w, http.StatusNotFound, "result_unavailable", "result store unavailable")
		return true
	}
	res, ok := h.fabricRuntime.resultByRun(taskID, runID)
	if !ok {
		writeError(w, http.StatusNotFound, "result_unavailable", "result not available")
		return true
	}
	// Exact taskID+runID required; handle-only lookup must not bypass task isolation.
	if res.TaskID != taskID || res.RunID != runID {
		writeError(w, http.StatusNotFound, "result_unavailable", "result not available")
		return true
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      true,
		"handle":       res.Handle,
		"runId":        res.RunID,
		"taskId":       res.TaskID,
		"status":       res.Status,
		"output":       res.Output,
		"provider":     res.Provider,
		"model":        res.Model,
		"requestId":    res.RequestID,
		"truncated":    res.Truncated,
		"expiresAt":    res.ExpiresAt.UTC().Format(time.RFC3339),
		"availability": "memory-only; expires by TTL or process restart",
	})
	return true
}

func writeFabricError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fabric.ErrNotFound):
		writeError(w, http.StatusNotFound, "task_not_found", "unknown fabric task")
	case errors.Is(err, fabric.ErrActiveRun):
		writeError(w, http.StatusConflict, "invalid_transition", "cannot delete a started task")
	case errors.Is(err, fabric.ErrRemoved):
		writeError(w, http.StatusConflict, "invalid_transition", "task has been removed")
	default:
		var fe *fabric.Error
		if errors.As(err, &fe) {
			switch fe.Code {
			case fabric.CodeInvalidTask:
				writeError(w, http.StatusBadRequest, fabric.CodeInvalidTask, fe.Msg)
			case fabric.CodeInvalidTransition:
				writeError(w, http.StatusConflict, fabric.CodeInvalidTransition, fe.Msg)
			case fabric.CodeCapacityExceeded:
				writeError(w, http.StatusConflict, fabric.CodeCapacityExceeded, fe.Msg)
			default:
				writeError(w, http.StatusBadRequest, "invalid_task", "invalid fabric task")
			}
			return
		}
		writeError(w, http.StatusInternalServerError, "fabric_unreadable", "fabric request could not be completed")
	}
}

func writeFabricConfigError(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "config_unreadable", "fabric settings could not be read")
}

func (h *handler) fabricEnabledState() (bool, error) {
	if h == nil || strings.TrimSpace(h.configPath) == "" {
		return false, nil
	}
	disk, err := config.LoadDiskConfig(h.configPath, 0)
	if err != nil {
		return false, err
	}
	var root struct {
		Fabric struct {
			Enabled bool `json:"enabled"`
		} `json:"fabric"`
	}
	if err := json.Unmarshal(disk.Raw, &root); err != nil {
		return false, err
	}
	return root.Fabric.Enabled, nil
}

func (h *handler) fabricEnabled() bool {
	enabled, err := h.fabricEnabledState()
	return err == nil && enabled
}

var fabricRepos sync.Map

func (h *handler) fabricRepo() (*fabric.Repo, error) {
	dir := filepath.Join(filepath.Dir(h.configPath), "fabric")
	if cached, ok := fabricRepos.Load(dir); ok {
		repo := cached.(*fabric.Repo)
		if h.fabricRuntime != nil {
			h.fabricRuntime.ensureRecovered(repo)
		}
		return repo, nil
	}
	repo := fabric.New(dir)
	actual, _ := fabricRepos.LoadOrStore(dir, repo)
	repo = actual.(*fabric.Repo)
	if h.fabricRuntime != nil {
		h.fabricRuntime.ensureRecovered(repo)
	}
	return repo, nil
}

// readFabricCancel parses cancel preconditions.
// When expectedOwner+expectedFencingToken are both supplied, precondition=true and
// cancelExpected fencing applies.
// When both are omitted, precondition=false: callers must cancelCurrent against the
// live claim (or durable CancelTask with empty expected). Do NOT snapshot Repo.Get
// into expected fields here — that races with handoff and can 409 spuriously.
func readFabricCancel(w http.ResponseWriter, r *http.Request, repo *fabric.Repo, taskID string) (fabric.Identity, bool, error) {
	var body struct {
		Principal            *string `json:"principal"`
		ExpectedOwner        *string `json:"expectedOwner"`
		ExpectedFencingToken *int    `json:"expectedFencingToken"`
	}
	err := decodeExactJSON(r.Body, 1<<16, &body)
	if err != nil && !errors.Is(err, io.EOF) {
		status, code, message, _ := fabricJSONError(err)
		writeError(w, status, code, message)
		return fabric.Identity{}, false, err
	}
	id := fabric.Identity{Principal: "operator"}
	if body.Principal != nil {
		id.Principal = strings.TrimSpace(*body.Principal)
		if id.Principal == "" {
			id.Principal = "operator"
		}
	}
	ownerSet := body.ExpectedOwner != nil
	tokenSet := body.ExpectedFencingToken != nil
	if ownerSet != tokenSet {
		writeError(w, http.StatusBadRequest, "invalid_body", "expectedOwner and expectedFencingToken must both be supplied or both omitted")
		return fabric.Identity{}, false, errFabricCancelPrecondition
	}
	if ownerSet && tokenSet {
		id.ExpectedOwner = strings.TrimSpace(*body.ExpectedOwner)
		id.ExpectedFencingToken = *body.ExpectedFencingToken
		return id, true, nil
	}
	_ = repo
	_ = taskID
	return id, false, nil
}
