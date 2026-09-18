package lab

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var ErrCorrupt = errors.New("lab ledger corrupt")

type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) path() string { return filepath.Join(s.dir, "events.jsonl") }

func (s *Store) Append(ev Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := s.readAllLocked()
	if err != nil {
		return err
	}
	ev.Sequence = uint64(len(existing) + 1)
	if len(existing) > 0 {
		ev.PrevHash = existing[len(existing)-1].EventHash
	}
	if ev.EventID == "" {
		ev.EventID = newEventID(ev.Sequence, ev.RecordedAt)
	}
	if ev.Producer == "" {
		ev.Producer = ProducerName
	}
	h, err := HashEvent(ev)
	if err != nil {
		return err
	}
	ev.EventHash = h
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

func (s *Store) All() ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readAllLocked()
}

func (s *Store) readAllLocked() ([]Event, error) {
	data, err := os.ReadFile(s.path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Event
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var ev Event
		if json.Unmarshal([]byte(line), &ev) != nil || ev.EventID == "" {
			return nil, ErrCorrupt
		}
		out = append(out, ev)
	}
	return out, nil
}
