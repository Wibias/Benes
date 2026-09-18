package integrations

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/export"
	"github.com/Wibias/Benes/internal/store/atomicfile"
)

type Input struct {
	ClientID     string
	Models       []export.Model
	BaseURL      string
	Hostname     string
	Home         string
	Env          map[string]string
	Store        *Store
	ConfirmDrift bool
	OpID         string
}

type Outcome struct {
	OK            bool   `json:"ok"`
	Changed       bool   `json:"changed,omitempty"`
	State         State  `json:"state"`
	ClientID      string `json:"clientId"`
	OpID          string `json:"opId,omitempty"`
	Message       string `json:"message"`
	Reason        string `json:"reason,omitempty"`
	SnapshotPath  string `json:"snapshotPath,omitempty"`
	Residual      bool   `json:"residual,omitempty"`
	Installed     bool   `json:"installed,omitempty"`
	ConfigPath    string `json:"configPath,omitempty"`
	DetectDir     string `json:"detectDir,omitempty"`
	AppliedAt     string `json:"appliedAt,omitempty"`
	LastOpID      string `json:"lastOpId,omitempty"`
	SnapshotCount int    `json:"snapshotCount,omitempty"`
}

func refuse(clientID, reason string, state State, message, snapshotPath string) Outcome {
	return Outcome{OK: false, Reason: reason, State: state, ClientID: clientID, Message: message, SnapshotPath: snapshotPath}
}

func Status(in Input) Outcome {
	paths, err := ResolvePaths(in.ClientID, in.Home, in.Env)
	if err != nil {
		return refuse(in.ClientID, "unsafe", StateUnsafe, err.Error(), "")
	}
	store := in.Store
	if store == nil {
		store = NewStore(filepath.Join(in.Home, "integrations"))
	}
	installed := dirExists(paths.DetectDir)
	text, regular, readErr := readTarget(paths.ConfigPath)
	if readErr != nil && !os.IsNotExist(readErr) && regular {
		return Outcome{OK: true, State: StateUnsafe, ClientID: in.ClientID, Installed: installed, ConfigPath: paths.ConfigPath, Message: paths.ConfigPath + " exists but could not be read", Reason: "unparseable"}
	}
	format, err := clientFormat(in.ClientID)
	if err != nil {
		return refuse(in.ClientID, "unsafe", StateUnsafe, err.Error(), "")
	}
	var parsed any = map[string]any{}
	parseFailed := false
	if text != nil {
		parsed, err = parseDocument(format, *text)
		if err != nil {
			parseFailed = true
		}
	}
	contrib, err := buildContribution(in)
	if err != nil {
		return refuse(in.ClientID, "unsafe", StateUnsafe, err.Error(), "")
	}
	stored := store.ReadRecords()[in.ClientID]
	var record *OwnershipRecord
	if stored.ClientID == in.ClientID && stored.ConfigPath == paths.ConfigPath {
		copy := stored
		record = &copy
	}
	classified := classify(text, parsed, parseFailed, record, contrib, paths.ConfigPath, in.ClientID, format)
	out := Outcome{
		OK:            true,
		State:         classified.State,
		ClientID:      in.ClientID,
		Installed:     installed,
		ConfigPath:    paths.ConfigPath,
		DetectDir:     paths.DetectDir,
		Reason:        classified.Reason,
		SnapshotCount: store.CountSnapshots(in.ClientID),
	}
	if record != nil {
		out.AppliedAt = record.AppliedAt
		out.LastOpID = record.OpID
	}
	return out
}

func Apply(in Input) Outcome {
	return mutate(in, true)
}

func Disable(in Input) Outcome {
	return mutate(in, false)
}

