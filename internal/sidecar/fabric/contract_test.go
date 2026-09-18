package fabric

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSummaryStatesAgreeAfterCreateStartClose(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "states"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if created.Summary.TaskState != "open" || created.Summary.RunState != "" || created.Summary.Terminal || created.Projection.Terminal {
		t.Fatalf("create %+v proj=%+v", created.Summary, created.Projection)
	}
	if err := repo.StartRun(id, "operator"); err != nil {
		t.Fatal(err)
	}
	started, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if started.Summary.TaskState != "open" || started.Summary.RunState != "running" || started.Summary.Terminal || started.Projection.Terminal {
		t.Fatalf("start %+v proj=%+v", started.Summary, started.Projection)
	}
	if err := repo.CompleteRun(id, "operator"); err != nil {
		t.Fatal(err)
	}
	closed, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Summary.TaskState != "completed" || closed.Summary.RunState != "completed" || !closed.Summary.Terminal || !closed.Projection.Terminal {
		t.Fatalf("close %+v proj=%+v", closed.Summary, closed.Projection)
	}
	if err := repo.StartRun(id, "operator"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("start after close: %v", err)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerIsAuditNotFencingAndRejectsOverLimit(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	atLimit := strings.Repeat("o", maxOwnerLen)
	if err := repo.MarkStarted(id, atLimit); err != nil {
		t.Fatal(err)
	}
	started, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if started.Summary.RunState != "running" {
		t.Fatalf("running=%+v", started.Summary)
	}
	if started.Projection.CurrentOwner != "" || started.Projection.FencingToken != 0 {
		t.Fatalf("audit actor became fencing authority: %+v", started.Projection)
	}
	if started.Summary.CurrentOwner != "" {
		t.Fatalf("summary owner=%q", started.Summary.CurrentOwner)
	}
	foundActor := false
	for _, ev := range started.Events {
		if ev.EventType == EventRunStarted {
			if ev.ActorID != atLimit {
				t.Fatalf("actor=%q", ev.ActorID)
			}
			if ev.RuntimeSessionID != "" {
				t.Fatalf("session=%q", ev.RuntimeSessionID)
			}
			foundActor = true
		}
	}
	if !foundActor {
		t.Fatal("missing start event")
	}
	if err := repo.MarkClosed(id, "other-owner"); err != nil {
		t.Fatalf("audit close with different actor: %v", err)
	}
	closed, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Projection.CurrentOwner != "" || closed.Projection.FencingToken != 0 {
		t.Fatalf("close mutated fencing: %+v", closed.Projection)
	}

	id2, err := repo.Create(CreateTaskInput{Title: "session"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id2, "rs_a"); err != nil {
		t.Fatal(err)
	}
	rich, err := repo.Get(id2)
	if err != nil {
		t.Fatal(err)
	}
	if rich.Projection.CurrentOwner != "rs_a" {
		t.Fatalf("richer StartRun lost session owner: %+v", rich.Projection)
	}

	id3, err := repo.Create(CreateTaskInput{Title: "owner3"})
	if err != nil {
		t.Fatal(err)
	}
	over := strings.Repeat("o", maxOwnerLen+1)
	if err := repo.MarkStarted(id3, over); CodeOf(err) != CodeInvalidTask {
		t.Fatalf("over-limit start: %v", err)
	}
	prefix := strings.Repeat("o", maxOwnerLen)
	if err := repo.MarkStarted(id3, prefix+"x"); CodeOf(err) != CodeInvalidTask {
		t.Fatalf("prefix alias start: %v", err)
	}
	if err := repo.MarkStarted(id3, prefix); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkClosed(id3, over); CodeOf(err) != CodeInvalidTask {
		t.Fatalf("over-limit close: %v", err)
	}
}

func TestStartRunDoesNotWriteLeaseAndIgnoresHeldLease(t *testing.T) {
	dir := t.TempDir()
	repo := New(dir)
	id, err := repo.Create(CreateTaskInput{Title: "lease"})
	if err != nil {
		t.Fatal(err)
	}
	held := &Leases{Root: dir, TaskID: id}
	if _, err := held.AcquireWriteLease("other"); err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "operator"); err != nil {
		t.Fatalf("held lease blocked v1 start: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "leases", id+".lease.json")); err != nil {
		t.Fatalf("pre-existing lease vanished: %v", err)
	}
	id2, err := repo.Create(CreateTaskInput{Title: "nolease"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id2, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "leases", id2+".lease.json")); !os.IsNotExist(err) {
		t.Fatalf("start wrote lease: %v", err)
	}
}

func TestStartRunLeavesNoLeaseWhenAppendFails(t *testing.T) {
	dir := t.TempDir()
	repo := New(dir)
	id, err := repo.Create(CreateTaskInput{Title: "failappend"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".events.jsonl")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "operator"); err == nil {
		t.Fatal("start succeeded against directory log")
	}
	if _, err := os.Stat(filepath.Join(dir, "leases", id+".lease.json")); !os.IsNotExist(err) {
		t.Fatalf("failed start wrote lease: %v", err)
	}
}

func TestRemovedHistoryReclaimAllowsFutureCreates(t *testing.T) {
	origTotal, origLive := maxTotalTasks, maxLiveTasks
	maxTotalTasks = 3
	maxLiveTasks = 3
	defer func() {
		maxTotalTasks = origTotal
		maxLiveTasks = origLive
	}()
	repo := New(t.TempDir())
	a, err := repo.Create(CreateTaskInput{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	b, err := repo.Create(CreateTaskInput{Title: "B"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	c, err := repo.Create(CreateTaskInput{Title: "C"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(a); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := repo.Delete(b); err != nil {
		t.Fatal(err)
	}
	live := c
	if _, err := repo.Get(a); err != nil {
		t.Fatal(err)
	}
	d, err := repo.Create(CreateTaskInput{Title: "D"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(a); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oldest removed not reclaimed: %v", err)
	}
	if _, err := repo.Get(b); err != nil {
		t.Fatalf("newer removed reclaimed: %v", err)
	}
	if _, err := repo.Get(live); err != nil {
		t.Fatalf("live evicted: %v", err)
	}
	if _, err := repo.Get(d); err != nil {
		t.Fatal(err)
	}
}

func TestReclaimNeverEvictsLiveTasks(t *testing.T) {
	origTotal, origLive := maxTotalTasks, maxLiveTasks
	maxTotalTasks = 2
	maxLiveTasks = 2
	defer func() {
		maxTotalTasks = origTotal
		maxLiveTasks = origLive
	}()
	repo := New(t.TempDir())
	a, err := repo.Create(CreateTaskInput{Title: "live-a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.Create(CreateTaskInput{Title: "live-b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(CreateTaskInput{Title: "live-c"}); CodeOf(err) != CodeCapacityExceeded {
		t.Fatalf("live create: %v", err)
	}
	if _, err := repo.Get(a); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(b); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentCreateDeleteReclaim(t *testing.T) {
	origTotal, origLive := maxTotalTasks, maxLiveTasks
	maxTotalTasks = 8
	maxLiveTasks = 8
	defer func() {
		maxTotalTasks = origTotal
		maxLiveTasks = origLive
	}()
	repo := New(t.TempDir())
	var wg sync.WaitGroup
	ids := make(chan string, 32)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := repo.Create(CreateTaskInput{Title: "race"})
			if err != nil {
				return
			}
			_ = repo.Delete(id)
			ids <- id
		}()
	}
	wg.Wait()
	close(ids)
	if _, err := repo.Create(CreateTaskInput{Title: "after"}); err != nil {
		t.Fatalf("create after reclaim races: %v", err)
	}
}

func TestRunFailedIsTerminalAndUsesControlReserve(t *testing.T) {
	origOrdinary := maxOrdinaryEventsPerTask
	maxOrdinaryEventsPerTask = 4
	defer func() { maxOrdinaryEventsPerTask = origOrdinary }()
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "fail"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "rs_a"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxOrdinaryEventsPerTask-2; i++ {
		if err := repo.Record(id, EventClaimValidated, "", map[string]any{"claim_id": "c"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Record(id, EventClaimValidated, "", map[string]any{"claim_id": "x"}); CodeOf(err) != CodeCapacityExceeded {
		t.Fatalf("ordinary overflow: %v", err)
	}
	if err := repo.FailRun(id, "operator"); err != nil {
		t.Fatalf("fail after ordinary cap: %v", err)
	}
	failed, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Summary.TaskState != "failed" || failed.Summary.RunState != "failed" || !failed.Summary.Terminal || !failed.Projection.Terminal {
		t.Fatalf("failed summary=%+v proj=%+v", failed.Summary, failed.Projection)
	}
	if err := repo.StartRun(id, "rs_a"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("start after fail: %v", err)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatal(err)
	}
	if len(failed.Events)+1 > maxEventsAbsolute() {
		t.Fatalf("absolute exceeded")
	}
	id2, err := repo.Create(CreateTaskInput{Title: "close-still"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id2, "rs_a"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteRun(id2, "rs_a"); err != nil {
		t.Fatal(err)
	}
	closed, err := repo.Get(id2)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Summary.TaskState != "completed" || closed.Summary.RunState != "completed" {
		t.Fatalf("close path: %+v", closed.Summary)
	}
}

func TestLegacyOverCapHistoryCanStillCloseAndRemove(t *testing.T) {
	origOrdinary, origReserve := maxOrdinaryEventsPerTask, lifecycleEventReserve
	maxOrdinaryEventsPerTask = 4
	lifecycleEventReserve = 3
	defer func() {
		maxOrdinaryEventsPerTask = origOrdinary
		lifecycleEventReserve = origReserve
	}()
	abs := maxEventsAbsolute()
	for _, ordinary := range []int{
		maxOrdinaryEventsPerTask + 1,
		abs - 1,
		abs,
		abs + 5,
	} {
		ordinary := ordinary
		t.Run(fmt.Sprintf("close-ordinary-%d", ordinary), func(t *testing.T) {
			assertLegacyRunningEscape(t, ordinary, false)
		})
		t.Run(fmt.Sprintf("fail-ordinary-%d", ordinary), func(t *testing.T) {
			assertLegacyRunningEscape(t, ordinary, true)
		})
	}

	dir := t.TempDir()
	openID := "task_legacy_open"
	seedLegacyEvents(t, dir, openID, abs, false)
	repo := New(dir)
	if err := repo.Record(openID, EventClaimValidated, "", map[string]any{"claim_id": "more"}); CodeOf(err) != CodeCapacityExceeded {
		t.Fatalf("legacy ordinary: %v", err)
	}
	if err := repo.CancelTask(openID, ident("", 0)); err != nil {
		t.Fatalf("legacy cancel: %v", err)
	}
	if err := repo.Delete(openID); err != nil {
		t.Fatalf("legacy remove: %v", err)
	}

	modern, err := repo.Create(CreateTaskInput{Title: "modern"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(modern, "rs_a"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxOrdinaryEventsPerTask-2; i++ {
		if err := repo.Record(modern, EventClaimValidated, "", map[string]any{"n": i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Record(modern, EventClaimValidated, "", map[string]any{"n": "x"}); CodeOf(err) != CodeCapacityExceeded {
		t.Fatalf("modern ordinary: %v", err)
	}
	if err := repo.CompleteRun(modern, "rs_a"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(modern); err != nil {
		t.Fatal(err)
	}
	detail, err := repo.Get(modern)
	if err != nil {
		t.Fatal(err)
	}
	if countOrdinary(detail.Events) > maxOrdinaryEventsPerTask {
		t.Fatalf("modern used legacy exception: ordinary=%d", countOrdinary(detail.Events))
	}
	if len(detail.Events) > abs {
		t.Fatalf("modern exceeded abs: %d", len(detail.Events))
	}
	if err := repo.Record(modern, EventTaskCancelled, "", map[string]any{"status": "cancelled"}); err != nil {
		t.Fatal(err)
	}
	atCap, err := repo.Get(modern)
	if err != nil {
		t.Fatal(err)
	}
	if len(atCap.Events) != abs {
		t.Fatalf("modern abs=%d want %d", len(atCap.Events), abs)
	}
	if err := repo.Record(modern, EventTaskCancelled, "", map[string]any{"status": "cancelled"}); CodeOf(err) != CodeCapacityExceeded {
		t.Fatalf("modern extra control: %v", err)
	}
}

func assertLegacyRunningEscape(t *testing.T, ordinary int, fail bool) {
	t.Helper()
	dir := t.TempDir()
	id := "task_legacy_run"
	seedLegacyEvents(t, dir, id, ordinary, true)
	repo := New(dir)
	if err := repo.Record(id, EventClaimValidated, "", map[string]any{"claim_id": "more"}); CodeOf(err) != CodeCapacityExceeded {
		t.Fatalf("legacy ordinary: %v", err)
	}
	fresh := New(dir)
	got, err := fresh.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if countOrdinary(got.Events) != ordinary {
		t.Fatalf("restart ordinary=%d want %d", countOrdinary(got.Events), ordinary)
	}
	if fail {
		if err := fresh.FailRun(id, "operator"); err != nil {
			t.Fatalf("legacy fail: %v", err)
		}
	} else {
		if err := fresh.MarkClosed(id, "operator"); err != nil {
			t.Fatalf("legacy close: %v", err)
		}
	}
	if err := fresh.Delete(id); err != nil {
		t.Fatalf("legacy remove: %v", err)
	}
	if err := fresh.Delete(id); err != nil {
		t.Fatal(err)
	}
	done, err := fresh.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if fail {
		if done.Summary.TaskState != "failed" || done.Summary.RunState != "failed" || !done.Summary.Terminal {
			t.Fatalf("failed summary=%+v", done.Summary)
		}
	} else if done.Summary.TaskState != "completed" || done.Summary.RunState != "completed" {
		t.Fatalf("closed summary=%+v", done.Summary)
	}
	if !done.Projection.Removed {
		t.Fatal("missing tombstone")
	}
	if countOrdinary(done.Events) != ordinary {
		t.Fatalf("ordinary grew: %d", countOrdinary(done.Events))
	}
	if trailingControlCount(done.Events) > lifecycleEventReserve {
		t.Fatalf("unbounded control: %d", trailingControlCount(done.Events))
	}
	n := len(done.Events)
	if err := fresh.MarkClosed(id, "operator"); err == nil || (CodeOf(err) != CodeInvalidTransition && !errors.Is(err, ErrRemoved)) {
		t.Fatalf("repeat close: %v", err)
	}
	if err := fresh.FailRun(id, "operator"); err == nil || (CodeOf(err) != CodeInvalidTransition && !errors.Is(err, ErrRemoved)) {
		t.Fatalf("repeat fail: %v", err)
	}
	if err := fresh.Delete(id); err != nil {
		t.Fatal(err)
	}
	again, err := fresh.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Events) != n {
		t.Fatalf("repeat lifecycle grew history %d -> %d", n, len(again.Events))
	}
}

func seedLegacyEvents(t *testing.T, dir, taskID string, n int, started bool) {
	t.Helper()
	log := NewLog(dir, taskID)
	for i := 1; i <= n; i++ {
		et := EventClaimValidated
		payload := any(map[string]string{"k": "v"})
		session := ""
		if i == 1 {
			et = EventTaskCreated
			payload = map[string]string{"title": "legacy"}
		} else if started && i == 2 {
			et = EventRunStarted
			session = "rs_legacy"
			payload = map[string]string{"status": "running"}
		}
		ev := mkEvent(uint64(i), et, session, payload)
		ev.TaskID = taskID
		ev.EventID = fmt.Sprintf("evt_%d", i)
		if err := log.Append(ev); err != nil {
			t.Fatalf("seed %s %d: %v", taskID, i, err)
		}
	}
}

func TestIdentityRejectsSecretsAndHomePaths(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "priv"})
	if err != nil {
		t.Fatal(err)
	}
	secret := "sk-abcdefghijklmnopqrstuvwxyz012345"
	if err := repo.MarkStarted(id, secret); CodeOf(err) != CodeInvalidTask {
		t.Fatalf("secret start: %v", err)
	}
	if err := repo.MarkStarted(id, `C:\Users\alice\bin`); CodeOf(err) != CodeInvalidTask {
		t.Fatalf("home start: %v", err)
	}
	before, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if before.Summary.RunState != "" {
		t.Fatal("secret start mutated")
	}
	raw, err := os.ReadFile(filepath.Join(repo.Dir(), id+".events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), `C:\Users\alice`) {
		t.Fatalf("secret persisted:\n%s", raw)
	}
	uni := strings.Repeat("é", maxOwnerLen)
	if !utf8.ValidString(uni) {
		t.Fatal("test fixture")
	}
	if err := repo.MarkStarted(id, uni); err != nil {
		t.Fatalf("unicode start: %v", err)
	}
	if err := repo.MarkClosed(id, strings.Repeat("é", maxOwnerLen+1)); CodeOf(err) != CodeInvalidTask {
		t.Fatalf("unicode over: %v", err)
	}
	id2, err := repo.Create(CreateTaskInput{Title: "priv2"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CancelTask(id2, Identity{Principal: secret}); CodeOf(err) != CodeInvalidTask {
		t.Fatalf("secret cancel: %v", err)
	}
}
