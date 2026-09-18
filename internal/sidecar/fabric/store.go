package fabric

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const taskIDCreateTries = 8

var (
	maxLiveTasks             = 256
	maxTotalTasks            = 1024
	maxOrdinaryEventsPerTask = 256
	lifecycleEventReserve    = 8
)

func maxEventsAbsolute() int {
	return maxOrdinaryEventsPerTask + lifecycleEventReserve
}

func isLifecycleControlEvent(eventType string) bool {
	switch eventType {
	case EventRunCompleted, EventRunFailed, EventRunCancelled, EventRunInterrupted,
		EventChildRunCompleted, EventChildRunFailed, EventChildRunCancelled, EventChildRunInterrupted,
		EventHandoffProposed, EventHandoffCommitted, EventHandoffRolledBack, EventHandoffFailed,
		EventTaskCompleted, EventTaskCancelled, EventTaskRemoved:
		return true
	default:
		return false
	}
}

var (
	ErrNotFound    = errors.New("task not found")
	ErrStaleWriter = errors.New("stale writer")
	ErrRemoved     = errors.New("task has been removed")
	ErrActiveRun   = errors.New("cannot remove task while the primary run is active")
)

// Log is an append-only JSONL event log for one task.
type Log struct {
	Path string
	mu   sync.Mutex
}

func NewLog(dir, taskID string) *Log {
	return &Log{Path: filepath.Join(dir, taskID+".events.jsonl")}
}

func (l *Log) lastEvent() (*Event, error) {
	data, err := os.ReadFile(l.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return nil, nil
	}
	var last Event
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		return nil, err
	}
	return &last, nil
}

// Last returns the most recent event, or nil when the log is empty.
func (l *Log) Last() (*Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastEvent()
}

// Append appends an event, enforcing expected sequence and the hash chain.
// Writers that present the wrong sequence are rejected as stale.
func (l *Log) Append(ev Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	last, err := l.lastEvent()
	if err != nil {
		return err
	}
	wantSeq := uint64(1)
	prevHash := ""
	if last != nil {
		wantSeq = last.Sequence + 1
		prevHash = last.EventHash
	}
	if ev.Sequence != wantSeq {
		return fmt.Errorf("%w: expected sequence %d, got %d", ErrStaleWriter, wantSeq, ev.Sequence)
	}
	ev.Payload = sanitizePayload(ev.Payload)
	ev.PreviousEventHash = prevHash
	h, err := HashEvent(ev)
	if err != nil {
		return err
	}
	ev.EventHash = h
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(l.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return err
	}
	return f.Sync()
}

// All reads every event in sequence order.
func (l *Log) All() ([]Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	data, err := os.ReadFile(l.Path)
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
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, nil
}

// Rebuild replays the log, verifying the hash chain.
func (l *Log) Rebuild() (*Projection, error) {
	evs, err := l.All()
	if err != nil {
		return nil, err
	}
	return Rebuild(evs)
}

// Repo is a directory-backed task store. Dir is supplied by the caller.
type Repo struct {
	dir string
	mu  sync.Mutex
}

func New(dir string) *Repo {
	return &Repo{dir: dir}
}

func (r *Repo) Dir() string { return r.dir }

type CreateTaskInput struct {
	Title string
	Goal  string
}

func (r *Repo) log(taskID string) (*Log, error) {
	if err := validTaskID(taskID); err != nil {
		return nil, InvalidTask("invalid task id")
	}
	return NewLog(r.dir, taskID), nil
}

func (r *Repo) leases(taskID string) *Leases {
	return &Leases{Root: r.dir, TaskID: taskID}
}

func (r *Repo) Create(in CreateTaskInput) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return "", InvalidTask("title is required")
	}
	if len(title) > maxTitleLen {
		return "", InvalidTask("title exceeds 120 characters")
	}
	goal := strings.TrimSpace(in.Goal)
	if len(goal) > maxGoalLen {
		return "", InvalidTask("goal exceeds 200 characters")
	}
	if goal != "" && looksLikePrompt(goal) {
		return "", InvalidTask("goal is not allowed")
	}
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return "", err
	}
	if err := r.ensureCreateCapacityLocked(); err != nil {
		return "", err
	}
	taskID, err := r.allocateTaskIDLocked()
	if err != nil {
		return "", err
	}
	payload := map[string]any{"title": title}
	if goal != "" {
		payload["goal"] = goal
	}
	payload["acceptance_criteria_hash"] = sha256Hex("")
	if err := r.appendLocked(taskID, EventTaskCreated, "", "operator", "kernel", payload); err != nil {
		return "", err
	}
	return taskID, nil
}

