package integrations

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

const snapshotRetention = 10

type Store struct {
	Root string
}

func NewStore(root string) *Store {
	return &Store{Root: root}
}

func (s *Store) recordsPath() string { return filepath.Join(s.Root, "records.json") }
func (s *Store) journalPath() string { return filepath.Join(s.Root, "journal.jsonl") }

func (s *Store) ReadRecords() map[string]OwnershipRecord {
	raw, err := os.ReadFile(s.recordsPath())
	if err != nil {
		return map[string]OwnershipRecord{}
	}
	var parsed map[string]OwnershipRecord
	if json.Unmarshal(raw, &parsed) != nil || parsed == nil {
		return map[string]OwnershipRecord{}
	}
	return parsed
}

func (s *Store) putRecord(record OwnershipRecord) error {
	all := s.ReadRecords()
	all[record.ClientID] = record
	return s.writeRecords(all)
}

func (s *Store) dropRecord(clientID string) error {
	all := s.ReadRecords()
	delete(all, clientID)
	return s.writeRecords(all)
}

func (s *Store) writeRecords(all map[string]OwnershipRecord) error {
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(s.recordsPath(), append(raw, '\n'), atomicfile.Options{Mode: 0o600})
}

func (s *Store) appendJournal(entry JournalEntry) error {
	if err := os.MkdirAll(s.Root, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.journalPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(raw, '\n'))
	return err
}

func (s *Store) ListOperations(clientID string) []JournalEntry {
	f, err := os.Open(s.journalPath())
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []JournalEntry
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}
		var entry JournalEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if clientID != "" && entry.ClientID != clientID {
			continue
		}
		out = append(out, entry)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (s *Store) FindOperation(opID string) *JournalEntry {
	for _, entry := range s.ListOperations("") {
		if entry.OpID == opID {
			copy := entry
			return &copy
		}
	}
	return nil
}

func (s *Store) captureSnapshot(clientID, opID string, text *string) SnapshotRef {
	if text == nil {
		return SnapshotRef{Kind: "none"}
	}
	rel := filepath.ToSlash(filepath.Join("snapshots", clientID, opID))
	path := filepath.Join(s.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return SnapshotRef{Kind: "none"}
	}
	if atomicfile.Write(path, []byte(*text), atomicfile.Options{Mode: 0o600}) != nil {
		return SnapshotRef{Kind: "none"}
	}
	s.pruneSnapshots(clientID)
	return SnapshotRef{Kind: "stored", RelPath: rel}
}

func (s *Store) ReadSnapshot(entry JournalEntry) (kind string, text string, path string) {
	if entry.Snapshot.Kind == "none" || entry.Snapshot.RelPath == "" {
		return "none", "", ""
	}
	path = filepath.Join(s.Root, filepath.FromSlash(entry.Snapshot.RelPath))
	raw, err := os.ReadFile(path)
	if err != nil {
		return "expired", "", path
	}
	return "stored", string(raw), path
}

func (s *Store) CountSnapshots(clientID string) int {
	dir := filepath.Join(s.Root, "snapshots", clientID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			n++
		}
	}
	return n
}

func (s *Store) pruneSnapshots(clientID string) {
	dir := filepath.Join(s.Root, "snapshots", clientID)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= snapshotRetention {
		return
	}
	type file struct {
		name string
		mod  int64
	}
	files := make([]file, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, file{name: entry.Name(), mod: info.ModTime().UnixNano()})
	}
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].mod < files[i].mod {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
	for len(files) > snapshotRetention {
		_ = os.Remove(filepath.Join(dir, files[0].name))
		files = files[1:]
	}
}
