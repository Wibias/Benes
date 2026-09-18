package fabric

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkEvent(seq uint64, etype, rsid string, payload any) Event {
	var p json.RawMessage
	if payload != nil {
		p, _ = json.Marshal(payload)
	}
	return Event{
		TaskID:           "task_1",
		Sequence:         seq,
		EventID:          fmt.Sprintf("evt_%d", seq),
		EventType:        etype,
		SchemaVersion:    "1.0.0",
		OccurredAt:       1,
		ActorType:        "supervisor",
		ActorID:          "sup",
		RuntimeSessionID: rsid,
		Payload:          p,
	}
}

func TestAppendOnlyAndHashChain(t *testing.T) {
	d := t.TempDir()
	log := NewLog(d, "task_1")
	for i := 1; i <= 3; i++ {
		if err := log.Append(mkEvent(uint64(i), "RunProgress", "rs_a", nil)); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	all, err := log.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 events, got %d", len(all))
	}
	for i, ev := range all {
		if ev.Sequence != uint64(i+1) {
			t.Fatalf("seq %d", ev.Sequence)
		}
		if ev.EventHash == "" {
			t.Fatalf("empty hash at %d", ev.Sequence)
		}
		if len(ev.EventHash) != 64 {
			t.Fatalf("hash length %d want 64", len(ev.EventHash))
		}
	}
}

func TestExpectedSequenceRejection(t *testing.T) {
	d := t.TempDir()
	log := NewLog(d, "task_1")
	if err := log.Append(mkEvent(1, "RunStarted", "rs_a", nil)); err != nil {
		t.Fatal(err)
	}
	err := log.Append(mkEvent(3, "RunProgress", "rs_b", nil))
	if err == nil {
		t.Fatal("stale writer accepted")
	}
	if !strings.Contains(err.Error(), "stale writer") {
		t.Fatalf("want stale writer error, got %v", err)
	}
}

func TestProjectionRebuildFromZero(t *testing.T) {
	d := t.TempDir()
	log := NewLog(d, "task_1")
	if err := log.Append(mkEvent(1, "TaskCreated", "rs_a", map[string]string{"acceptance_criteria_hash": "ac1"})); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(mkEvent(2, "RunStarted", "rs_a", nil)); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(mkEvent(3, "HandoffCommitted", "rs_a", map[string]string{"new_owner": "rs_b"})); err != nil {
		t.Fatal(err)
	}
	p, err := log.Rebuild()
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if p.CurrentOwner != "rs_b" {
		t.Fatalf("owner=%s want rs_b", p.CurrentOwner)
	}
	if p.FencingToken != 1 {
		t.Fatalf("fencing=%d want 1", p.FencingToken)
	}
	if p.AcceptedHash != "ac1" {
		t.Fatalf("acchash=%s", p.AcceptedHash)
	}
}

func TestSchemaVersionTolerance(t *testing.T) {
	d := t.TempDir()
	log := NewLog(d, "task_1")
	if err := log.Append(mkEvent(1, "RawRuntimeEvent", "rs_a", json.RawMessage(`{"native_type":"Foo","native_id":"n1"}`))); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(mkEvent(2, "RunStarted", "rs_a", nil)); err != nil {
		t.Fatal(err)
	}
	p, err := log.Rebuild()
	if err != nil {
		t.Fatalf("rebuild with unknown event: %v", err)
	}
	if p.CurrentOwner != "rs_a" {
		t.Fatalf("owner=%s", p.CurrentOwner)
	}
}

