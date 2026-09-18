package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/sessions"
)

func (h *handler) serveSessionsAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/sessions" && !strings.HasPrefix(r.URL.Path, "/api/sessions/") {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if h.sessionsUnavailable {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			return true
		}
		writeSessionStoreUnavailable(w)
		return true
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	if h.sessions == nil {
		if r.URL.Path == "/api/sessions" {
			writeJSON(w, http.StatusOK, map[string]any{"sessions": []any{}, "hasMore": false})
			return true
		}
		if r.URL.Path == "/api/sessions/filters" {
			writeJSON(w, http.StatusOK, sessionFilterValuesJSON(sessions.FilterValues{}))
			return true
		}
		writeError(w, http.StatusNotFound, "not_found", "unknown session")
		return true
	}
	if r.URL.Path == "/api/sessions/filters" {
		values, err := h.sessions.FilterValues()
		if errors.Is(err, sessions.ErrUnavailable) || errors.Is(err, sessions.ErrUnsupportedSchema) {
			writeSessionStoreUnavailable(w)
			return true
		}
		if err != nil {
			writeSessionStoreUnavailable(w)
			return true
		}
		writeJSON(w, http.StatusOK, sessionFilterValuesJSON(values))
		return true
	}
	if r.URL.Path == "/api/sessions" {
		limit, err := parseSessionLimit(r.URL.Query().Get("limit"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
			return true
		}
		listed, err := h.sessions.List(sessionListOptions(r.URL.Query(), limit))
		if errors.Is(err, sessions.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_cursor", "cursor is malformed")
			return true
		}
		if errors.Is(err, sessions.ErrInvalidQuery) {
			writeError(w, http.StatusBadRequest, "invalid_q", "search query is invalid")
			return true
		}
		if errors.Is(err, sessions.ErrInvalidFilter) {
			writeSessionFilterError(w, err)
			return true
		}
		if errors.Is(err, sessions.ErrUnavailable) || errors.Is(err, sessions.ErrUnsupportedSchema) {
			writeSessionStoreUnavailable(w)
			return true
		}
		if err != nil {
			writeSessionStoreUnavailable(w)
			return true
		}
		out := map[string]any{"sessions": sessionSummariesJSON(listed.Sessions), "hasMore": listed.HasMore}
		if listed.HasMore {
			out["nextCursor"] = listed.NextCursor
		}
		writeJSON(w, http.StatusOK, out)
		return true
	}
	rawID := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	if strings.Contains(rawID, "/") {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	id, err := url.PathUnescape(rawID)
	if err != nil || strings.TrimSpace(id) == "" {
		writeError(w, http.StatusBadRequest, "invalid_id", "session id is malformed")
		return true
	}
	limit, err := parseSessionLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be a positive integer")
		return true
	}
	detail, err := h.sessions.Get(id, sessions.DetailOptions{Limit: limit, Cursor: strings.TrimSpace(r.URL.Query().Get("cursor"))})
	if errors.Is(err, sessions.ErrInvalidCursor) {
		writeError(w, http.StatusBadRequest, "invalid_cursor", "cursor is malformed")
		return true
	}
	if errors.Is(err, sessions.ErrInvalidID) {
		writeError(w, http.StatusBadRequest, "invalid_id", "session id is malformed")
		return true
	}
	if errors.Is(err, sessions.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "unknown session")
		return true
	}
	if errors.Is(err, sessions.ErrUnavailable) || errors.Is(err, sessions.ErrUnsupportedSchema) {
		writeSessionStoreUnavailable(w)
		return true
	}
	if err != nil {
		writeSessionStoreUnavailable(w)
		return true
	}
	out := map[string]any{
		"session":    sessionSummaryJSON(detail.Session),
		"aggregates": sessionAggregatesJSON(detail.Aggregates),
		"requests":   sessionRequestsJSON(detail.Requests),
		"hasMore":    detail.HasMore,
	}
	if detail.HasMore {
		out["nextCursor"] = detail.NextCursor
	}
	writeJSON(w, http.StatusOK, out)
	return true
}

func writeSessionStoreUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "store_unavailable", "session store is unavailable")
}

func writeSessionFilterError(w http.ResponseWriter, err error) {
	message := err.Error()
	code := "invalid_filter"
	if strings.Contains(message, "protocol") {
		code = "invalid_protocol"
		writeError(w, http.StatusBadRequest, code, "protocol is not a known session protocol")
		return
	}
	writeError(w, http.StatusBadRequest, code, "session filter is invalid")
}

func sessionListOptions(query url.Values, limit int) sessions.ListOptions {
	return sessions.ListOptions{
		Limit:     limit,
		Cursor:    strings.TrimSpace(query.Get("cursor")),
		Q:         query.Get("q"),
		Namespace: strings.TrimSpace(query.Get("namespace")),
		Protocol:  strings.TrimSpace(query.Get("protocol")),
		Provider:  strings.TrimSpace(query.Get("provider")),
		Model:     strings.TrimSpace(query.Get("model")),
		Policy:    strings.TrimSpace(query.Get("policy")),
		Combo:     strings.TrimSpace(query.Get("combo")),
	}
}

func parseSessionLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, fmt.Errorf("invalid limit")
	}
	return limit, nil
}

func sessionSummariesJSON(rows []sessions.Summary) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, sessionSummaryJSON(row))
	}
	return out
}

