package integrations

import (
	"strings"

	"github.com/Wibias/Benes/internal/export"
)

type State string

const (
	StateAbsent   State = "absent"
	StateCurrent  State = "current"
	StateStale    State = "stale"
	StateConflict State = "conflict"
	StateUnsafe   State = "unsafe"
)

type Classified struct {
	State  State
	Reason string
}

func classify(fileText *string, parsed any, parseFailed bool, record *OwnershipRecord, contrib Contribution, configPath, clientID string, format export.Format) Classified {
	if parseFailed {
		return Classified{State: StateUnsafe, Reason: "unparseable"}
	}
	if blocked := blockedContainerPath(parsed, contrib.Fragments); len(blocked) > 0 {
		return Classified{State: StateUnsafe, Reason: "blocked-container"}
	}
	if record != nil && (record.ClientID != clientID || record.ConfigPath != configPath) {
		record = nil
	}
	if fileText == nil {
		if record == nil {
			return Classified{State: StateAbsent}
		}
		return Classified{State: StateStale}
	}
	hasOurs := false
	for _, fragment := range contrib.Fragments {
		if _, ok := readPath(parsed, fragment.Path); ok {
			hasOurs = true
			break
		}
	}
	if record == nil {
		if hasOurs {
			return Classified{State: StateConflict, Reason: "unowned-key"}
		}
		return Classified{State: StateAbsent}
	}
	fileHash := fingerprint(*fileText)
	blockHash := recordedFragmentFingerprint(parsed, *record)
	wantBlock := record.BlockFingerprint
	catalogHash := fingerprint(canonicalContribution(contrib))
	if sourcePreserving(clientID) {
		if blockHash == "" {
			return Classified{State: StateStale}
		}
		if blockHash != wantBlock {
			return Classified{State: StateConflict, Reason: "foreign-edit"}
		}
		if catalogHash != wantBlock && catalogHash != fingerprint(canonicalContribution(contribFromRecord(*record, parsed))) {
			if catalogHash != record.BlockFingerprint {
				return Classified{State: StateStale}
			}
		}
		if catalogHash != record.BlockFingerprint {
			return Classified{State: StateStale}
		}
		return Classified{State: StateCurrent}
	}
	if commentCapable(format) {
		if fileHash != record.FileFingerprint {
			return Classified{State: StateConflict, Reason: "foreign-edit"}
		}
		if catalogHash != record.BlockFingerprint {
			return Classified{State: StateStale}
		}
		return Classified{State: StateCurrent}
	}
	if fileHash == record.FileFingerprint && catalogHash == record.BlockFingerprint {
		return Classified{State: StateCurrent}
	}
	if blockHash != "" && blockHash == record.BlockFingerprint {
		if catalogHash != record.BlockFingerprint || fileHash != record.FileFingerprint {
			return Classified{State: StateStale}
		}
		return Classified{State: StateCurrent}
	}
	if hasOurs && blockHash != record.BlockFingerprint {
		return Classified{State: StateConflict, Reason: "foreign-edit"}
	}
	if fileHash != record.FileFingerprint && blockHash == record.BlockFingerprint {
		return Classified{State: StateStale}
	}
	if strings.TrimSpace(blockHash) == "" {
		return Classified{State: StateStale}
	}
	return Classified{State: StateConflict, Reason: "foreign-edit"}
}

func recordedFragmentFingerprint(doc any, record OwnershipRecord) string {
	if len(record.FragmentPaths) == 0 {
		return ""
	}
	contrib := Contribution{Client: record.ClientID}
	for _, path := range record.FragmentPaths {
		value, ok := readPath(doc, path)
		if !ok {
			return ""
		}
		contrib.Fragments = append(contrib.Fragments, Fragment{Path: path, Value: value})
	}
	return fingerprint(canonicalContribution(contrib))
}

func contribFromRecord(record OwnershipRecord, doc any) Contribution {
	out := Contribution{Client: record.ClientID}
	for _, path := range record.FragmentPaths {
		value, ok := readPath(doc, path)
		if !ok {
			continue
		}
		out.Fragments = append(out.Fragments, Fragment{Path: path, Value: value})
	}
	return out
}