func mutate(in Input, enable bool) Outcome {
	paths, err := ResolvePaths(in.ClientID, in.Home, in.Env)
	if err != nil {
		return refuse(in.ClientID, "unsafe", StateUnsafe, err.Error(), "")
	}
	store := in.Store
	if store == nil {
		store = NewStore(filepath.Join(in.Home, "integrations"))
	}
	if enable && !dirExists(paths.DetectDir) {
		return refuse(in.ClientID, "not_installed", StateAbsent, in.ClientID+" is not installed", "")
	}
	if enable && loopbackOnly(in.ClientID) && !isLoopbackHost(in.Hostname) {
		return refuse(in.ClientID, "non_loopback", StateAbsent, "The generated "+in.ClientID+" integration is loopback-only and does not emit the admission header a non-loopback bind requires.", "")
	}
	format, err := clientFormat(in.ClientID)
	if err != nil {
		return refuse(in.ClientID, "unsafe", StateUnsafe, err.Error(), "")
	}
	before, regular, readErr := readTarget(paths.ConfigPath)
	if readErr != nil && regular {
		return refuse(in.ClientID, "unsafe", StateUnsafe, paths.ConfigPath+" exists but could not be read", "")
	}
	var parsed any = map[string]any{}
	parseFailed := false
	if before != nil {
		parsed, err = parseDocument(format, *before)
		if err != nil {
			parseFailed = true
			parsed = nil
		}
	}
	contrib, err := buildContribution(in)
	if err != nil {
		return refuse(in.ClientID, "unsafe", StateUnsafe, err.Error(), "")
	}
	stored := store.ReadRecords()[in.ClientID]
	var record *OwnershipRecord
	if stored.ClientID == in.ClientID && stored.ConfigPath == paths.ConfigPath {
		copy := stored
		record = &copy
	}
	classified := classify(before, parsed, parseFailed, record, contrib, paths.ConfigPath, in.ClientID, format)
	if parseFailed {
		return refuse(in.ClientID, "unsafe", StateUnsafe, paths.ConfigPath+" could not be parsed, so it was left alone", "")
	}
	if classified.State == StateUnsafe {
		msg := paths.ConfigPath + " cannot be changed safely"
		if classified.Reason == "blocked-container" {
			msg = paths.ConfigPath + " holds a value where benes would have to write a section, so applying would replace it"
		}
		return refuse(in.ClientID, "unsafe", StateUnsafe, msg, "")
	}
	if classified.State == StateConflict {
		msg := paths.ConfigPath + " already contains an benes block we did not write"
		if classified.Reason == "foreign-edit" {
			msg = paths.ConfigPath + " changed after benes wrote it"
		}
		return refuse(in.ClientID, "conflict", StateConflict, msg, "")
	}

	if enable {
		if classified.State == StateCurrent {
			return Outcome{OK: true, Changed: false, State: StateCurrent, ClientID: in.ClientID, Message: "already applied", ConfigPath: paths.ConfigPath}
		}
		base := parsed
		if classified.State == StateStale && record != nil {
			base, _ = removeFragments(parsed, record.FragmentPaths, record.CreatedContainers)
		}
		created := createdContainerPaths(base, contrib.Fragments)
		nextDoc := mergeContribution(base, contrib.Fragments)
		text, err := export.FormatDocument(format, nextDoc)
		if err != nil {
			return refuse(in.ClientID, "unsafe", StateUnsafe, paths.ConfigPath+" contains something benes cannot rewrite safely", "")
		}
		if sourcePreserving(in.ClientID) && before != nil && strings.Contains(*before, "#") {
			return refuse(in.ClientID, "unsafe", StateUnsafe, paths.ConfigPath+" uses YAML source benes cannot patch without risking unrelated comments or formatting, so it was left alone", "")
		}
		kind := "apply"
		if classified.State == StateStale {
			kind = "refresh"
		}
		return commit(store, in.ClientID, paths.ConfigPath, before, &text, record, &OwnershipRecord{
			ClientID: in.ClientID, ConfigPath: paths.ConfigPath, FileFingerprint: fingerprint(text),
			BlockFingerprint: fingerprint(canonicalContribution(contrib)), FragmentPaths: fragmentPathsOf(contrib),
			CreatedContainers: created, AppliedAt: time.Now().UTC().Format(time.RFC3339),
		}, kind, StateCurrent, "ok")
	}

	if classified.State == StateAbsent {
		return Outcome{OK: true, Changed: false, State: StateAbsent, ClientID: in.ClientID, Message: "not applied", ConfigPath: paths.ConfigPath}
	}
	if record == nil {
		return refuse(in.ClientID, "unsafe", StateUnsafe, paths.ConfigPath+" cannot be changed safely", "")
	}
	if sourcePreserving(in.ClientID) && before != nil && strings.Contains(*before, "#") {
		return refuse(in.ClientID, "unsafe", StateUnsafe, paths.ConfigPath+" uses YAML source benes cannot patch without risking unrelated comments or formatting, so nothing was removed", "")
	}
	doc, removed := removeFragments(parsed, record.FragmentPaths, record.CreatedContainers)
	if !removed {
		return Outcome{OK: true, Changed: false, State: StateAbsent, ClientID: in.ClientID, Message: "nothing to remove", ConfigPath: paths.ConfigPath}
	}
	text, err := export.FormatDocument(format, doc)
	if err != nil {
		return refuse(in.ClientID, "unsafe", StateUnsafe, paths.ConfigPath+" contains something benes cannot rewrite safely, so nothing was removed", "")
	}
	return commit(store, in.ClientID, paths.ConfigPath, before, &text, record, nil, "disable", StateAbsent, "ok")
}

