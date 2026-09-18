package server

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/compat"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/lab"
)

func (h *handler) serveLabAPI(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/lab/") && r.URL.Path != "/api/lab" {
		return false
	}
	if !isLoopbackRequestHost(r.Host) {
		return false
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return true
	}
	projection := h.labProjection()
	switch {
	case r.URL.Path == "/api/lab/status":
		writeJSON(w, http.StatusOK, projection.status)
		return true
	case r.URL.Path == "/api/lab/verdicts":
		writeJSON(w, http.StatusOK, paginateVerdicts(projection.verdicts, r))
		return true
	case r.URL.Path == "/api/lab/subjects":
		writeJSON(w, http.StatusOK, paginateSubjects(projection.subjects, r))
		return true
	case strings.HasPrefix(r.URL.Path, "/api/lab/subjects/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/lab/subjects/")
		subject, ok := projection.subjectByID[id]
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "unknown subject")
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"subject": subject})
		return true
	case r.URL.Path == "/api/lab/observations":
		writeJSON(w, http.StatusOK, paginateMaps(projection.observations, r.URL.Query().Get("cursor"), r.URL.Query().Get("limit"), "observations"))
		return true
	case r.URL.Path == "/api/lab/catalog":
		writeJSON(w, http.StatusOK, map[string]any{"scenarios": labCatalogScenarios()})
		return true
	case r.URL.Path == "/api/lab/production-signals":
		writeJSON(w, http.StatusOK, productionSignal(r.URL.Query().Get("subjectId")))
		return true
	case r.URL.Path == "/api/lab/public/community":
		writeJSON(w, http.StatusOK, map[string]any{
			"evidence":        []any{},
			"trustClass":      "community_untrusted_v1",
			"locallyVerified": false,
		})
		return true
	case strings.HasPrefix(r.URL.Path, "/api/lab/events/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/lab/events/")
		ev, ok := projection.eventByID[id]
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return true
		}
		writeJSON(w, http.StatusOK, map[string]any{"event": labEventDTO(ev)})
		return true
	default:
		if strings.HasPrefix(r.URL.Path, "/api/lab/artifacts/") {
			writeError(w, http.StatusNotFound, "not_found", "not found")
			return true
		}
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return true
	}
}

type labProjection struct {
	status       map[string]any
	verdicts     []map[string]any
	subjects     []map[string]any
	subjectByID  map[string]map[string]any
	observations []map[string]any
	eventByID    map[string]lab.Event
}

func (h *handler) labProjection() labProjection {
	now := time.Now().UnixMilli()
	adapters := map[string]string{}
	var store lab.Projection
	if strings.TrimSpace(h.configPath) != "" {
		if disk, err := config.LoadDiskConfig(h.configPath, 0); err == nil {
			for id, raw := range disk.Providers {
				adapters[id] = compat.ProviderAdapter(raw)
			}
		}
		if loaded, err := lab.LoadProjection(filepath.Join(filepath.Dir(h.configPath), "lab")); err == nil {
			store = loaded
		}
	}
	verdicts := make([]map[string]any, 0)
	subjects := make([]map[string]any, 0)
	subjectByID := map[string]map[string]any{}
	seen := map[string]struct{}{}
	for _, model := range h.catalogModels {
		provider, modelID := compat.SplitModelID(model.ID)
		if provider == "" || modelID == "" {
			continue
		}
		if _, ok := seen[model.ID]; ok {
			continue
		}
		seen[model.ID] = struct{}{}
		subject := map[string]any{
			"subjectId":            model.ID,
			"subjectKind":          "model",
			"subjectSchemaVersion": 1,
			"provider":             provider,
			"model":                modelID,
			"displayName":          model.DisplayName,
		}
		subjects = append(subjects, map[string]any{"subjectId": model.ID, "subjectKind": "model"})
		subjectByID[model.ID] = subject
		adapter := adapters[provider]
		for _, harness := range compat.Harnesses {
			verdict := compat.Classify(harness, adapter, model.Vision)
			notes := []string{}
			if note := compat.VisionNote(model.Vision); note != "" {
				notes = append(notes, note)
			}
			verdicts = append(verdicts, labVerdict(model.ID, harness, "live_route_compatibility", verdict, now, notes))
		}
		if provider == "combo" || provider == "policy" {
			continue
		}
		for _, harness := range compat.Harnesses {
			row := labVerdict(model.ID, harness, "protocol_conformance", compat.VerdictClaimed, now, []string{})
			if store.CorruptionCount == 0 {
				if ev, ok := store.Verdicts[lab.VerdictKey(model.ID, harness)]; ok {
					row["verdict"] = ev.Verdict
					row["contributingEventIds"] = []string{ev.EventID}
					row["asOf"] = ev.RecordedAt
				}
			}
			verdicts = append(verdicts, row)
		}
	}
	sort.Slice(subjects, func(i, j int) bool {
		left, _ := subjects[i]["subjectId"].(string)
		right, _ := subjects[j]["subjectId"].(string)
		return left < right
	})
	observations := labObservationDTOs(store.Observations)
	eventByID := map[string]lab.Event{}
	for _, ev := range store.Events {
		eventByID[ev.EventID] = ev
	}
	return labProjection{
		status: map[string]any{
			"projectionAvailable":   true,
			"projectionSpecVersion": compat.SpecVersion,
			"builtAtMs":             now,
			"subjectCount":          len(subjects),
			"verdictCount":          len(verdicts),
			"observationCount":      len(store.Observations),
			"eventCount":            len(store.Events),
			"claimCount":            0,
			"artifactCount":         0,
			"corruptionCount":       store.CorruptionCount,
		},
		verdicts:     verdicts,
		subjects:     subjects,
		subjectByID:  subjectByID,
		observations: observations,
		eventByID:    eventByID,
	}
}

