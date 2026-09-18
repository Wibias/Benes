package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/sessions"
)

var (
	errInvalidDiagnosticsLimit     = errors.New("invalid_limit")
	errInvalidDiagnosticsStatus    = errors.New("invalid_status")
	errInvalidDiagnosticsProtocol  = errors.New("invalid_protocol")
	errInvalidDiagnosticsRouteKind = errors.New("invalid_route_kind")
	errInvalidDiagnosticsSessionID = errors.New("invalid_session_id")
)

func (h *handler) serveDiagnosticsAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/diagnostics/requests" && !strings.HasPrefix(r.URL.Path, "/api/diagnostics/requests/") {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if h.diagnostics == nil {
		h.diagnostics = newRequestTelemetryState()
	}
	if r.URL.Path == "/api/diagnostics/requests" {
		return h.serveDiagnosticsRequestList(w, r)
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/diagnostics/requests/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		if _, ok := h.diagnostics.lookup(id); !ok {
			w.WriteHeader(http.StatusNotFound)
			return true
		}
		w.WriteHeader(http.StatusOK)
		return true
	}
	record, ok := h.diagnostics.lookup(id)
	if !ok {
		writeError(w, http.StatusNotFound, "request_not_retained", "request is no longer retained")
		return true
	}
	writeJSON(w, http.StatusOK, record.detail())
	return true
}

func (h *handler) serveDiagnosticsRequestList(w http.ResponseWriter, r *http.Request) bool {
	query, err := parseDiagnosticsQuery(r)
	if err != nil {
		switch err {
		case errInvalidDiagnosticsCursor:
			writeError(w, http.StatusBadRequest, "invalid_cursor", "cursor is malformed")
		case errInvalidDiagnosticsLimit:
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
		case errInvalidDiagnosticsStatus:
			writeError(w, http.StatusBadRequest, "invalid_status", "status must be an HTTP status code")
		case errInvalidDiagnosticsProtocol:
			writeError(w, http.StatusBadRequest, "invalid_protocol", "protocol is not a known request protocol")
		case errInvalidDiagnosticsRouteKind:
			writeError(w, http.StatusBadRequest, "invalid_route_kind", "routeKind must be direct, combo, or policy")
		case errInvalidDiagnosticsSessionID:
			writeError(w, http.StatusBadRequest, "invalid_session_id", "session id is malformed")
		default:
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		}
		return true
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	listed, err := h.diagnostics.query(query)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_cursor", "cursor is malformed")
		return true
	}
	if listed.Requests == nil {
		listed.Requests = []diagnosticsRequestSummary{}
	}
	writeJSON(w, http.StatusOK, listed)
	return true
}

func parseDiagnosticsQuery(r *http.Request) (diagnosticsQuery, error) {
	values := r.URL.Query()
	out := diagnosticsQuery{
		Cursor:        strings.TrimSpace(values.Get("cursor")),
		SessionID:     strings.TrimSpace(values.Get("sessionId")),
		Protocol:      strings.TrimSpace(values.Get("protocol")),
		Provider:      strings.TrimSpace(values.Get("provider")),
		Model:         strings.TrimSpace(values.Get("model")),
		RouteKind:     strings.TrimSpace(values.Get("routeKind")),
		RequestID:     strings.TrimSpace(values.Get("requestId")),
		CorrelationID: strings.TrimSpace(values.Get("correlationId")),
	}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return diagnosticsQuery{}, errInvalidDiagnosticsLimit
		}
		out.Limit = limit
	}
	if raw := strings.TrimSpace(values.Get("status")); raw != "" {
		status, err := strconv.Atoi(raw)
		if err != nil || status < 100 || status > 599 {
			return diagnosticsQuery{}, errInvalidDiagnosticsStatus
		}
		out.Status = status
		out.HasStatus = true
	}
	if out.SessionID != "" && !sessions.ValidSessionID(out.SessionID) {
		return diagnosticsQuery{}, errInvalidDiagnosticsSessionID
	}
	if out.Protocol != "" {
		switch out.Protocol {
		case sessions.ProtocolResponses, sessions.ProtocolChat, sessions.ProtocolAnthropic:
		default:
			return diagnosticsQuery{}, errInvalidDiagnosticsProtocol
		}
	}
	if out.RouteKind != "" {
		switch out.RouteKind {
		case "direct", "combo", "policy":
		default:
			return diagnosticsQuery{}, errInvalidDiagnosticsRouteKind
		}
	}
	if out.Cursor != "" {
		if _, _, err := decodeDiagnosticsCursor(out.Cursor); err != nil {
			return diagnosticsQuery{}, errInvalidDiagnosticsCursor
		}
	}
	return out, nil
}
