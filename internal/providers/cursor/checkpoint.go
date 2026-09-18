package cursor

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

type Checkpoint struct {
	ConversationID string
	Identity       string
	Model          string
	PrefixDigest   string
	Bytes          []byte
	BlobIDs        [][]byte
	StoredAt       time.Time
	Isolated       bool
}

type CheckpointStore struct {
	mu       sync.Mutex
	entries  []Checkpoint
	ttl      time.Duration
	maxN     int
	maxBytes int64
	used     int64
	blobs    *BlobStore
}

func NewCheckpointStore() *CheckpointStore {
	return &CheckpointStore{ttl: time.Hour, maxN: 32, maxBytes: 8 << 20}
}

func (s *CheckpointStore) BindBlobs(store *BlobStore) {
	if s == nil {
		return
	}
	s.blobs = store
}

func PrefixDigest(system []string, messages []string) string {
	var b strings.Builder
	for _, line := range system {
		b.WriteString(line)
		b.WriteByte(0)
	}
	b.WriteByte('|')
	for _, message := range messages {
		b.WriteString(message)
		b.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func (s *CheckpointStore) Remember(cp Checkpoint) {
	if s == nil || len(cp.Bytes) == 0 || cp.ConversationID == "" || cp.Isolated {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.maxBytes > 0 && int64(len(cp.Bytes)) > s.maxBytes {
		return
	}
	s.entries = append(s.entries, cp)
	s.used += int64(len(cp.Bytes))
	s.evictLocked()
}

func (s *CheckpointStore) evictLocked() {
	now := time.Now()
	kept := s.entries[:0]
	var used int64
	for _, entry := range s.entries {
		if s.ttl > 0 && now.Sub(entry.StoredAt) > s.ttl {
			s.releaseLocked(entry)
			continue
		}
		kept = append(kept, entry)
		used += int64(len(entry.Bytes))
	}
	s.entries = kept
	s.used = used
	for (s.maxN > 0 && len(s.entries) > s.maxN) || (s.maxBytes > 0 && s.used > s.maxBytes) {
		s.releaseLocked(s.entries[0])
		s.used -= int64(len(s.entries[0].Bytes))
		s.entries = s.entries[1:]
	}
}

func (s *CheckpointStore) releaseLocked(cp Checkpoint) {
	if s.blobs == nil {
		return
	}
	for _, id := range cp.BlobIDs {
		s.blobs.Delete(id)
	}
}

func (s *CheckpointStore) Forget(conversationID, identity, model, digest string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.entries[:0]
	var used int64
	for _, entry := range s.entries {
		if entry.ConversationID == conversationID && entry.Identity == identity && entry.Model == model && entry.PrefixDigest == digest {
			s.releaseLocked(entry)
			continue
		}
		kept = append(kept, entry)
		used += int64(len(entry.Bytes))
	}
	s.entries = kept
	s.used = used
}

func (s *CheckpointStore) Lookup(conversationID, identity, model, digest string) (Checkpoint, bool) {
	if s == nil {
		return Checkpoint{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var found []Checkpoint
	now := time.Now()
	for _, entry := range s.entries {
		if s.ttl > 0 && now.Sub(entry.StoredAt) > s.ttl {
			continue
		}
		if entry.ConversationID == conversationID && entry.Identity == identity && entry.Model == model && entry.PrefixDigest == digest {
			found = append(found, entry)
		}
	}
	if len(found) != 1 {
		return Checkpoint{}, false
	}
	return found[0], true
}
