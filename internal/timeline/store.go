package timeline

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

var ErrNotFound = errors.New("timeline not found")

type Store struct {
	dir   string
	limit int
}

func NewStore(dir string, limit int) *Store {
	if limit <= 0 {
		limit = 32
	}
	return &Store{dir: dir, limit: limit}
}

type persistedTrace struct {
	ID     string          `json:"id"`
	Route  *persistedRoute `json:"route,omitempty"`
	Events []persistedEvent `json:"events"`
}

type persistedRoute struct {
	RequestedProvider  string `json:"requested_provider,omitempty"`
	ProviderConnection string `json:"provider_connection,omitempty"`
	Model              string `json:"model,omitempty"`
}

type persistedEvent struct {
	Stage     string `json:"stage"`
	Side      string `json:"side,omitempty"`
	Milestone string `json:"milestone,omitempty"`
	OK        bool   `json:"ok"`
	Cause     string `json:"cause,omitempty"`
	ElapsedMS int64  `json:"elapsed_ms,omitempty"`
	Attempt   int    `json:"attempt,omitempty"`
}

func (s *Store) Save(tr *Trace) error {
	if s == nil || tr == nil || strings.TrimSpace(tr.ID()) == "" {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil
	}
	events := tr.Events()
	if len(events) > s.limit {
		events = events[:s.limit]
	}
	out := persistedTrace{ID: tr.ID(), Events: make([]persistedEvent, 0, len(events))}
	route := tr.Route()
	if route != (Route{}) {
		out.Route = &persistedRoute{
			RequestedProvider:  route.RequestedProvider,
			ProviderConnection: route.ProviderConnection,
			Model:              route.Model,
		}
	}
	for _, ev := range events {
		out.Events = append(out.Events, persistedEvent{
			Stage:     string(ev.Stage),
			Side:      string(ev.Side),
			Milestone: string(ev.Milestone),
			OK:        ev.OK,
			Cause:     ev.Cause,
			ElapsedMS: ev.Elapsed.Milliseconds(),
			Attempt:   ev.Attempt,
		})
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	_ = atomicfile.Write(s.path(tr.ID()), raw, atomicfile.Options{Mode: 0o600})
	return nil
}

func (s *Store) Lookup(id string) (*Trace, error) {
	id = strings.TrimSpace(id)
	if s == nil || id == "" || strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return nil, ErrNotFound
	}
	if _, err := os.Stat(s.path(id)); err != nil {
		return nil, ErrNotFound
	}
	return s.Load(id)
}

func (s *Store) Load(id string) (*Trace, error) {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return New(id, s.limitOrDefault()), nil
	}
	if strings.Contains(id, "/") || strings.Contains(id, "\\") || strings.Contains(id, "..") {
		return nil, ErrNotFound
	}
	raw, err := atomicfile.ReadBounded(s.path(id), 64<<10)
	if err != nil {
		if os.IsNotExist(err) {
			return New(id, s.limitOrDefault()), nil
		}
		return New(id, s.limitOrDefault()), nil
	}
	var persisted persistedTrace
	if json.Unmarshal(raw, &persisted) != nil {
		return New(id, s.limitOrDefault()), nil
	}
	tr := New(firstNonEmpty(persisted.ID, id), s.limitOrDefault())
	if persisted.Route != nil {
		tr.SetRoute(Route{
			RequestedProvider:  persisted.Route.RequestedProvider,
			ProviderConnection: persisted.Route.ProviderConnection,
			Model:              persisted.Route.Model,
		})
	}
	for _, ev := range persisted.Events {
		tr.SetAttempt(ev.Attempt)
		tr.Mark(Stage(ev.Stage), Side(ev.Side), Milestone(ev.Milestone), ev.OK, ev.Cause)
	}
	return tr, nil
}

func (s *Store) path(id string) string {
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, id)
	return filepath.Join(s.dir, safe+".json")
}

func (s *Store) limitOrDefault() int {
	if s == nil || s.limit <= 0 {
		return 32
	}
	return s.limit
}

func (s *Store) List() []string {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return nil
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	ids := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(ids)
	return ids
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
