package fabric

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestLifecycleCreateStartCloseDelete(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "board", Goal: "ship"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "task_") {
		t.Fatalf("id=%s", id)
	}
	created, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if created.Summary.TaskState != "open" || created.Summary.RunState != "" {
		t.Fatalf("created summary=%+v", created.Summary)
	}
	if err := repo.StartRun(id, "operator"); err != nil {
		t.Fatal(err)
	}
	started, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if started.Summary.TaskState != "open" || started.Summary.RunState != "running" {
		t.Fatalf("started summary=%+v", started.Summary)
	}
	if err := repo.CompleteRun(id, "operator"); err != nil {
		t.Fatal(err)
	}
	closed, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Summary.TaskState != "completed" || closed.Summary.RunState != "completed" || !closed.Summary.Terminal || !closed.Projection.Terminal {
		t.Fatalf("closed summary=%+v proj=%+v", closed.Summary, closed.Projection)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatal(err)
	}
	removed, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !removed.Projection.Removed {
		t.Fatal("delete did not tombstone")
	}
	list, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("list after delete=%+v", list)
	}
}

func TestLifecycleInvalidTransitions(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteRun(id, "operator"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("create->close: %v", err)
	}
	if err := repo.StartRun(id, "operator"); err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "operator"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("start->start: %v", err)
	}
	if err := repo.Delete(id); !errors.Is(err, ErrActiveRun) {
		t.Fatalf("start->delete: %v", err)
	}
	if err := repo.CompleteRun(id, "operator"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteRun(id, "operator"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("close->close: %v", err)
	}
	if err := repo.StartRun(id, "operator"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("close->start: %v", err)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatal(err)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	nRemoved := 0
	nStarted := 0
	nClosed := 0
	for _, ev := range detail.Events {
		switch ev.EventType {
		case EventTaskRemoved:
			nRemoved++
		case EventRunStarted:
			nStarted++
		case EventRunCompleted:
			nClosed++
		}
	}
	if nRemoved != 1 || nStarted != 1 || nClosed != 1 {
		t.Fatalf("events started=%d closed=%d removed=%d", nStarted, nClosed, nRemoved)
	}
	if err := repo.StartRun(id, "operator"); !errors.Is(err, ErrRemoved) {
		t.Fatalf("start after delete: %v", err)
	}
	if err := repo.CompleteRun(id, "operator"); !errors.Is(err, ErrRemoved) {
		t.Fatalf("close after delete: %v", err)
	}
	if err := repo.CancelTask(id, currentIdent(t, repo, id)); !errors.Is(err, ErrRemoved) {
		t.Fatalf("cancel after delete: %v", err)
	}
}

func TestCreateRejectsOversizedFields(t *testing.T) {
	repo := New(t.TempDir())
	if _, err := repo.Create(CreateTaskInput{Title: ""}); CodeOf(err) != CodeInvalidTask {
		t.Fatalf("empty title: %v", err)
	}
	if _, err := repo.Create(CreateTaskInput{Title: strings.Repeat("a", maxTitleLen+1)}); CodeOf(err) != CodeInvalidTask {
		t.Fatalf("long title: %v", err)
	}
	if _, err := repo.Create(CreateTaskInput{Title: "ok", Goal: strings.Repeat("g", maxGoalLen+1)}); CodeOf(err) != CodeInvalidTask {
		t.Fatalf("long goal: %v", err)
	}
}

func TestInvalidTaskIDRejected(t *testing.T) {
	repo := New(t.TempDir())
	_, err := repo.Get("../etc/passwd")
	if CodeOf(err) != CodeInvalidTask {
		t.Fatalf("path id: %v", err)
	}
	_, err = repo.Get("")
	if CodeOf(err) != CodeInvalidTask {
		t.Fatalf("empty id: %v", err)
	}
	_, err = repo.Get("missing-task-id-ok")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestRestartPreservesTasksAndDoesNotResume(t *testing.T) {
	dir := t.TempDir()
	repo := New(dir)
	id, err := repo.Create(CreateTaskInput{Title: "persist"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "operator"); err != nil {
		t.Fatal(err)
	}
	fresh := New(dir)
	detail, err := fresh.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Summary.RunState != "running" {
		t.Fatalf("restart summary=%+v", detail.Summary)
	}
	if _, err := fresh.Get("task_0_deadbeef"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old id: %v", err)
	}
}

func TestEventOrderingAndAtomicity(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "order"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "operator"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteRun(id, "operator"); err != nil {
		t.Fatal(err)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Events) != 3 {
		t.Fatalf("events=%d", len(detail.Events))
	}
	for i, ev := range detail.Events {
		if ev.Sequence != uint64(i+1) {
			t.Fatalf("seq=%d want %d", ev.Sequence, i+1)
		}
		if ev.OccurredAt <= 0 {
			t.Fatalf("timestamp missing at %d", i)
		}
		if i > 0 && ev.OccurredAt < detail.Events[i-1].OccurredAt {
			t.Fatalf("timestamps moved backwards")
		}
	}
	if detail.Events[0].EventType != EventTaskCreated || detail.Events[1].EventType != EventRunStarted || detail.Events[2].EventType != EventRunCompleted {
		t.Fatalf("types=%v", []string{detail.Events[0].EventType, detail.Events[1].EventType, detail.Events[2].EventType})
	}
	if detail.Summary.TaskState != "completed" || detail.Summary.RunState != "completed" || !detail.Summary.Terminal || !detail.Projection.Terminal {
		t.Fatalf("split-brain summary=%+v proj=%+v", detail.Summary, detail.Projection)
	}
}

func TestEventCapacityAndLiveTaskRetention(t *testing.T) {
	origOrdinary, origLive := maxOrdinaryEventsPerTask, maxLiveTasks
	maxOrdinaryEventsPerTask = 4
	maxLiveTasks = 2
	defer func() {
		maxOrdinaryEventsPerTask = origOrdinary
		maxLiveTasks = origLive
	}()
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "cap"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "operator"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxOrdinaryEventsPerTask-2; i++ {
		if err := repo.Record(id, EventClaimValidated, "", map[string]any{"claim_id": "c"}); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	if err := repo.Record(id, EventClaimValidated, "", map[string]any{"claim_id": "overflow"}); CodeOf(err) != CodeCapacityExceeded {
		t.Fatalf("overflow: %v", err)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if countOrdinary(detail.Events) != maxOrdinaryEventsPerTask {
		t.Fatalf("ordinary=%d", countOrdinary(detail.Events))
	}
	if detail.Summary.TaskState != "open" || detail.Summary.RunState != "running" {
		t.Fatalf("active task mutated: %+v", detail.Summary)
	}
	if err := repo.CompleteRun(id, "operator"); err != nil {
		t.Fatalf("close after ordinary cap: %v", err)
	}
	if err := repo.CompleteRun(id, "operator"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("duplicate close: %v", err)
	}
	closed, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Summary.TaskState != "completed" || !closed.Summary.Terminal {
		t.Fatalf("closed=%+v", closed.Summary)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatalf("delete after close: %v", err)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatal(err)
	}
	removed, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !removed.Projection.Removed {
		t.Fatal("missing tombstone")
	}
	if len(removed.Events) > maxEventsAbsolute() {
		t.Fatalf("absolute cap exceeded: %d", len(removed.Events))
	}
	nClose, nRemoved := 0, 0
	for _, ev := range removed.Events {
		switch ev.EventType {
		case EventRunCompleted:
			nClose++
		case EventTaskRemoved:
			nRemoved++
		}
	}
	if nClose != 1 || nRemoved != 1 {
		t.Fatalf("duplicate control events close=%d removed=%d", nClose, nRemoved)
	}
	if _, err := repo.Create(CreateTaskInput{Title: "two"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(CreateTaskInput{Title: "three"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(CreateTaskInput{Title: "four"}); CodeOf(err) != CodeCapacityExceeded {
		t.Fatalf("live overflow: %v", err)
	}
	still, err := repo.Get(id)
	if err != nil || !still.Projection.Removed {
		t.Fatalf("history evicted: err=%v", err)
	}
}

func countOrdinary(events []Event) int {
	n := 0
	for _, ev := range events {
		if !isLifecycleControlEvent(ev.EventType) {
			n++
		}
	}
	return n
}

func TestConcurrentStartCloseDeleteAndCreates(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "race"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	startErrs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startErrs <- repo.StartRun(id, "operator")
		}()
	}
	wg.Wait()
	close(startErrs)
	ok, fail := 0, 0
	for err := range startErrs {
		if err == nil {
			ok++
		} else {
			fail++
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("start/start ok=%d fail=%d", ok, fail)
	}
	started, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	nStart := 0
	for _, ev := range started.Events {
		if ev.EventType == EventRunStarted {
			nStart++
		}
	}
	if nStart != 1 || started.Summary.RunState != "running" {
		t.Fatalf("start race events=%d summary=%+v", nStart, started.Summary)
	}

	closeErrs := make(chan error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			closeErrs <- repo.CompleteRun(id, "operator")
		}()
	}
	wg.Wait()
	close(closeErrs)
	ok, fail = 0, 0
	for err := range closeErrs {
		if err == nil {
			ok++
		} else {
			fail++
		}
	}
	if ok != 1 || fail != 1 {
		t.Fatalf("close/close ok=%d fail=%d", ok, fail)
	}

	id2, err := repo.Create(CreateTaskInput{Title: "race-del"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id2, "operator"); err != nil {
		t.Fatal(err)
	}
	var startClose sync.WaitGroup
	var startErr, closeErr error
	startClose.Add(2)
	go func() {
		defer startClose.Done()
		startErr = repo.StartRun(id2, "operator")
	}()
	go func() {
		defer startClose.Done()
		closeErr = repo.CompleteRun(id2, "operator")
	}()
	startClose.Wait()
	if startErr == nil && closeErr == nil {
		t.Fatal("start and close both succeeded on an already-started task")
	}
	detail, err := repo.Get(id2)
	if err != nil {
		t.Fatal(err)
	}
	nStart, nClose := 0, 0
	for _, ev := range detail.Events {
		switch ev.EventType {
		case EventRunStarted:
			nStart++
		case EventRunCompleted:
			nClose++
		}
	}
	if nStart != 1 {
		t.Fatalf("start/close started=%d closed=%d", nStart, nClose)
	}
	if nClose == 1 && detail.Summary.RunState != "completed" {
		t.Fatalf("closed event without completed state: %+v", detail.Summary)
	}
	if nClose == 0 && detail.Summary.RunState != "running" {
		t.Fatalf("running without close, state=%+v", detail.Summary)
	}

	ids := make([]string, 32)
	createErrs := make(chan error, 32)
	var createWG sync.WaitGroup
	createWG.Add(32)
	for i := 0; i < 32; i++ {
		i := i
		go func() {
			defer createWG.Done()
			id, err := repo.Create(CreateTaskInput{Title: "c"})
			ids[i] = id
			createErrs <- err
		}()
	}
	createWG.Wait()
	close(createErrs)
	seen := map[string]struct{}{}
	for i, id := range ids {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %s", id)
		}
		seen[id] = struct{}{}
		_ = i
	}
	for err := range createErrs {
		if err != nil {
			t.Fatal(err)
		}
	}

	target, err := repo.Create(CreateTaskInput{Title: "list-race"})
	if err != nil {
		t.Fatal(err)
	}
	var listWG sync.WaitGroup
	listWG.Add(3)
	go func() {
		defer listWG.Done()
		_ = repo.StartRun(target, "operator")
	}()
	go func() {
		defer listWG.Done()
		if _, err := repo.List(); err != nil {
			t.Errorf("list: %v", err)
		}
	}()
	go func() {
		defer listWG.Done()
		if _, err := repo.Get(target); err != nil && !errors.Is(err, ErrNotFound) && CodeOf(err) != CodeInvalidTask {
			t.Errorf("get: %v", err)
		}
	}()
	listWG.Wait()
}

func TestDeleteDoesNotEmitClose(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "noclose"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(id); err != nil {
		t.Fatal(err)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range detail.Events {
		if ev.EventType == EventRunCompleted || ev.EventType == EventTaskCompleted {
			t.Fatalf("delete emitted %s", ev.EventType)
		}
	}
}
