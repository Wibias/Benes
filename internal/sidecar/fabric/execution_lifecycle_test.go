package fabric

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Execution lifecycle matrix A–H (21 cases).

func TestExecA1_BeginSetsOwnerFenceRunID(t *testing.T) {
	repo := New(t.TempDir())
	id, err := repo.Create(CreateTaskInput{Title: "a1"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := repo.BeginExecute(id, ExecuteInput{Owner: "worker", Model: "openai-apikey/gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	if run.RunID == "" || run.Owner != "worker" || run.Fence < 1 {
		t.Fatalf("%+v", run)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "running" || detail.Projection.CurrentOwner != "worker" {
		t.Fatalf("%+v", detail.Summary)
	}
}

func TestExecA2_OneActivePrimaryOnly(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "a2"})
	if _, err := repo.BeginExecute(id, ExecuteInput{Owner: "w"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginExecute(id, ExecuteInput{Owner: "w2"}); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("err=%v", err)
	}
}

func TestExecA3_CompleteIsTerminal(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "a3"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	if err := repo.CompleteExecute(run); err != nil {
		t.Fatal(err)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "completed" || !detail.Projection.Terminal {
		t.Fatalf("%+v", detail.Summary)
	}
}

func TestExecB1_FailIsTerminal(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "b1"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	if err := repo.FailExecute(run, "boom"); err != nil {
		t.Fatal(err)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "failed" || detail.Summary.TaskState != "failed" {
		t.Fatalf("%+v", detail.Summary)
	}
}

func TestExecB2_CancelIsTerminal(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "b2"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	if err := repo.CancelExecute(run, "operator_cancelled"); err != nil {
		t.Fatal(err)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "cancelled" {
		t.Fatalf("%+v", detail.Summary)
	}
}

func TestExecB3_InterruptLeavesTaskOpen(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "b3"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	if err := repo.InterruptExecute(run, "lost_on_restart"); err != nil {
		t.Fatal(err)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "interrupted" || detail.Projection.Terminal {
		t.Fatalf("%+v proj=%+v", detail.Summary, detail.Projection)
	}
	if _, err := repo.BeginExecute(id, ExecuteInput{Owner: "w2"}); err != nil {
		t.Fatalf("re-execute after interrupt: %v", err)
	}
}

func TestExecC1_StaleCompleteRejected(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "c1"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	stale := run
	stale.Fence = run.Fence - 1
	if err := repo.CompleteExecute(stale); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("err=%v", err)
	}
}

func TestExecC2_UnownedCompleteRejected(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "c2"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	stale := run
	stale.Owner = "other"
	if err := repo.CompleteExecute(stale); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("err=%v", err)
	}
}

func TestExecC3_FutureFenceCompleteRejected(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "c3"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	stale := run
	stale.Fence = run.Fence + 5
	if err := repo.FailExecute(stale, "x"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("err=%v", err)
	}
}

func TestExecC4_StaleRunIDRejected(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "c4"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	stale := run
	stale.RunID = "run_old"
	if err := repo.CancelExecute(stale, "x"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("err=%v", err)
	}
}

func TestExecD1_ExactlyOneTerminal(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "d1"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	if err := repo.CompleteExecute(run); err != nil {
		t.Fatal(err)
	}
	if err := repo.FailExecute(run, "late"); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("second terminal: %v", err)
	}
	detail, _ := repo.Get(id)
	terminals := 0
	for _, ev := range detail.Events {
		switch ev.EventType {
		case EventRunCompleted, EventRunFailed, EventRunCancelled, EventRunInterrupted:
			terminals++
		}
	}
	if terminals != 1 {
		t.Fatalf("terminals=%d", terminals)
	}
}

func TestExecD2_ProgressDoesNotBlockTerminal(t *testing.T) {
	orig := maxOrdinaryEventsPerTask
	maxOrdinaryEventsPerTask = 3
	defer func() { maxOrdinaryEventsPerTask = orig }()
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "d2"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	for i := 0; i < 5; i++ {
		_ = repo.RecordProgress(run, "phase")
	}
	if err := repo.CompleteExecute(run); err != nil {
		t.Fatalf("terminal blocked by progress: %v", err)
	}
}

func TestExecE1_CancelTaskWhileActiveExecute(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "e1"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	err := repo.CancelTask(id, Identity{Principal: "op", ExpectedOwner: run.Owner, ExpectedFencingToken: run.Fence})
	if err != nil {
		t.Fatal(err)
	}
	detail, _ := repo.Get(id)
	if detail.Summary.RunState != "cancelled" {
		t.Fatalf("%+v", detail.Summary)
	}
}

func TestExecE2_StaleCancelRejected(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "e2"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	err := repo.CancelTask(id, Identity{Principal: "op", ExpectedOwner: run.Owner, ExpectedFencingToken: run.Fence + 1})
	if CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("err=%v", err)
	}
}

func TestExecF1_PrivacyNoInputInEvents(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "f1", Goal: "ship"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w", Model: "openai-apikey/gpt"})
	_ = repo.CompleteExecute(run)
	detail, _ := repo.Get(id)
	blob := strings.ToLower(mustJSON(detail.Events))
	for _, bad := range []string{"prompt", "you are ", "sk-", "transcript", "full output"} {
		if strings.Contains(blob, bad) {
			t.Fatalf("leaked %q in %s", bad, blob)
		}
	}
}

func TestExecF2_ModelMetadataOnly(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "f2"})
	run, _ := repo.BeginExecute(id, ExecuteInput{Owner: "w", Model: "openai-apikey/gpt-5"})
	detail, _ := repo.Get(id)
	found := false
	for _, ev := range detail.Events {
		if ev.EventType == EventRunStarted {
			pl := payloadMap(ev.Payload)
			if pl["model"] == "openai-apikey/gpt-5" && pl["kind"] == "execute" {
				found = true
			}
			if _, ok := pl["input"]; ok {
				t.Fatal("input persisted")
			}
		}
	}
	if !found {
		t.Fatal("model metadata missing")
	}
	_ = run
}

func TestExecG1_RecoverOrphans(t *testing.T) {
	dir := t.TempDir()
	repo := New(dir)
	id, _ := repo.Create(CreateTaskInput{Title: "g1"})
	_, _ = repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	repo2 := New(dir)
	n, err := repo2.RecoverOrphans()
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	detail, _ := repo2.Get(id)
	if detail.Summary.RunState != "interrupted" {
		t.Fatalf("%+v", detail.Summary)
	}
}

func TestExecG2_NoAutoReplayAfterRecover(t *testing.T) {
	dir := t.TempDir()
	repo := New(dir)
	id, _ := repo.Create(CreateTaskInput{Title: "g2"})
	_, _ = repo.BeginExecute(id, ExecuteInput{Owner: "w"})
	repo2 := New(dir)
	_, _ = repo2.RecoverOrphans()
	n, err := repo2.RecoverOrphans()
	if err != nil || n != 0 {
		t.Fatalf("second recover n=%d err=%v", n, err)
	}
}

func TestExecH1_LifecycleStartStillAuditOnly(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "h1"})
	if err := repo.MarkStarted(id, "operator"); err != nil {
		t.Fatal(err)
	}
	detail, _ := repo.Get(id)
	if detail.Projection.CurrentOwner != "" {
		t.Fatalf("mark started set owner=%q", detail.Projection.CurrentOwner)
	}
	if _, err := os.Stat(filepath.Join(repo.Dir(), "leases", id+".lease.json")); !os.IsNotExist(err) {
		t.Fatalf("mark started wrote lease: %v", err)
	}
}

func TestExecH2_ExecuteDistinctFromMarkStarted(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "h2"})
	if err := repo.MarkStarted(id, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginExecute(id, ExecuteInput{Owner: "w"}); CodeOf(err) != CodeInvalidTransition {
		t.Fatalf("execute over mark-started: %v", err)
	}
}

func TestExecH3_SecretOwnerRejected(t *testing.T) {
	repo := New(t.TempDir())
	id, _ := repo.Create(CreateTaskInput{Title: "h3"})
	if _, err := repo.BeginExecute(id, ExecuteInput{Owner: "sk-abcdefghijklmnopqrstuvwxyz123456"}); CodeOf(err) != CodeInvalidTask {
		t.Fatalf("err=%v", err)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
