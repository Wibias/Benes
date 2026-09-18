package continuation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/resourcebudget"
)

var (
	ErrEntryTooLarge   = errors.New("continuation entry exceeds per-entry byte limit")
	ErrStoreCapacity   = errors.New("continuation store capacity is exhausted by pinned state")
	ErrSnapshotVersion = errors.New("continuation snapshot version is not supported")
	ErrInvalidAnchor   = errors.New("continuation prefix has no valid provider occurrence anchor")
)

type StoreLimits struct {
	MaxEntryBytes int64
	MaxTotalBytes int64
	MaxEntries    int
	TTL           time.Duration
}

type Entry struct {
	Key     ReplayKey       `json:"key"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Prefix  Prefix          `json:"prefix"`
}

type storedEntry struct {
	entry     Entry
	size      int64
	expiresAt time.Time
	lastUsed  uint64
	pins      int
}

type Store struct {
	mu sync.Mutex

	limits StoreLimits
	now    func() time.Time
	rows   map[string]*storedEntry
	bytes  int64
	seq    uint64
}

type Lease struct {
	store       *Store
	key         string
	entry       Entry
	reservation *resourcebudget.Reservation
	once        sync.Once
}

type snapshot struct {
	Version int           `json:"version"`
	Entries []snapshotRow `json:"entries"`
}

type snapshotRow struct {
	Entry     Entry     `json:"entry"`
	ExpiresAt time.Time `json:"expires_at"`
	LastUsed  uint64    `json:"last_used"`
}

func NewStore(limits StoreLimits, now func() time.Time) *Store {
	if limits.MaxEntryBytes <= 0 {
		limits.MaxEntryBytes = 4 << 20
	}
	if limits.MaxTotalBytes <= 0 {
		limits.MaxTotalBytes = 64 << 20
	}
	if limits.MaxEntries <= 0 {
		limits.MaxEntries = 256
	}
	if limits.TTL <= 0 {
		limits.TTL = 30 * time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &Store{limits: limits, now: now, rows: make(map[string]*storedEntry)}
}

func (s *Store) Put(entry Entry) error {
	if s == nil {
		return fmt.Errorf("continuation store is nil")
	}
	if err := validateEntry(entry); err != nil {
		return err
	}
	size := entryRetainedBytes(entry)
	if size > s.limits.MaxEntryBytes {
		return ErrEntryTooLarge
	}
	key, err := replayKeyString(entry.Key)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.cleanupExpiredLocked(now)
	if current := s.rows[key]; current != nil {
		if current.pins > 0 {
			return ErrStoreCapacity
		}
		s.removeLocked(key, current)
	}
	if err := s.makeRoomLocked(size, now); err != nil {
		return err
	}
	s.seq++
	s.rows[key] = &storedEntry{
		entry:     cloneEntry(entry),
		size:      size,
		expiresAt: now.Add(s.limits.TTL),
		lastUsed:  s.seq,
	}
	s.bytes += size
	return nil
}

func (s *Store) Lease(key ReplayKey, owner Owner, turn *resourcebudget.Turn) (*Lease, bool, error) {
	if s == nil || turn == nil {
		return nil, false, nil
	}
	if !key.Owner.Matches(owner) {
		return nil, false, nil
	}
	keyString, err := replayKeyString(key)
	if err != nil {
		return nil, false, err
	}

	s.mu.Lock()
	now := s.now()
	row := s.rows[keyString]
	if row == nil {
		s.mu.Unlock()
		return nil, false, nil
	}
	if !row.entry.Key.Owner.Matches(owner) || !row.entry.Key.Owner.Matches(key.Owner) {
		s.mu.Unlock()
		return nil, false, nil
	}
	if !now.Before(row.expiresAt) {
		if row.pins == 0 {
			s.removeLocked(keyString, row)
		}
		s.mu.Unlock()
		return nil, false, nil
	}
	size := row.size
	entry := cloneEntry(row.entry)
	s.mu.Unlock()

	reservation, err := turn.Reserve(resourcebudget.ClassContinuation, size)
	if err != nil {
		return nil, false, err
	}

	s.mu.Lock()
	row = s.rows[keyString]
	if row == nil || !s.now().Before(row.expiresAt) || !row.entry.Key.Owner.Matches(owner) {
		s.mu.Unlock()
		reservation.Release()
		return nil, false, nil
	}
	row.pins++
	s.seq++
	row.lastUsed = s.seq
	entry = cloneEntry(row.entry)
	s.mu.Unlock()

	return &Lease{store: s, key: keyString, entry: entry, reservation: reservation}, true, nil
}

func (l *Lease) Entry() Entry {
	if l == nil {
		return Entry{}
	}
	return cloneEntry(l.entry)
}

func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		if l.store != nil {
			l.store.mu.Lock()
			if row := l.store.rows[l.key]; row != nil && row.pins > 0 {
				row.pins--
			}
			l.store.mu.Unlock()
		}
		if l.reservation != nil {
			l.reservation.Release()
		}
	})
}

func (s *Store) Peek(key ReplayKey, owner Owner) (Entry, bool) {
	if s == nil || !key.Owner.Matches(owner) {
		return Entry{}, false
	}
	keyString, err := replayKeyString(key)
	if err != nil {
		return Entry{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	row := s.rows[keyString]
	if row == nil || !s.now().Before(row.expiresAt) || !row.entry.Key.Owner.Matches(owner) {
		return Entry{}, false
	}
	return cloneEntry(row.entry), true
}

func (s *Store) Snapshot() ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("continuation store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(s.now())
	rows := make([]snapshotRow, 0, len(s.rows))
	for _, row := range s.rows {
		rows = append(rows, snapshotRow{Entry: cloneEntry(row.entry), ExpiresAt: row.expiresAt, LastUsed: row.lastUsed})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].LastUsed == rows[j].LastUsed {
			left, _ := replayKeyString(rows[i].Entry.Key)
			right, _ := replayKeyString(rows[j].Entry.Key)
			return left < right
		}
		return rows[i].LastUsed < rows[j].LastUsed
	})
	return json.Marshal(snapshot{Version: StoreVersion, Entries: rows})
}

func (s *Store) Restore(data []byte) error {
	if s == nil {
		return fmt.Errorf("continuation store is nil")
	}
	var decoded snapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("decode continuation snapshot: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = make(map[string]*storedEntry)
	s.bytes = 0
	s.seq = 0
	if decoded.Version != StoreVersion {
		return ErrSnapshotVersion
	}
	now := s.now()
	for _, persisted := range decoded.Entries {
		if err := validateEntry(persisted.Entry); err != nil {
			continue
		}
		if !persisted.ExpiresAt.After(now) {
			continue
		}
		size := entryRetainedBytes(persisted.Entry)
		if size > s.limits.MaxEntryBytes {
			continue
		}
		key, err := replayKeyString(persisted.Entry.Key)
		if err != nil {
			continue
		}
		if _, duplicate := s.rows[key]; duplicate {
			continue
		}
		if len(s.rows)+1 > s.limits.MaxEntries || s.bytes+size > s.limits.MaxTotalBytes {
			continue
		}
		lastUsed := persisted.LastUsed
		if lastUsed == 0 {
			lastUsed = s.seq + 1
		}
		if lastUsed > s.seq {
			s.seq = lastUsed
		}
		s.rows[key] = &storedEntry{entry: cloneEntry(persisted.Entry), size: size, expiresAt: persisted.ExpiresAt, lastUsed: lastUsed}
		s.bytes += size
	}
	return nil
}

func (s *Store) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked(s.now())
	return len(s.rows)
}

func (s *Store) makeRoomLocked(incoming int64, now time.Time) error {
	if incoming > s.limits.MaxTotalBytes {
		return ErrEntryTooLarge
	}
	for len(s.rows)+1 > s.limits.MaxEntries || s.bytes+incoming > s.limits.MaxTotalBytes {
		key, row := s.oldestEvictableLocked(now)
		if row == nil {
			return ErrStoreCapacity
		}
		s.removeLocked(key, row)
	}
	return nil
}

func (s *Store) oldestEvictableLocked(now time.Time) (string, *storedEntry) {
	var selectedKey string
	var selected *storedEntry
	for key, row := range s.rows {
		if row.pins > 0 {
			continue
		}
		if !now.Before(row.expiresAt) {
			return key, row
		}
		if selected == nil || row.lastUsed < selected.lastUsed {
			selectedKey, selected = key, row
		}
	}
	return selectedKey, selected
}

func (s *Store) cleanupExpiredLocked(now time.Time) {
	for key, row := range s.rows {
		if row.pins == 0 && !now.Before(row.expiresAt) {
			s.removeLocked(key, row)
		}
	}
}

func (s *Store) removeLocked(key string, row *storedEntry) {
	delete(s.rows, key)
	s.bytes -= row.size
	if s.bytes < 0 {
		s.bytes = 0
	}
}

func validateEntry(entry Entry) error {
	if strings.TrimSpace(entry.Key.Thread) == "" || strings.TrimSpace(entry.Key.CallID) == "" {
		return fmt.Errorf("continuation replay key thread and call id are required")
	}
	owner := entry.Key.Owner
	if strings.TrimSpace(owner.Provider) == "" || strings.TrimSpace(owner.Destination) == "" || strings.TrimSpace(owner.Adapter) == "" || strings.TrimSpace(owner.Model) == "" || strings.TrimSpace(owner.Credential) == "" {
		return fmt.Errorf("continuation replay owner is incomplete")
	}
	if !validPrefixAnchor(entry.Prefix) {
		return ErrInvalidAnchor
	}
	if len(entry.Payload) > 0 && !json.Valid(entry.Payload) {
		return fmt.Errorf("continuation payload is invalid JSON")
	}
	for _, occurrence := range entry.Prefix.Items {
		if len(occurrence.Payload) > 0 && !json.Valid(occurrence.Payload) {
			return fmt.Errorf("continuation occurrence payload is invalid JSON")
		}
	}
	return nil
}

func replayKeyString(key ReplayKey) (string, error) {
	encoded, err := json.Marshal(key)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func entryRetainedBytes(entry Entry) int64 {
	total := int64(len(entry.Payload))
	for _, item := range entry.Prefix.Items {
		total += int64(len(item.Kind) + len(item.ID) + len(item.CallID) + len(item.Payload))
	}
	return total
}

func cloneEntry(entry Entry) Entry {
	out := entry
	out.Payload = append(json.RawMessage(nil), entry.Payload...)
	out.Prefix.Items = make([]Occurrence, len(entry.Prefix.Items))
	for i, item := range entry.Prefix.Items {
		out.Prefix.Items[i] = item
		out.Prefix.Items[i].Payload = append(json.RawMessage(nil), item.Payload...)
	}
	return out
}
