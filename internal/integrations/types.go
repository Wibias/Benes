package integrations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/Wibias/Benes/internal/export"
)

type Fragment struct {
	Path  []string
	Value any
}

type Contribution struct {
	Client    string
	Fragments []Fragment
}

type OwnershipRecord struct {
	ClientID          string     `json:"clientId"`
	ConfigPath        string     `json:"configPath"`
	FileFingerprint   string     `json:"fileFingerprint"`
	BlockFingerprint  string     `json:"blockFingerprint"`
	FragmentPaths     [][]string `json:"fragmentPaths"`
	CreatedContainers []string   `json:"createdContainers,omitempty"`
	AppliedAt         string     `json:"appliedAt"`
	OpID              string     `json:"opId"`
}

type JournalEntry struct {
	OpID              string           `json:"opId"`
	ClientID          string           `json:"clientId"`
	Kind              string           `json:"kind"`
	At                string           `json:"at"`
	ConfigPath        string           `json:"configPath"`
	Snapshot          SnapshotRef      `json:"snapshot"`
	ResultFingerprint string           `json:"resultFingerprint"`
	ResultAbsent      bool             `json:"resultAbsent"`
	PriorRecord       *OwnershipRecord `json:"priorRecord"`
}

type SnapshotRef struct {
	Kind    string `json:"kind"`
	RelPath string `json:"relPath,omitempty"`
}

func fingerprint(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}

func canonicalContribution(contrib Contribution) string {
	frags := append([]Fragment{}, contrib.Fragments...)
	sort.SliceStable(frags, func(i, j int) bool {
		return joinPath(frags[i].Path) < joinPath(frags[j].Path)
	})
	rows := make([]any, 0, len(frags))
	for _, fragment := range frags {
		rows = append(rows, []any{fragment.Path, fragment.Value})
	}
	raw, _ := json.Marshal(rows)
	return string(raw)
}

func fragmentPathsOf(contrib Contribution) [][]string {
	out := make([][]string, 0, len(contrib.Fragments))
	for _, fragment := range contrib.Fragments {
		out = append(out, append([]string{}, fragment.Path...))
	}
	return out
}

func fromExport(contrib export.Contribution) Contribution {
	out := Contribution{Client: contrib.Client}
	for _, fragment := range contrib.Fragments {
		out.Fragments = append(out.Fragments, Fragment{Path: append([]string{}, fragment.Path...), Value: fragment.Value})
	}
	return out
}

func MatchesResult(entry JournalEntry, current *string) bool {
	return matchesOperationResult(entry, current)
}

func matchesOperationResult(entry JournalEntry, current *string) bool {
	if entry.ResultAbsent {
		return current == nil
	}
	if current == nil {
		return false
	}
	return fingerprint(*current) == entry.ResultFingerprint
}

func commentCapable(format export.Format) bool {
	switch format {
	case export.FormatYAML, export.FormatTOML, export.FormatJSON5:
		return true
	default:
		return false
	}
}

func sourcePreserving(client string) bool {
	return client == "omp" || client == "dsh"
}

func loopbackOnly(client string) bool {
	info := export.Clients()
	for _, item := range info {
		if item.ID == client {
			return item.LoopbackOnly
		}
	}
	return false
}

func knownClient(id string) bool {
	id = strings.TrimSpace(id)
	for _, item := range export.ClientIDs {
		if item == id {
			return true
		}
	}
	return false
}