func Restore(in Input) Outcome {
	store := in.Store
	if store == nil {
		store = NewStore(filepath.Join(in.Home, "integrations"))
	}
	entry := store.FindOperation(in.OpID)
	if entry == nil {
		return refuse(in.ClientID, "not_found", StateAbsent, "integration operation not found", "")
	}
	if in.ClientID != "" && entry.ClientID != in.ClientID {
		return refuse(in.ClientID, "conflict", StateConflict, "restore input names a different client than the operation", "")
	}
	paths, err := ResolvePaths(entry.ClientID, in.Home, in.Env)
	if err != nil {
		return refuse(entry.ClientID, "unsafe", StateUnsafe, err.Error(), "")
	}
	if paths.ConfigPath != entry.ConfigPath {
		return refuse(entry.ClientID, "conflict", StateConflict, "that operation was recorded for "+entry.ConfigPath+", but this client now resolves to "+paths.ConfigPath, "")
	}
	kind, snapText, snapPath := store.ReadSnapshot(*entry)
	if kind == "expired" {
		return refuse(entry.ClientID, "snapshot_expired", StateAbsent, "that backup has expired", snapPath)
	}
	current, regular, readErr := readTarget(entry.ConfigPath)
	if readErr != nil && regular {
		return refuse(entry.ClientID, "unsafe", StateUnsafe, entry.ConfigPath+" exists but could not be read", snapPath)
	}
	if !matchesOperationResult(*entry, current) && !in.ConfirmDrift {
		return refuse(entry.ClientID, "drift_requires_confirm", StateConflict, "this file changed after that operation; confirm to replace it (the current version is backed up first)", "")
	}
	var restored *string
	if kind != "none" {
		restored = &snapText
	}
	return commit(store, entry.ClientID, entry.ConfigPath, current, restored, lookupRecord(store, entry.ClientID, entry.ConfigPath), entry.PriorRecord, "restore", StateAbsent, "ok")
}

func lookupRecord(store *Store, clientID, configPath string) *OwnershipRecord {
	stored := store.ReadRecords()[clientID]
	if stored.ClientID == clientID && stored.ConfigPath == configPath {
		copy := stored
		return &copy
	}
	return nil
}

func commit(store *Store, clientID, configPath string, before, nextText *string, prior, nextRecord *OwnershipRecord, kind string, state State, message string) Outcome {
	opID := newOpID()
	snapshot := store.captureSnapshot(clientID, opID, before)
	_, _, snapPath := store.ReadSnapshot(JournalEntry{Snapshot: snapshot})
	if nextText == nil {
		if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
			return refuse(clientID, "write_failed", state, err.Error(), snapPath)
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
			return refuse(clientID, "write_failed", state, err.Error(), snapPath)
		}
		if err := atomicfile.Write(configPath, []byte(*nextText), atomicfile.Options{Mode: 0o600}); err != nil {
			return refuse(clientID, "write_failed", state, err.Error(), snapPath)
		}
	}
	fp := ""
	absent := nextText == nil
	if nextText != nil {
		fp = fingerprint(*nextText)
	}
	if nextRecord != nil {
		nextRecord.OpID = opID
		if nextRecord.AppliedAt == "" {
			nextRecord.AppliedAt = time.Now().UTC().Format(time.RFC3339)
		}
		if err := store.putRecord(*nextRecord); err != nil {
			_ = restoreBytes(configPath, before)
			return refuse(clientID, "write_failed", state, "could not record ownership", snapPath)
		}
	} else if err := store.dropRecord(clientID); err != nil {
		_ = restoreBytes(configPath, before)
		return refuse(clientID, "write_failed", state, "could not record ownership", snapPath)
	}
	entry := JournalEntry{
		OpID: opID, ClientID: clientID, Kind: kind, At: time.Now().UTC().Format(time.RFC3339),
		ConfigPath: configPath, Snapshot: snapshot, ResultFingerprint: fp, ResultAbsent: absent, PriorRecord: prior,
	}
	if err := store.appendJournal(entry); err != nil {
		_ = restoreBytes(configPath, before)
		if prior != nil {
			_ = store.putRecord(*prior)
		} else {
			_ = store.dropRecord(clientID)
		}
		return refuse(clientID, "write_failed", state, "could not append the journal row", snapPath)
	}
	return Outcome{OK: true, Changed: true, State: state, ClientID: clientID, OpID: opID, Message: message, ConfigPath: configPath, SnapshotPath: snapPath}
}

func restoreBytes(path string, before *string) error {
	if before == nil {
		return os.Remove(path)
	}
	return atomicfile.Write(path, []byte(*before), atomicfile.Options{Mode: 0o600})
}

func buildContribution(in Input) (Contribution, error) {
	raw, err := export.Contribute(in.ClientID, export.Context{BaseURL: in.BaseURL, Models: in.Models, Hostname: in.Hostname})
	if err != nil {
		return Contribution{}, err
	}
	return fromExport(raw), nil
}

func readTarget(path string) (*string, bool, error) {
	st, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, err
		}
		return nil, false, err
	}
	if !st.Mode().IsRegular() {
		return nil, false, fmt.Errorf("not a regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, true, err
	}
	text := string(raw)
	return &text, true, nil
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func isLoopbackHost(hostname string) bool {
	switch strings.ToLower(strings.TrimSpace(hostname)) {
	case "", "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	default:
		return false
	}
}

func newOpID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:])
}
