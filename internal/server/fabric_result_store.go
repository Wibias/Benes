package server

import (
	"strings"
	"sync"
	"time"
)

const (
	fabricResultMaxItems   = 64
	fabricResultMaxBytes   = 1 << 20 // 1 MiB total across material fields
	fabricResultMaxPerItem = 64 << 10
	fabricResultTTL        = 15 * time.Minute
)

// fabricResultStore is a bounded server-owned in-memory result cache for
// completed Fabric executes. Results do NOT survive process restart. Entries
// expire by TTL and are evicted by true LRU (get refreshes recency) when
// item/byte caps are hit. Raw output is never written to Fabric events,
// leases, debug dumps, or timeline logs.
type fabricResultStore struct {
	mu         sync.Mutex
	items      map[string]*fabricStoredResult
	order      []string // LRU: oldest/least-recent -> newest/most-recent
	totalBytes int
}

type fabricStoredResult struct {
	Handle    string    `json:"handle"`
	RunID     string    `json:"runId"`
	TaskID    string    `json:"taskId"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason,omitempty"`
	Output    string    `json:"output,omitempty"`
	Provider  string    `json:"provider,omitempty"`
	Model     string    `json:"model,omitempty"`
	RequestID string    `json:"requestId,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	bytes     int
}

func newFabricResultStore() *fabricResultStore {
	return &fabricResultStore{items: map[string]*fabricStoredResult{}}
}

func fabricResultMaterialBytes(res *fabricStoredResult) int {
	if res == nil {
		return 0
	}
	return len(res.Handle) + len(res.RunID) + len(res.TaskID) + len(res.Status) +
		len(res.Reason) + len(res.Output) + len(res.Provider) + len(res.Model) + len(res.RequestID)
}

func (s *fabricResultStore) put(res fabricStoredResult) string {
	if s == nil {
		return ""
	}
	if res.Status != "completed" {
		// Failed/cancel/interrupt are not success results - do not store usable output.
		return ""
	}
	now := time.Now().UTC()
	out, capped := clampFabricResultOutput(res.Output)
	truncated := res.Truncated || capped
	handle := res.Handle
	if handle == "" {
		handle = "fr_" + res.RunID
	}
	item := &fabricStoredResult{
		Handle:    handle,
		RunID:     res.RunID,
		TaskID:    res.TaskID,
		Status:    res.Status,
		Reason:    res.Reason,
		Output:    out,
		Provider:  res.Provider,
		Model:     res.Model,
		RequestID: res.RequestID,
		Truncated: truncated,
		CreatedAt: now,
		ExpiresAt: now.Add(fabricResultTTL),
	}
	item.bytes = fabricResultMaterialBytes(item)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked(now)
	if old, ok := s.items[handle]; ok {
		s.totalBytes -= old.bytes
		s.removeOrderLocked(handle)
		delete(s.items, handle)
	}
	for (len(s.items) >= fabricResultMaxItems || s.totalBytes+item.bytes > fabricResultMaxBytes) && len(s.order) > 0 {
		victim := s.order[0]
		s.order = s.order[1:]
		if v, ok := s.items[victim]; ok {
			s.totalBytes -= v.bytes
			delete(s.items, victim)
		}
	}
	s.items[handle] = item
	s.order = append(s.order, handle)
	s.totalBytes += item.bytes
	return handle
}

func (s *fabricResultStore) get(handle string) (*fabricStoredResult, bool) {
	if s == nil {
		return nil, false
	}
	handle = strings.TrimSpace(handle)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked(time.Now().UTC())
	item, ok := s.items[handle]
	if !ok {
		return nil, false
	}
	s.touchLRULocked(handle)
	cp := *item
	return &cp, true
}

func (s *fabricResultStore) getByRun(taskID, runID string) (*fabricStoredResult, bool) {
	if s == nil {
		return nil, false
	}
	taskID = strings.TrimSpace(taskID)
	runID = strings.TrimSpace(runID)
	if taskID == "" || runID == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked(time.Now().UTC())
	for _, key := range s.order {
		item := s.items[key]
		if item != nil && item.TaskID == taskID && item.RunID == runID {
			s.touchLRULocked(key)
			cp := *item
			return &cp, true
		}
	}
	return nil, false
}

func (s *fabricResultStore) touchLRULocked(handle string) {
	s.removeOrderLocked(handle)
	s.order = append(s.order, handle)
}

func (s *fabricResultStore) clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = map[string]*fabricStoredResult{}
	s.order = nil
	s.totalBytes = 0
}

func (s *fabricResultStore) evictExpiredLocked(now time.Time) {
	kept := s.order[:0]
	for _, key := range s.order {
		item := s.items[key]
		if item == nil || !item.ExpiresAt.After(now) {
			if item != nil {
				s.totalBytes -= item.bytes
			}
			delete(s.items, key)
			continue
		}
		kept = append(kept, key)
	}
	s.order = kept
}

func (s *fabricResultStore) removeOrderLocked(handle string) {
	out := s.order[:0]
	for _, k := range s.order {
		if k != handle {
			out = append(out, k)
		}
	}
	s.order = out
}

func (s *fabricResultStore) lenForTest() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked(time.Now().UTC())
	return len(s.items)
}
