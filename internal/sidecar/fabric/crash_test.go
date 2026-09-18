package fabric

import (
	"testing"
)

func TestCrashBoundaries(t *testing.T) {
	d := t.TempDir()
	taskID := "task_crash"
	log := NewLog(d, taskID)
	ls := &Leases{Root: d, TaskID: taskID}
	if _, err := ls.AcquireWriteLease("rs_source"); err != nil {
		t.Fatal(err)
	}
	accHash := "sha256:acceptance_demo"
	mustAppend(t, log, crashEvent(1, "TaskCreated", "rs_source", map[string]string{"acceptance_criteria_hash": accHash}))
	mustAppend(t, log, crashEvent(2, "RunStarted", "rs_source", nil))

	p, err := log.Rebuild()
	if err != nil || p.CurrentOwner != "rs_source" {
		t.Fatalf("boundary 2 rebuild: err=%v owner=%s", err, p.CurrentOwner)
	}

	p3, err := log.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	if p3.CurrentOwner != "rs_source" || p3.FencingToken != 0 {
		t.Fatalf("boundary 3/4: handoff must not commit; owner=%s fencing=%d", p3.CurrentOwner, p3.FencingToken)
	}

	mustAppend(t, log, crashEvent(3, "HandoffCommitted", "rs_source", map[string]string{"new_owner": "rs_target"}))
	if _, err := ls.CommitHandoff("rs_target"); err != nil {
		t.Fatal(err)
	}
	if err := ls.CheckFencing(0); err == nil {
		t.Fatal("boundary 5: stale source not fenced")
	}

	pf, err := log.Rebuild()
	if err != nil || pf.CurrentOwner != "rs_target" || pf.FencingToken != 1 || pf.AcceptedHash != accHash {
		t.Fatalf("final verify owner=%s token=%d hash=%s err=%v", pf.CurrentOwner, pf.FencingToken, pf.AcceptedHash, err)
	}
}

func crashEvent(seq uint64, etype, rsid string, payload any) Event {
	ev := mkEvent(seq, etype, rsid, payload)
	ev.TaskID = "task_crash"
	return ev
}

func mustAppend(t *testing.T, log *Log, ev Event) {
	t.Helper()
	if err := log.Append(ev); err != nil {
		t.Fatalf("append %s: %v", ev.EventType, err)
	}
}
