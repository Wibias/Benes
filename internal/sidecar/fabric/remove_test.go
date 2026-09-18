package fabric

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ident(owner string, fence int) Identity {
	return Identity{Principal: "p", ExpectedOwner: owner, ExpectedFencingToken: fence}
}

func currentIdent(t *testing.T, repo *Repo, taskID string) Identity {
	t.Helper()
	detail, err := repo.Get(taskID)
	if err != nil {
		t.Fatal(err)
	}
	return ident(detail.Projection.CurrentOwner, detail.Projection.FencingToken)
}

func TestRemoveTaskTombstone(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "t", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveTask(id, currentIdent(t, repo, id)); err != nil {
		t.Fatal(err)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Projection.Removed || !detail.Projection.Terminal {
		t.Fatalf("removed=%v terminal=%v", detail.Projection.Removed, detail.Projection.Terminal)
	}
}

func TestRemoveTaskIdempotent(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveTask(id, currentIdent(t, repo, id)); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveTask(id, currentIdent(t, repo, id)); err != nil {
		t.Fatal(err)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ev := range detail.Events {
		if ev.EventType == EventTaskRemoved {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want 1 TaskRemoved, got %d", n)
	}
}

func TestRemoveTaskRejectsActiveRun(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "rs_a"); err != nil {
		t.Fatal(err)
	}
	err = repo.RemoveTask(id, currentIdent(t, repo, id))
	if err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("want active-run rejection, got %v", err)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Projection.Removed {
		t.Fatal("active task was removed")
	}
}

func TestCompletedAndCancelledTasksMayBeRemoved(t *testing.T) {
	repo := New(t.TempDir())
	completed, err := repo.Create(CreateTaskInput{Title: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(completed, "rs_a"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteRun(completed, "rs_a"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveTask(completed, currentIdent(t, repo, completed)); err != nil {
		t.Fatal(err)
	}

	cancelled, err := repo.Create(CreateTaskInput{Title: "bye"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CancelTask(cancelled, currentIdent(t, repo, cancelled)); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveTask(cancelled, currentIdent(t, repo, cancelled)); err != nil {
		t.Fatal(err)
	}
}

func TestListHidesRemovedTasks(t *testing.T) {
	repo := New(t.TempDir())
	keep, err := repo.Create(CreateTaskInput{Title: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	drop, err := repo.Create(CreateTaskInput{Title: "drop"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveTask(drop, currentIdent(t, repo, drop)); err != nil {
		t.Fatal(err)
	}
	list, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].TaskID != keep {
		t.Fatalf("list=%+v want only %s", list, keep)
	}
	if _, err := repo.Get(drop); err != nil {
		t.Fatalf("removed history must stay readable: %v", err)
	}
}

func TestRemovedHistoryReplayableAndSurvivesRebuild(t *testing.T) {
	dir := t.TempDir()
	repo := New(dir)
	id, err := repo.Create(CreateTaskInput{Title: "t", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "rs_a"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Record(id, EventRunCompleted, "rs_a", map[string]string{"status": "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveTask(id, currentIdent(t, repo, id)); err != nil {
		t.Fatal(err)
	}
	fresh := New(dir)
	detail, err := fresh.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Projection.Removed {
		t.Fatal("rebuild dropped tombstone")
	}
	types := map[string]bool{}
	for _, ev := range detail.Events {
		types[ev.EventType] = true
	}
	if !types[EventTaskCreated] || !types[EventTaskRemoved] {
		t.Fatalf("history types=%v", types)
	}
	if len(detail.Events) < 3 {
		t.Fatalf("want replayable history, got %d events", len(detail.Events))
	}
}

func TestRemoveDoesNotAffectOtherTask(t *testing.T) {
	repo := New(t.TempDir())
	a, err := repo.Create(CreateTaskInput{Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.Create(CreateTaskInput{Title: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveTask(a, currentIdent(t, repo, a)); err != nil {
		t.Fatal(err)
	}
	da, _ := repo.Get(a)
	db, _ := repo.Get(b)
	if !da.Projection.Removed || db.Projection.Removed {
		t.Fatalf("A removed=%v B removed=%v", da.Projection.Removed, db.Projection.Removed)
	}
}

func TestRemoveRejectsStaleFenceAndWrongOwner(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "rs_a"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Record(id, EventRunCompleted, "rs_a", map[string]string{"status": "completed"}); err != nil {
		t.Fatal(err)
	}
	cur := currentIdent(t, repo, id)
	err = repo.RemoveTask(id, ident(cur.ExpectedOwner, cur.ExpectedFencingToken+999))
	if err == nil || !strings.Contains(err.Error(), "fencing") {
		t.Fatalf("want fencing mismatch, got %v", err)
	}
	err = repo.RemoveTask(id, ident("stale:9.9.9", cur.ExpectedFencingToken))
	if err == nil || !strings.Contains(err.Error(), "ownership") {
		t.Fatalf("want ownership changed, got %v", err)
	}
	err = repo.RemoveTask(id, ident("", cur.ExpectedFencingToken))
	if err == nil || (!strings.Contains(err.Error(), "expectedOwner") && !strings.Contains(err.Error(), "ownership")) {
		t.Fatalf("want missing-authority rejection, got %v", err)
	}
	detail, _ := repo.Get(id)
	if detail.Projection.Removed {
		t.Fatal("rejected remove still tombstoned")
	}
}

func TestPostRemoveLifecycleWritesRejected(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveTask(id, currentIdent(t, repo, id)); err != nil {
		t.Fatal(err)
	}
	cur := currentIdent(t, repo, id)
	if err := repo.CompleteTask(id, cur); err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("complete after remove: %v", err)
	}
	if err := repo.CancelTask(id, cur); err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("cancel after remove: %v", err)
	}
	if err := repo.StartRun(id, "rs_a"); err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("start after remove: %v", err)
	}
	detail, _ := repo.Get(id)
	for _, ev := range detail.Events {
		if ev.EventType == EventTaskCompleted || ev.EventType == EventTaskCancelled {
			t.Fatal("lifecycle event resurrected removed task")
		}
	}
}

func TestAbandonInitialInput(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "rs_a"); err != nil {
		t.Fatal(err)
	}
	st, err := repo.InitialInputState(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != InitialInputNone {
		t.Fatalf("status=%s want none", st.Status)
	}
	if err := repo.ReserveInitialInput(id, "sess-1"); err != nil {
		t.Fatal(err)
	}
	st, _ = repo.InitialInputState(id)
	if st.Status != InitialInputReserved {
		t.Fatalf("status=%s want reserved", st.Status)
	}

	dir := t.TempDir()
	repo2 := New(dir)
	id2, err := repo2.Create(CreateTaskInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo2.StartRun(id2, "rs_a"); err != nil {
		t.Fatal(err)
	}
	if err := repo2.ReserveInitialInput(id2, "sess-1"); err != nil {
		t.Fatal(err)
	}
	fresh := New(dir)
	st, err = fresh.InitialInputState(id2)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != InitialInputReserved {
		t.Fatalf("restart auto-advanced reserved state to %s", st.Status)
	}
	if err := fresh.ReserveInitialInput(id2, "sess-1"); err != nil {
		t.Fatal(err)
	}
	st, _ = fresh.InitialInputState(id2)
	if st.Status != InitialInputReserved {
		t.Fatalf("re-reserve changed state to %s", st.Status)
	}
}

func TestAbandonResolvesReservedAndUncertain(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "rs_a"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReserveInitialInput(id, "sess-1"); err != nil {
		t.Fatal(err)
	}
	out := repo.AbandonInitialInput(id, "sess-1", currentIdent(t, repo, id))
	if out.Status != OutcomeExecuted {
		t.Fatalf("abandon reserved: %+v", out)
	}
	st, _ := repo.InitialInputState(id)
	if st.Status != InitialInputAbandoned {
		t.Fatalf("status=%s", st.Status)
	}

	id2, err := repo.Create(CreateTaskInput{Title: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id2, "rs_b"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReserveInitialInput(id2, "sess-2"); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkInitialInputUncertain(id2, "sess-2", "transport timeout"); err != nil {
		t.Fatal(err)
	}
	st, _ = repo.InitialInputState(id2)
	if st.Status != InitialInputUncertain {
		t.Fatalf("status=%s want uncertain", st.Status)
	}
	if err := repo.ReserveInitialInput(id2, "sess-2"); err == nil || !strings.Contains(err.Error(), "uncertain") {
		t.Fatalf("blind retry after uncertain: %v", err)
	}
	out = repo.AbandonInitialInput(id2, "sess-2", currentIdent(t, repo, id2))
	if out.Status != OutcomeExecuted {
		t.Fatalf("abandon uncertain: %+v", out)
	}
	st, _ = repo.InitialInputState(id2)
	if st.Status != InitialInputAbandoned {
		t.Fatalf("status=%s", st.Status)
	}
}

func TestAbandonRejectsStaleAuthorityAndIsIdempotent(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "rs_a"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReserveInitialInput(id, "sess-1"); err != nil {
		t.Fatal(err)
	}
	cur := currentIdent(t, repo, id)
	out := repo.AbandonInitialInput(id, "sess-1", ident(cur.ExpectedOwner, cur.ExpectedFencingToken+7))
	if out.Status != OutcomeRejected {
		t.Fatalf("stale fence: %+v", out)
	}
	out = repo.AbandonInitialInput(id, "sess-1", ident("stale:9.9.9", cur.ExpectedFencingToken))
	if out.Status != OutcomeRejected {
		t.Fatalf("stale owner: %+v", out)
	}
	st, _ := repo.InitialInputState(id)
	if st.Status != InitialInputReserved {
		t.Fatalf("status=%s after rejected abandon", st.Status)
	}
	first := repo.AbandonInitialInput(id, "sess-1", cur)
	if first.Status != OutcomeExecuted {
		t.Fatalf("first abandon: %+v", first)
	}
	second := repo.AbandonInitialInput(id, "sess-1", cur)
	if second.Status != OutcomeAlready {
		t.Fatalf("second abandon: %+v", second)
	}
	detail, _ := repo.Get(id)
	n := 0
	for _, ev := range detail.Events {
		if ev.EventType == EventInitialInputAbandoned {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want 1 abandon event, got %d", n)
	}
}

func TestAbandonSurvivesReplayAndAllowsFreshReservation(t *testing.T) {
	dir := t.TempDir()
	repo := New(dir)
	id, err := repo.Create(CreateTaskInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "rs_a"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReserveInitialInput(id, "sess-1"); err != nil {
		t.Fatal(err)
	}
	out := repo.AbandonInitialInput(id, "sess-1", currentIdent(t, repo, id))
	if out.Status != OutcomeExecuted {
		t.Fatalf("%+v", out)
	}
	fresh := New(dir)
	st, err := fresh.InitialInputState(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != InitialInputAbandoned {
		t.Fatalf("replay status=%s", st.Status)
	}
	if err := fresh.ReserveInitialInput(id, "sess-1"); err != nil {
		t.Fatal(err)
	}
	st, _ = fresh.InitialInputState(id)
	if st.Status != InitialInputReserved {
		t.Fatalf("fresh generation status=%s", st.Status)
	}
}

func TestSentBlocksAbandonAndReserve(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "rs_a"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReserveInitialInput(id, "sess-1"); err != nil {
		t.Fatal(err)
	}
	if err := repo.ConfirmInitialInputSent(id, "sess-1"); err != nil {
		t.Fatal(err)
	}
	st, _ := repo.InitialInputState(id)
	if st.Status != InitialInputSent {
		t.Fatalf("status=%s", st.Status)
	}
	if err := repo.ReserveInitialInput(id, "sess-1"); err == nil || !strings.Contains(err.Error(), "already sent") {
		t.Fatalf("reserve after sent: %v", err)
	}
	out := repo.AbandonInitialInput(id, "sess-1", currentIdent(t, repo, id))
	if out.Status != OutcomeRejected {
		t.Fatalf("abandon after sent: %+v", out)
	}
}

func TestCreateDoesNotPersistPromptLikeGoal(t *testing.T) {
	dir := t.TempDir()
	repo := New(dir)
	prompt := "You are a helpful assistant. Here is the full user prompt with secret sk-abcdefghijklmnopqrstuvwxyz012345."
	id, err := repo.Create(CreateTaskInput{Title: "short title", Goal: prompt})
	if err == nil || CodeOf(err) != CodeInvalidTask {
		t.Fatalf("create prompt-like goal: id=%s err=%v", id, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".events.jsonl") {
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			s := string(raw)
			if strings.Contains(s, "You are a helpful assistant") || strings.Contains(s, "sk-abcdefghijklmnopqrstuvwxyz") {
				t.Fatalf("create persisted prompt/secret:\n%s", s)
			}
			t.Fatalf("prompt-like create wrote %s", e.Name())
		}
	}
}

func TestDashboardListAndDetail(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "board"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StartRun(id, "rs_a"); err != nil {
		t.Fatal(err)
	}
	list, err := repo.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Title != "board" || list[0].TaskID != id {
		t.Fatalf("list=%+v", list)
	}
	detail, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Summary.Title != "board" || detail.Projection.CurrentOwner != "rs_a" {
		t.Fatalf("detail summary=%+v proj=%+v", detail.Summary, detail.Projection)
	}
	if len(detail.Timeline) == 0 {
		t.Fatal("detail missing timeline")
	}
}