func (r *Repo) List() ([]TaskSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []TaskSummary
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".events.jsonl") {
			continue
		}
		taskID := strings.TrimSuffix(name, ".events.jsonl")
		log, err := r.log(taskID)
		if err != nil {
			continue
		}
		events, err := log.All()
		if err != nil || len(events) == 0 {
			continue
		}
		p, err := Rebuild(events)
		if err != nil || p.Removed {
			continue
		}
		out = append(out, projectTaskSummary(*p, events))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LatestActivity != out[j].LatestActivity {
			return out[i].LatestActivity > out[j].LatestActivity
		}
		return out[i].TaskID < out[j].TaskID
	})
	return out, nil
}

func (r *Repo) Get(taskID string) (*TaskDetail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.getLocked(taskID)
}

func (r *Repo) getLocked(taskID string) (*TaskDetail, error) {
	log, err := r.log(taskID)
	if err != nil {
		return nil, err
	}
	events, err := log.All()
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, taskID)
	}
	p, err := Rebuild(events)
	if err != nil {
		return nil, err
	}
	return &TaskDetail{
		Summary:    projectTaskSummary(*p, events),
		Projection: *p,
		Timeline:   projectTimeline(events),
		Events:     events,
	}, nil
}

// Record appends a structured event with the next sequence number.
func (r *Repo) Record(taskID, eventType, sessionID string, payload any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.appendLocked(taskID, eventType, sessionID, "operator", "kernel", payload)
}

func (r *Repo) CompleteRun(taskID, owner string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.completeRunLocked(taskID, owner, true)
}

// MarkClosed records v1 operator close. actor is audit-only.
func (r *Repo) MarkClosed(taskID, actor string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.completeRunLocked(taskID, actor, false)
}

func (r *Repo) completeRunLocked(taskID, actor string, runtimeSession bool) error {
	detail, err := r.getLocked(taskID)
	if err != nil {
		return err
	}
	if detail.Projection.Removed {
		return ErrRemoved
	}
	if !hasActivePrimaryRun(detail.Events) {
		if detail.Projection.Terminal {
			return InvalidTransition("task is already closed")
		}
		return InvalidTransition("task is not started")
	}
	actor, err = validateIdentity(actor, "owner")
	if err != nil {
		return err
	}
	if actor == "" {
		actor = "operator"
	}
	session := ""
	if runtimeSession {
		session = actor
	}
	return r.appendLocked(taskID, EventRunCompleted, session, "operator", actor, map[string]any{
		"actor":  actor,
		"status": "completed",
	})
}

func (r *Repo) StartRun(taskID, owner string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startRunLocked(taskID, owner, true)
}

// MarkStarted records v1 operator start. actor is audit-only and does not
// become CurrentOwner or fencing identity.
func (r *Repo) MarkStarted(taskID, actor string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.startRunLocked(taskID, actor, false)
}

func (r *Repo) startRunLocked(taskID, actor string, runtimeSession bool) error {
	detail, err := r.getLocked(taskID)
	if err != nil {
		return err
	}
	if detail.Projection.Removed {
		return ErrRemoved
	}
	if detail.Projection.Terminal {
		return InvalidTransition("task is already closed")
	}
	if hasActivePrimaryRun(detail.Events) {
		return InvalidTransition("task is already started")
	}
	actor, err = validateIdentity(actor, "owner")
	if err != nil {
		return err
	}
	if actor == "" {
		actor = "operator"
	}
	session := ""
	if runtimeSession {
		session = actor
	}
	return r.appendLocked(taskID, EventRunStarted, session, "operator", actor, map[string]any{
		"actor":  actor,
		"status": "running",
	})
}

func (r *Repo) FailRun(taskID, actor string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	detail, err := r.getLocked(taskID)
	if err != nil {
		return err
	}
	if detail.Projection.Removed {
		return ErrRemoved
	}
	if !hasActivePrimaryRun(detail.Events) {
		if detail.Projection.Terminal {
			return InvalidTransition("task is already closed")
		}
		return InvalidTransition("task is not started")
	}
	actor, err = validateIdentity(actor, "owner")
	if err != nil {
		return err
	}
	if actor == "" {
		actor = "operator"
	}
	return r.appendLocked(taskID, EventRunFailed, "", "operator", actor, map[string]any{
		"actor":  actor,
		"status": "failed",
	})
}

// appendLockedHook, when non-nil, short-circuits appendLocked (tests inject
// terminal persistence failures). Production keeps this nil.
var appendLockedHook func(taskID, eventType string) error

// SetAppendLockedHookForTest injects append failures for tests. Pass nil to clear.
func SetAppendLockedHookForTest(fn func(taskID, eventType string) error) {
	appendLockedHook = fn
}

