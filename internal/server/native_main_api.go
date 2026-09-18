package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/nativemain"
)

func (h *handler) nativeMainManager() (*nativemain.Manager, error) {
	codexHome := strings.TrimSpace(h.codexHome)
	configDir := h.benesHome()
	keys := h.nativeMainKeys
	return nativemain.NewManager(codexHome, configDir, keys)
}

func (h *handler) serveNativeMainProfilesAPI(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/native-main-profiles") {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	mgr, err := h.nativeMainManager()
	if err != nil {
		writeNativeMainError(w, err)
		return true
	}
	switch {
	case r.URL.Path == "/api/native-main-profiles" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		result, err := mgr.List()
		if err != nil {
			writeNativeMainError(w, err)
			return true
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return true
		}
		writeJSON(w, http.StatusOK, result)
		return true
	case r.URL.Path == "/api/native-main-profiles/doctor" && r.Method == http.MethodGet:
		result, err := mgr.Doctor()
		if err != nil {
			writeNativeMainError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, result)
		return true
	case r.URL.Path == "/api/native-main-profiles/register" && r.Method == http.MethodPost:
		body, ok := readNativeMainBody(w, r)
		if !ok {
			return true
		}
		label, _ := body["label"].(string)
		result, err := mgr.Register(label)
		if err != nil {
			writeNativeMainError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, result)
		return true
	case r.URL.Path == "/api/native-main-profiles/stage" && r.Method == http.MethodPost:
		result, err := mgr.PrepareStage()
		if err != nil {
			writeNativeMainError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, result)
		return true
	case r.URL.Path == "/api/native-main-profiles/stage/heartbeat" && r.Method == http.MethodPost:
		body, ok := readNativeMainBody(w, r)
		if !ok {
			return true
		}
		stageID, _ := body["stageId"].(string)
		token, _ := body["writerToken"].(string)
		if stageID == "" || token == "" {
			writeJSON(w, 400, map[string]any{"error": "A staging identifier and writer token are required.", "code": "INVALID_REQUEST"})
			return true
		}
		result, err := mgr.Heartbeat(stageID, token)
		if err != nil {
			writeNativeMainError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, result)
		return true
	case r.URL.Path == "/api/native-main-profiles/stage/finish" && r.Method == http.MethodPost:
		body, ok := readNativeMainBody(w, r)
		if !ok {
			return true
		}
		stageID, _ := body["stageId"].(string)
		token, _ := body["writerToken"].(string)
		label, _ := body["label"].(string)
		if stageID == "" || token == "" || label == "" {
			writeJSON(w, 400, map[string]any{"error": "A staging identifier, writer token, and profile label are required.", "code": "INVALID_REQUEST"})
			return true
		}
		result, err := mgr.Finish(stageID, token, label)
		if err != nil {
			writeNativeMainError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, result)
		return true
	case r.URL.Path == "/api/native-main-profiles/stage/cancel" && r.Method == http.MethodPost:
		body, ok := readNativeMainBody(w, r)
		if !ok {
			return true
		}
		stageID, _ := body["stageId"].(string)
		token, _ := body["writerToken"].(string)
		result, err := mgr.Cancel(stageID, token)
		if err != nil {
			writeNativeMainError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, result)
		return true
	case r.URL.Path == "/api/native-main-profiles/switch" && r.Method == http.MethodPost:
		body, ok := readNativeMainBody(w, r)
		if !ok {
			return true
		}
		target, _ := body["target"].(string)
		confirmed, _ := body["confirmedStopped"].(bool)
		if target == "" {
			writeJSON(w, 400, map[string]any{"error": "A target profile is required.", "code": "INVALID_REQUEST"})
			return true
		}
		result, err := mgr.Switch(target, confirmed)
		if err != nil {
			writeNativeMainError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, result)
		return true
	case r.URL.Path == "/api/native-main-profiles/recover" && r.Method == http.MethodPost:
		body, ok := readNativeMainBody(w, r)
		if !ok {
			return true
		}
		rollback, _ := body["rollback"].(bool)
		confirmed, _ := body["confirmedStopped"].(bool)
		result, err := mgr.Recover(rollback, confirmed)
		if err != nil {
			writeNativeMainError(w, err)
			return true
		}
		writeJSON(w, http.StatusOK, result)
		return true
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Unknown native-profile operation", "code": "INVALID_REQUEST"})
		return true
	}
}

func readNativeMainBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]any{"error": "A JSON object body is required.", "code": "INVALID_REQUEST"})
		return nil, false
	}
	return body, true
}

func writeNativeMainError(w http.ResponseWriter, err error) {
	te := nativemain.AsError(err)
	payload := map[string]any{"error": te.Message, "code": te.Code, "retryable": te.Retryable}
	if te.CleanupRequired {
		payload["cleanupRequired"] = true
	}
	if te.PlaintextMayRemain {
		payload["plaintextMayRemain"] = true
	}
	writeJSON(w, te.Status, payload)
}