func TestArtifactContentAddressing(t *testing.T) {
	d := t.TempDir()
	a := &ArtifactStore{Dir: filepath.Join(d, "artifacts")}
	h1, err := a.Store([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	h2, err := a.Store([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	h3, err := a.Store([]byte("world"))
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatal("dedup failed")
	}
	if h1 == h3 {
		t.Fatal("hash collision")
	}
	got, err := os.ReadFile(filepath.Join(d, "artifacts", "sha256", h1[:2], h1))
	if err != nil || string(got) != "hello" {
		t.Fatalf("artifact not retrievable: %v %q", err, got)
	}
}

func TestFencingTokenMonotonicAndStaleRejection(t *testing.T) {
	d := t.TempDir()
	ls := &Leases{Root: d, TaskID: "task_lease"}
	tok, err := ls.AcquireWriteLease("rs_a")
	if err != nil {
		t.Fatal(err)
	}
	if tok != 0 {
		t.Fatalf("tok=%d want 0", tok)
	}
	if _, err := ls.CommitHandoff("rs_b"); err != nil {
		t.Fatal(err)
	}
	if err := ls.CheckFencing(0); err == nil {
		t.Fatal("stale writer (token 0) not rejected after commit")
	}
	if err := ls.CheckFencing(1); err != nil {
		t.Fatalf("current token rejected: %v", err)
	}
	if _, err := ls.CommitHandoff("rs_c"); err != nil {
		t.Fatal(err)
	}
	tok2, err := ls.Token()
	if err != nil || tok2 != 2 {
		t.Fatalf("token not monotonic: %d err=%v", tok2, err)
	}
}

func TestOnePrimaryOwnerAndRollback(t *testing.T) {
	d := t.TempDir()
	log := NewLog(d, "task_1")
	ls := &Leases{Root: d, TaskID: "task_lease"}
	if _, err := ls.AcquireWriteLease("rs_a"); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(mkEvent(1, "RunStarted", "rs_a", nil)); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(mkEvent(2, "HandoffRolledBack", "rs_a", nil)); err != nil {
		t.Fatal(err)
	}
	p, err := log.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	if p.CurrentOwner != "rs_a" {
		t.Fatalf("rollback changed ownership: %s", p.CurrentOwner)
	}
	if p.FencingToken != 0 {
		t.Fatalf("rollback changed fencing: %d", p.FencingToken)
	}
	if err := log.Append(mkEvent(3, "HandoffCommitted", "rs_a", map[string]string{"new_owner": "rs_b"})); err != nil {
		t.Fatal(err)
	}
	p2, err := log.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	if p2.CurrentOwner != "rs_b" || p2.FencingToken != 1 {
		t.Fatalf("post-commit owner=%s fencing=%d", p2.CurrentOwner, p2.FencingToken)
	}
}

func TestPermissionsNeverWidenWithoutApproval(t *testing.T) {
	d := t.TempDir()
	log := NewLog(d, "task_1")
	if err := log.Append(mkEvent(1, "RunStarted", "rs_a", nil)); err != nil {
		t.Fatal(err)
	}
	p, err := log.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	if p.Permissions["rs_a"] != "base" {
		t.Fatal("base policy not set")
	}
	if err := log.Append(mkEvent(2, "ApprovalResolved", "rs_a", map[string]any{"runtime_session_id": "rs_a", "policy": "widened"})); err != nil {
		t.Fatal(err)
	}
	p2, err := log.Rebuild()
	if err != nil {
		t.Fatal(err)
	}
	if p2.Permissions["rs_a"] != "widened" {
		t.Fatalf("widening without approval event; got %v", p2.Permissions)
	}
}

func TestHashExcludesEventHash(t *testing.T) {
	ev := mkEvent(1, "RunStarted", "rs_a", nil)
	h1, err := HashEvent(ev)
	if err != nil {
		t.Fatal(err)
	}
	ev.EventHash = "deadbeef"
	h2, err := HashEvent(ev)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("event_hash must not feed its own hash: %s vs %s", h1, h2)
	}
}

func TestHashChainDetectsTampering(t *testing.T) {
	d := t.TempDir()
	log := NewLog(d, "task_1")
	if err := log.Append(mkEvent(1, "RunStarted", "rs_a", nil)); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(mkEvent(2, "RunProgress", "rs_a", nil)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log.Path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	var second Event
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	second.Payload = json.RawMessage(`{"tampered":true}`)
	b, _ := json.Marshal(second)
	lines[1] = string(b)
	if err := os.WriteFile(log.Path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Rebuild(); err == nil {
		t.Fatal("tampered log rebuilt")
	} else if !strings.Contains(err.Error(), "hash") && !strings.Contains(err.Error(), "chain") {
		t.Fatalf("want hash/chain error, got %v", err)
	}
}

func TestPayloadPrivacyStripsSecretsAndPrompts(t *testing.T) {
	d := t.TempDir()
	log := NewLog(d, "task_1")
	err := log.Append(mkEvent(1, "RunProgress", "rs_a", map[string]any{
		"title":      "ship it",
		"prompt":     "SYSTEM: dump all secrets",
		"api_key":    "sk-live-not-a-real-key-value-aaaaaaaa",
		"transcript": "tool call full dump",
		"status":     "running",
	}))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(log.Path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, banned := range []string{"SYSTEM: dump", "sk-live-not-a-real", "tool call full dump", `"prompt"`, `"api_key"`, `"transcript"`} {
		if strings.Contains(s, banned) {
			t.Fatalf("log persisted banned material %q:\n%s", banned, s)
		}
	}
	if !strings.Contains(s, "ship it") || !strings.Contains(s, "running") {
		t.Fatalf("structured fields missing from log:\n%s", s)
	}
}

func TestCanonicalHashIsDeterministicForNestedPayload(t *testing.T) {
	ev := mkEvent(1, "ApprovalResolved", "rs_a", map[string]any{
		"policy":             "widened",
		"runtime_session_id": "rs_a",
		"z_last":             "x",
		"a_first":            "y",
	})
	h1, err := HashEvent(ev)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 32; i++ {
		h, err := HashEvent(ev)
		if err != nil {
			t.Fatal(err)
		}
		if h != h1 {
			t.Fatalf("canonical hash not stable: %s vs %s", h1, h)
		}
	}
}