func labVerdict(subjectID, harness, layer, verdict string, asOf int64, notes []string) map[string]any {
	key := fmt.Sprintf("%s:%s:%s:%s", subjectID, harness, layer, compat.SpecVersion)
	sum := sha256.Sum256([]byte(key))
	digest := fmt.Sprintf("%x", sum[:8])
	if notes == nil {
		notes = []string{}
	}
	return map[string]any{
		"projectionKey":           key,
		"subjectId":               subjectID,
		"evidenceLayer":           layer,
		"suiteId":                 harness,
		"suiteVersion":            "1",
		"suiteManifestDigest":     digest,
		"projectionSpecVersion":   compat.SpecVersion,
		"verdict":                 verdict,
		"asOf":                    asOf,
		"scenarioManifestDigests": []string{},
		"claimSourceDigest":       nil,
		"contributingEventIds":    []string{},
		"contradictingEventIds":   []string{},
		"notes":                   notes,
	}
}

func labObservationDTOs(events []lab.Event) []map[string]any {
	out := make([]map[string]any, 0, len(events))
	for _, ev := range events {
		if len(out) >= 200 {
			break
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:protocol_conformance:%s", ev.SubjectID, ev.SuiteID, compat.SpecVersion)))
		digest := fmt.Sprintf("%x", sum[:8])
		out = append(out, map[string]any{
			"eventId":                ev.EventID,
			"subjectId":              ev.SubjectID,
			"evidenceLayer":          "protocol_conformance",
			"suiteId":                ev.SuiteID,
			"suiteVersion":           "1",
			"suiteManifestDigest":    digest,
			"scenarioId":             "protocol_conformance/" + ev.SuiteID,
			"scenarioVersion":        "1",
			"scenarioManifestDigest": digest,
			"outcome":                ev.Verdict,
			"completedAt":            ev.RecordedAt,
			"executionMode":          "local_classifier",
			"excluded":               false,
			"exclusionReason":        nil,
		})
	}
	return out
}

func labEventDTO(ev lab.Event) map[string]any {
	return map[string]any{
		"eventKind":       ev.EventKind,
		"eventId":         ev.EventID,
		"recordedAt":      ev.RecordedAt,
		"producer":        ev.Producer,
		"producerVersion": ev.ProducerVersion,
		"subjectId":       ev.SubjectID,
		"evidenceLayer":   ev.EvidenceLayer,
		"suiteId":         ev.SuiteID,
		"outcome":         ev.Verdict,
		"excluded":        ev.Excluded,
		"exclusionReason": ev.ExclusionReason,
	}
}

func labCatalogScenarios() []map[string]any {
	out := make([]map[string]any, 0, len(compat.Harnesses)*2)
	for _, harness := range compat.Harnesses {
		out = append(out, map[string]any{
			"suiteId":       harness,
			"evidenceLayer": "protocol_conformance",
		})
		out = append(out, map[string]any{
			"suiteId":       harness,
			"evidenceLayer": "live_route_compatibility",
		})
	}
	return out
}

func paginateVerdicts(rows []map[string]any, r *http.Request) map[string]any {
	query := r.URL.Query()
	layer := query.Get("layer")
	verdict := query.Get("verdict")
	subjectID := query.Get("subjectId")
	suiteID := query.Get("suiteId")
	filtered := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if layer != "" && fmt.Sprint(row["evidenceLayer"]) != layer {
			continue
		}
		if verdict != "" && fmt.Sprint(row["verdict"]) != verdict {
			continue
		}
		if subjectID != "" && fmt.Sprint(row["subjectId"]) != subjectID {
			continue
		}
		if suiteID != "" && fmt.Sprint(row["suiteId"]) != suiteID {
			continue
		}
		filtered = append(filtered, row)
	}
	return paginateMaps(filtered, query.Get("cursor"), query.Get("limit"), "verdicts")
}

func paginateSubjects(rows []map[string]any, r *http.Request) map[string]any {
	return paginateMaps(rows, r.URL.Query().Get("cursor"), r.URL.Query().Get("limit"), "subjects")
}

func paginateMaps(rows []map[string]any, cursor, limitRaw, key string) map[string]any {
	limit := 50
	if parsed, err := strconv.Atoi(limitRaw); err == nil && parsed > 0 && parsed <= 200 {
		limit = parsed
	}
	start := 0
	if cursor != "" {
		if parsed, err := strconv.Atoi(cursor); err == nil && parsed > 0 {
			start = parsed
		}
	}
	if start > len(rows) {
		start = len(rows)
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	page := rows[start:end]
	hasMore := end < len(rows)
	out := map[string]any{key: page, "hasMore": hasMore}
	if hasMore {
		out["nextCursor"] = strconv.Itoa(end)
	}
	return out
}

func productionSignal(subjectID string) map[string]any {
	if strings.TrimSpace(subjectID) == "" {
		subjectID = "unknown"
	}
	return map[string]any{
		"verificationStatus": "not_verification",
		"summary": map[string]any{
			"subjectId":                subjectID,
			"verificationStatus":       "not_verification",
			"recentProductionAttempts": 0,
			"recentSuccessfulAttempts": 0,
			"recentRouteErrorSignals":  0,
		},
	}
}