func sessionSummaryJSON(row sessions.Summary) map[string]any {
	out := map[string]any{
		"id":             row.ID,
		"namespace":      row.Namespace,
		"startedAt":      formatSessionTime(row.StartedAt),
		"lastActivityAt": formatSessionTime(row.LastActivityAt),
		"requestCount":   row.RequestCount,
		"protocols":      stringListJSON(row.Protocols),
	}
	if row.ExternalID != "" {
		out["externalId"] = row.ExternalID
	}
	return out
}

func sessionRequestsJSON(rows []sessions.Request) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := map[string]any{
			"id":         row.ID,
			"sessionId":  row.SessionID,
			"startedAt":  formatSessionTime(row.StartedAt),
			"protocol":   row.Protocol,
			"method":     row.Method,
			"path":       row.Path,
			"status":     row.Status,
			"durationMs": row.DurationMs,
		}
		if row.CorrelationID != "" {
			item["correlationId"] = row.CorrelationID
		}
		if row.RequestBytes != nil {
			item["requestBytes"] = *row.RequestBytes
		}
		if row.ResponseBytes != nil {
			item["responseBytes"] = *row.ResponseBytes
		}
		if routing := sessionRoutingJSON(row.Routing, row.Attempts); len(routing) > 0 {
			item["routing"] = routing
		}
		if usage := sessionUsageJSON(row.Usage); usage != nil {
			item["usage"] = usage
		}
		out = append(out, item)
	}
	return out
}

func sessionRoutingJSON(row sessions.Routing, attempts []sessions.Attempt) map[string]any {
	out := map[string]any{}
	if row.Kind != "" {
		out["kind"] = row.Kind
	}
	if row.RequestedModel != "" {
		out["requestedModel"] = row.RequestedModel
	}
	if row.RequestedProvider != "" {
		out["requestedProvider"] = row.RequestedProvider
	}
	if row.ResolvedModel != "" {
		out["resolvedModel"] = row.ResolvedModel
	}
	if row.Provider != "" {
		out["provider"] = row.Provider
	}
	if row.ComboID != "" {
		out["comboId"] = row.ComboID
	}
	if row.PolicyID != "" {
		out["policyId"] = row.PolicyID
	}
	if row.CommittedMember != "" {
		out["committedMember"] = row.CommittedMember
	}
	if len(attempts) > 0 {
		out["attempts"] = attempts
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sessionUsageJSON(row *sessions.Usage) map[string]any {
	if row == nil {
		return nil
	}
	out := map[string]any{}
	if row.InputTokens != nil {
		out["inputTokens"] = *row.InputTokens
	}
	if row.CachedInputTokens != nil {
		out["cachedInputTokens"] = *row.CachedInputTokens
	}
	if row.OutputTokens != nil {
		out["outputTokens"] = *row.OutputTokens
	}
	if row.TotalTokens != nil {
		out["totalTokens"] = *row.TotalTokens
	}
	if row.Cost != nil {
		out["cost"] = *row.Cost
		if row.Currency != "" {
			out["currency"] = row.Currency
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func formatSessionTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func sessionFilterValuesJSON(values sessions.FilterValues) map[string]any {
	return map[string]any{
		"protocols":  stringListJSON(values.Protocols),
		"namespaces": stringListJSON(values.Namespaces),
		"providers":  stringListJSON(values.Providers),
		"models":     stringListJSON(values.Models),
		"policyIds":  stringListJSON(values.PolicyIDs),
		"comboIds":   stringListJSON(values.ComboIDs),
	}
}

func sessionAggregatesJSON(row sessions.Aggregates) map[string]any {
	return map[string]any{
		"usage":                sessionUsageAggregateJSON(row.Usage),
		"protocols":            stringListJSON(row.Protocols),
		"models":               stringListJSON(row.Models),
		"providers":            stringListJSON(row.Providers),
		"policyIds":            stringListJSON(row.PolicyIDs),
		"comboIds":             stringListJSON(row.ComboIDs),
		"failoverRequestCount": row.FailoverRequestCount,
		"hadFailover":          row.HadFailover,
	}
}

func sessionUsageAggregateJSON(row sessions.UsageAggregate) map[string]any {
	return map[string]any{
		"inputTokens":       sessionFieldCoverageJSON(row.InputTokens),
		"cachedInputTokens": sessionFieldCoverageJSON(row.CachedInputTokens),
		"outputTokens":      sessionFieldCoverageJSON(row.OutputTokens),
		"totalTokens":       sessionFieldCoverageJSON(row.TotalTokens),
		"cost":              sessionCostCoverageJSON(row.Cost),
	}
}

func sessionFieldCoverageJSON(row sessions.FieldCoverage) map[string]any {
	out := map[string]any{
		"attributedRequests": row.AttributedRequests,
		"totalRequests":      row.TotalRequests,
		"complete":           row.Complete,
	}
	if row.Value != nil {
		out["value"] = *row.Value
	}
	return out
}

func sessionCostCoverageJSON(row sessions.CostCoverage) map[string]any {
	out := map[string]any{
		"attributedRequests": row.AttributedRequests,
		"totalRequests":      row.TotalRequests,
		"complete":           row.Complete,
		"currencies":         stringListJSON(row.Currencies),
	}
	if row.Value != nil {
		out["value"] = *row.Value
	}
	if row.Currency != "" {
		out["currency"] = row.Currency
	}
	return out
}

func stringListJSON(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
