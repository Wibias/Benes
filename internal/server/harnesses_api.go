package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/harnessboard"
	"github.com/Wibias/Benes/internal/harnesspolicy"
)

type harnessSidecarPatch struct {
	webSearchPresent bool
	webSearch        *harnesspolicy.Activation
	visionPresent    bool
	vision           *harnesspolicy.Activation
}

func decodeHarnessSidecarPatch(raw json.RawMessage) (harnessSidecarPatch, error) {
	if len(raw) == 0 {
		return harnessSidecarPatch{}, nil
	}
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return harnessSidecarPatch{}, errors.New("sidecars must be an object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return harnessSidecarPatch{}, err
	}
	patch := harnessSidecarPatch{}
	for key, value := range fields {
		switch key {
		case "webSearch":
			activation, err := decodeHarnessSidecarActivation(value)
			if err != nil {
				return harnessSidecarPatch{}, err
			}
			patch.webSearchPresent = true
			patch.webSearch = activation
		case "vision":
			activation, err := decodeHarnessSidecarActivation(value)
			if err != nil {
				return harnessSidecarPatch{}, err
			}
			patch.visionPresent = true
			patch.vision = activation
		default:
			return harnessSidecarPatch{}, errors.New("unknown sidecar setting")
		}
	}
	return patch, nil
}

func decodeHarnessSidecarActivation(raw json.RawMessage) (*harnesspolicy.Activation, error) {
	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var activation harnesspolicy.Activation
	if err := json.Unmarshal(trimmed, &activation); err != nil {
		return nil, err
	}
	if err := activation.Validate(); err != nil {
		return nil, err
	}
	return &activation, nil
}

func applyHarnessSidecarPatch(row *harnessboard.Settings, patch harnessSidecarPatch) {
	if !patch.webSearchPresent && !patch.visionPresent {
		return
	}
	overrides := harnesspolicy.Overrides{}
	if row.Sidecars != nil {
		overrides = *row.Sidecars
	}
	if patch.webSearchPresent {
		overrides.WebSearch = patch.webSearch
	}
	if patch.visionPresent {
		overrides.Vision = patch.vision
	}
	if overrides.WebSearch == nil && overrides.Vision == nil {
		row.Sidecars = nil
		return
	}
	row.Sidecars = &overrides
}

func (h *handler) serveHarnessesAPI(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/harnesses", "/api/harnesses/settings", "/api/harnesses/reveal":
	default:
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	switch r.URL.Path {
	case "/api/harnesses":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			return true
		}
		h.serveHarnessesGET(w)
		return true
	case "/api/harnesses/reveal":
		if r.Method != http.MethodPost {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		h.serveHarnessesRevealPOST(w, r)
		return true
	default:
		if r.Method != http.MethodPut {
			writeError(w, http.StatusNotFound, "not_found", "route not found")
			return true
		}
		h.serveHarnessesSettingsPUT(w, r)
		return true
	}
}

func (h *handler) serveHarnessesGET(w http.ResponseWriter) {
	home := strings.TrimSpace(h.benesHome())
	if home == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	clients, err := harnessboard.Probe(harnessboard.ProbeInput{
		BenesHome: home,
		CodexHome: strings.TrimSpace(h.codexHome),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "harness_probe_failed", "harness status could not be read")
		return
	}
	views := make([]harnessClientView, 0, len(clients))
	for _, client := range clients {
		views = append(views, harnessClientView{
			Client:        client,
			SidecarPolicy: h.harnessSidecarPolicyView(client.ID),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"clients": views})
}

func (h *handler) serveHarnessesSettingsPUT(w http.ResponseWriter, r *http.Request) {
	home := strings.TrimSpace(h.benesHome())
	if home == "" {
		writeError(w, http.StatusServiceUnavailable, "config_unreadable", "config path is required")
		return
	}
	var body struct {
		ClientID       string          `json:"clientId"`
		AutoDetect     *bool           `json:"autoDetect"`
		AutoApply      *bool           `json:"autoApply"`
		RetainSnapshot *bool           `json:"retainSnapshot"`
		AllowRestart   *bool           `json:"allowRestart"`
		Sidecars       json.RawMessage `json:"sidecars"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || strings.TrimSpace(body.ClientID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "clientId is required")
		return
	}
	if !harnessboard.Known(body.ClientID) {
		writeError(w, http.StatusBadRequest, "unknown_client", "unknown harness")
		return
	}
	sidecarPatch, err := decodeHarnessSidecarPatch(body.Sidecars)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "sidecars must contain only enabled, disabled, or null overrides")
		return
	}
	all, err := harnessboard.LoadSettings(home)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "harness_settings_unreadable", "harness settings could not be read")
		return
	}
	row := harnessboard.DefaultSettings()
	if existing, ok := all[body.ClientID]; ok {
		row = existing
	}
	if body.AutoDetect != nil {
		row.AutoDetect = *body.AutoDetect
	}
	if body.AutoApply != nil {
		row.AutoApply = *body.AutoApply
	}
	if body.RetainSnapshot != nil {
		row.RetainSnapshot = *body.RetainSnapshot
	}
	if body.AllowRestart != nil {
		row.AllowRestart = *body.AllowRestart
	}
	applyHarnessSidecarPatch(&row, sidecarPatch)
	saved, err := harnessboard.PutSettings(home, body.ClientID, row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "harness_settings_unwritable", "harness settings could not be stored")
		return
	}
	if all == nil {
		all = map[string]harnessboard.Settings{}
	}
	all[body.ClientID] = saved
	harnessSidecarRuntimeFor(h).Replace(all)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"clientId": body.ClientID,
		"settings": saved,
	})
}

func (h *handler) serveHarnessesRevealPOST(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ClientID string `json:"clientId"`
		Target   string `json:"target"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "clientId and target are required")
		return
	}
	target := harnessboard.RevealTarget(strings.TrimSpace(body.Target))
	path, err := harnessboard.Reveal(harnessboard.RevealInput{
		ClientID:  body.ClientID,
		Target:    target,
		BenesHome: strings.TrimSpace(h.benesHome()),
		CodexHome: strings.TrimSpace(h.codexHome),
	})
	if err != nil {
		switch {
		case errors.Is(err, harnessboard.ErrUnknownClient):
			writeError(w, http.StatusBadRequest, "unknown_client", "unknown harness")
		case errors.Is(err, harnessboard.ErrUnknownTarget):
			writeError(w, http.StatusBadRequest, "invalid_target", "target must be log or config")
		case errors.Is(err, harnessboard.ErrPathUnavailable):
			writeError(w, http.StatusNotFound, "path_unavailable", "path is not available on this machine")
		default:
			writeError(w, http.StatusInternalServerError, "reveal_failed", "could not open path")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"clientId": strings.TrimSpace(body.ClientID),
		"target":   string(target),
		"path":     path,
	})
}
