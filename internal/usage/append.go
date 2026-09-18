package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	HeaderSurface = "X-Benes-Surface"
	maxFieldRunes = 256
)

var appendMu sync.Mutex

func CanonicalSurface(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "codex", "claude", "claude-desktop", "grok":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func ClipField(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	runes := []rune(raw)
	if len(runes) > maxFieldRunes {
		return string(runes[:maxFieldRunes])
	}
	return raw
}

func Encode(rec Entry) ([]byte, error) {
	rec.RequestID = ClipField(rec.RequestID)
	rec.RequestedModel = ClipField(rec.RequestedModel)
	rec.Provider = ClipField(rec.Provider)
	rec.Model = ClipField(rec.Model)
	rec.Account = ClipField(rec.Account)
	rec.Surface = CanonicalSurface(rec.Surface)
	if rec.RouteDecision != nil {
		rec.RouteDecision.RouteKind = ClipField(rec.RouteDecision.RouteKind)
		if rec.RouteDecision.Profile != nil {
			rec.RouteDecision.Profile.ID = ClipField(rec.RouteDecision.Profile.ID)
			if rec.RouteDecision.Profile.ID == "" {
				rec.RouteDecision.Profile = nil
			}
		}
		if rec.RouteDecision.Selected != nil {
			rec.RouteDecision.Selected.Provider = ClipField(rec.RouteDecision.Selected.Provider)
			rec.RouteDecision.Selected.Model = ClipField(rec.RouteDecision.Selected.Model)
			if rec.RouteDecision.Selected.Provider == "" && rec.RouteDecision.Selected.Model == "" {
				rec.RouteDecision.Selected = nil
			}
		}
		if policy := rec.RouteDecision.SidecarPolicy; policy != nil {
			policy.Identity.Status = ClipField(policy.Identity.Status)
			policy.Identity.HarnessID = ClipField(policy.Identity.HarnessID)
			clipRouteSidecarOutcome(&policy.WebSearch)
			clipRouteSidecarOutcome(&policy.Vision)
		}
	}
	if rec.UsageStatus == "" {
		rec.UsageStatus = "unreported"
	}
	return json.Marshal(rec)
}

func clipRouteSidecarOutcome(outcome *RouteSidecarOutcome) {
	if outcome == nil {
		return
	}
	outcome.Configured.Source = ClipField(outcome.Configured.Source)
	outcome.Resolution = ClipField(outcome.Resolution)
	outcome.Failure = ClipField(outcome.Failure)
}

func Append(path string, rec Entry) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	raw, err := Encode(rec)
	if err != nil {
		return err
	}
	appendMu.Lock()
	defer appendMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(raw, '\n')); err != nil {
		return err
	}
	return nil
}