func (r *Repo) appendLocked(taskID, eventType, sessionID, actorType, actorID string, payload any) error {
	if appendLockedHook != nil {
		if err := appendLockedHook(taskID, eventType); err != nil {
			return err
		}
	}
	log, err := r.log(taskID)
	if err != nil {
		return err
	}
	events, err := log.All()
	if err != nil {
		return err
	}
	if err := checkEventCapacity(events, eventType); err != nil {
		return err
	}
	seq := uint64(len(events) + 1)
	var raw json.RawMessage
	if payload != nil {
		raw, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	now := time.Now().UnixMilli()
	return log.Append(Event{
		TaskID:           taskID,
		Sequence:         seq,
		EventID:          newEventID(seq),
		EventType:        eventType,
		SchemaVersion:    SchemaVersion,
		OccurredAt:       now,
		ActorType:        actorType,
		ActorID:          actorID,
		RuntimeSessionID: sessionID,
		Payload:          raw,
	})
}

func (r *Repo) allocateTaskIDLocked() (string, error) {
	for i := 0; i < taskIDCreateTries; i++ {
		taskID, err := newTaskID()
		if err != nil {
			return "", err
		}
		path := filepath.Join(r.dir, taskID+".events.jsonl")
		_, err = os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return taskID, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", CapacityExceeded("task id allocation failed")
}

func checkEventCapacity(events []Event, nextType string) error {
	ordinary, control := 0, 0
	for _, ev := range events {
		if isLifecycleControlEvent(ev.EventType) {
			control++
		} else {
			ordinary++
		}
	}
	total := ordinary + control
	controlNext := isLifecycleControlEvent(nextType)
	if ordinary > maxOrdinaryEventsPerTask {
		if !controlNext {
			return CapacityExceeded("task event capacity exceeded")
		}
		if trailingControlCount(events) >= lifecycleEventReserve {
			return CapacityExceeded("task event capacity exceeded")
		}
		return nil
	}
	if controlNext {
		if total >= maxEventsAbsolute() {
			return CapacityExceeded("task event capacity exceeded")
		}
		return nil
	}
	if ordinary >= maxOrdinaryEventsPerTask {
		return CapacityExceeded("task event capacity exceeded")
	}
	return nil
}

func trailingControlCount(events []Event) int {
	n := 0
	for i := len(events) - 1; i >= 0; i-- {
		if !isLifecycleControlEvent(events[i].EventType) {
			break
		}
		n++
	}
	return n
}

func (r *Repo) ensureCreateCapacityLocked() error {
	live, total, err := r.taskCountsLocked()
	if err != nil {
		return err
	}
	if live >= maxLiveTasks {
		return CapacityExceeded("task capacity exceeded")
	}
	if total < maxTotalTasks {
		return nil
	}
	if err := r.reclaimOldestRemovedLocked(total - maxTotalTasks + 1); err != nil {
		return err
	}
	live, total, err = r.taskCountsLocked()
	if err != nil {
		return err
	}
	if live >= maxLiveTasks || total >= maxTotalTasks {
		return CapacityExceeded("task capacity exceeded")
	}
	return nil
}

type removedHistory struct {
	taskID string
	latest int64
}

func (r *Repo) reclaimOldestRemovedLocked(need int) error {
	if need <= 0 {
		return nil
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var removed []removedHistory
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".events.jsonl") {
			continue
		}
		taskID := strings.TrimSuffix(name, ".events.jsonl")
		log, err := r.log(taskID)
		if err != nil {
			continue
		}
		events, err := log.All()
		if err != nil || len(events) == 0 {
			continue
		}
		p, err := Rebuild(events)
		if err != nil || !p.Removed {
			continue
		}
		var latest int64
		for _, ev := range events {
			if ev.OccurredAt > latest {
				latest = ev.OccurredAt
			}
		}
		removed = append(removed, removedHistory{taskID: taskID, latest: latest})
	}
	sort.Slice(removed, func(i, j int) bool {
		if removed[i].latest != removed[j].latest {
			return removed[i].latest < removed[j].latest
		}
		return removed[i].taskID < removed[j].taskID
	})
	if len(removed) > need {
		removed = removed[:need]
	}
	for _, item := range removed {
		if err := r.deleteTaskFilesLocked(item.taskID); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) deleteTaskFilesLocked(taskID string) error {
	if err := validTaskID(taskID); err != nil {
		return InvalidTask("invalid task id")
	}
	logPath := filepath.Join(r.dir, taskID+".events.jsonl")
	if err := os.Remove(logPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	leasePath := filepath.Join(r.dir, "leases", taskID+".lease.json")
	if err := os.Remove(leasePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (r *Repo) taskCountsLocked() (live, total int, err error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".events.jsonl") {
			continue
		}
		total++
		taskID := strings.TrimSuffix(name, ".events.jsonl")
		log, err := r.log(taskID)
		if err != nil {
			continue
		}
		events, err := log.All()
		if err != nil || len(events) == 0 {
			continue
		}
		p, err := Rebuild(events)
		if err != nil || p.Removed {
			continue
		}
		live++
	}
	return live, total, nil
}

func newTaskID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("task_%d_%s", time.Now().UnixMilli(), hex.EncodeToString(b[:])), nil
}

func newEventID(seq uint64) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("evt_%d_%d", seq, time.Now().UnixNano())
	}
	return fmt.Sprintf("evt_%d_%s", seq, hex.EncodeToString(b[:]))
}
