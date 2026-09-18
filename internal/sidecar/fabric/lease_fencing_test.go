package fabric

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLease01_AcquireGrantsToken(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "task_a"}
	tok, err := ls.AcquireWriteLease("owner_a")
	if err != nil || tok != 0 {
		t.Fatalf("tok=%d err=%v", tok, err)
	}
	owner, err := ls.Owner()
	if err != nil || owner != "owner_a" {
		t.Fatalf("owner=%q err=%v", owner, err)
	}
}

func TestLease02_SecondOwnerRejected(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "task_a"}
	_, _ = ls.AcquireWriteLease("owner_a")
	if _, err := ls.AcquireWriteLease("owner_b"); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("err=%v", err)
	}
}

func TestLease03_SameOwnerReacquire(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "task_a"}
	_, _ = ls.AcquireWriteLease("owner_a")
	tok, err := ls.AcquireWriteLease("owner_a")
	if err != nil || tok != 0 {
		t.Fatalf("tok=%d err=%v", tok, err)
	}
}

func TestLease04_HandoffIncrements(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "task_a"}
	_, _ = ls.AcquireWriteLease("owner_a")
	tok, err := ls.CommitHandoff("owner_b")
	if err != nil || tok != 1 {
		t.Fatalf("tok=%d err=%v", tok, err)
	}
}

func TestLease05_StaleFenceRejected(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "task_a"}
	_, _ = ls.AcquireWriteLease("owner_a")
	_, _ = ls.CommitHandoff("owner_b")
	if err := ls.CheckFencing(0); !errors.Is(err, ErrLeaseStale) {
		t.Fatalf("err=%v", err)
	}
}

func TestLease06_FutureFenceRejected(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "task_a"}
	_, _ = ls.AcquireWriteLease("owner_a")
	if err := ls.CheckFencing(9); !errors.Is(err, ErrLeaseFuture) {
		t.Fatalf("err=%v", err)
	}
}

func TestLease07_ExactOwnerAndFence(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "task_a"}
	_, _ = ls.AcquireWriteLease("owner_a")
	tok, _ := ls.CommitHandoff("owner_b")
	if err := ls.CheckOwnerAndFence("owner_b", tok); err != nil {
		t.Fatal(err)
	}
}

func TestLease08_WrongOwnerRejected(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "task_a"}
	_, _ = ls.AcquireWriteLease("owner_a")
	tok, _ := ls.CommitHandoff("owner_b")
	if err := ls.CheckOwnerAndFence("owner_a", tok); !errors.Is(err, ErrLeaseUnowned) {
		t.Fatalf("err=%v", err)
	}
}

func TestLease09_StaleOwnerFenceRejected(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "task_a"}
	_, _ = ls.AcquireWriteLease("owner_a")
	_, _ = ls.CommitHandoff("owner_b")
	if err := ls.CheckOwnerAndFence("owner_b", 0); !errors.Is(err, ErrLeaseStale) {
		t.Fatalf("err=%v", err)
	}
}

func TestLease10_FutureOwnerFenceRejected(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "task_a"}
	_, _ = ls.AcquireWriteLease("owner_a")
	tok, _ := ls.CommitHandoff("owner_b")
	if err := ls.CheckOwnerAndFence("owner_b", tok+1); !errors.Is(err, ErrLeaseFuture) {
		t.Fatalf("err=%v", err)
	}
}

func TestLease11_MalformedJSONFailClosed(t *testing.T) {
	root := t.TempDir()
	ls := &Leases{Root: root, TaskID: "task_a"}
	path := filepath.Join(root, "leases", "task_a.lease.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ls.Owner(); !errors.Is(err, ErrLeaseMalformed) {
		t.Fatalf("err=%v", err)
	}
}

func TestLease12_EmptyFileFailClosed(t *testing.T) {
	root := t.TempDir()
	ls := &Leases{Root: root, TaskID: "task_a"}
	path := filepath.Join(root, "leases", "task_a.lease.json")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte{}, 0o600)
	if _, err := ls.Token(); !errors.Is(err, ErrLeaseMalformed) {
		t.Fatalf("err=%v", err)
	}
}

func TestLease13_TruncatedObjectFailClosed(t *testing.T) {
	root := t.TempDir()
	ls := &Leases{Root: root, TaskID: "task_a"}
	path := filepath.Join(root, "leases", "task_a.lease.json")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte(`{"version":1,"fencing_token":`), 0o600)
	if _, err := ls.AcquireWriteLease("owner_a"); !errors.Is(err, ErrLeaseMalformed) {
		t.Fatalf("err=%v", err)
	}
}

func TestLease14_UnsupportedVersionFailClosed(t *testing.T) {
	root := t.TempDir()
	ls := &Leases{Root: root, TaskID: "task_a"}
	path := filepath.Join(root, "leases", "task_a.lease.json")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte(`{"version":99,"fencing_token":0,"owner":"a"}`), 0o600)
	if _, err := ls.Owner(); !errors.Is(err, ErrLeaseMalformed) {
		t.Fatalf("err=%v", err)
	}
}

func TestLease15_SecretFieldRejected(t *testing.T) {
	root := t.TempDir()
	ls := &Leases{Root: root, TaskID: "task_a"}
	path := filepath.Join(root, "leases", "task_a.lease.json")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte(`{"version":1,"fencing_token":0,"owner":"a","api_key":"sk-secret"}`), 0o600)
	if _, err := ls.Owner(); !errors.Is(err, ErrLeaseMalformed) {
		t.Fatalf("err=%v", err)
	}
}

func TestLease16_AtomicReplaceNoWindow(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "task_a"}
	_, _ = ls.AcquireWriteLease("owner_a")
	tok, err := ls.CommitHandoff("owner_b")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ls.Owner()
	if err != nil || owner != "owner_b" || tok != 1 {
		t.Fatalf("owner=%q tok=%d err=%v", owner, tok, err)
	}
	path := filepath.Join(ls.Root, "leases", "task_a.lease.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if int(s["version"].(float64)) != LeaseSchemaVersion {
		t.Fatalf("version=%v", s["version"])
	}
}

func TestLease17_NoSecretsPersisted(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "task_a"}
	_, err := ls.AcquireWriteLease("owner_a")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(ls.Root, "leases", "task_a.lease.json"))
	body := string(raw)
	for _, bad := range []string{"api_key", "prompt", "authorization", "secret", "sk-"} {
		if strings.Contains(strings.ToLower(body), bad) {
			t.Fatalf("lease leaked %q: %s", bad, body)
		}
	}
}

func TestLease18_PathEscapeRejected(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "../escape"}
	if _, err := ls.AcquireWriteLease("owner_a"); err == nil {
		t.Fatal("expected escape rejection")
	}
}

func TestLease19_SymlinkLeaseFileRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("symlink privileges vary on Windows")
	}
	root := t.TempDir()
	real := filepath.Join(root, "real.json")
	_ = os.WriteFile(real, []byte(`{"version":1,"fencing_token":0,"owner":""}`), 0o600)
	leases := filepath.Join(root, "leases")
	_ = os.MkdirAll(leases, 0o700)
	link := filepath.Join(leases, "task_a.lease.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	ls := &Leases{Root: root, TaskID: "task_a"}
	if _, err := ls.Owner(); !errors.Is(err, ErrLeaseEscape) {
		t.Fatalf("err=%v", err)
	}
}

func TestLease20_ReleaseBumpsFence(t *testing.T) {
	ls := &Leases{Root: t.TempDir(), TaskID: "task_a"}
	_, _ = ls.AcquireWriteLease("owner_a")
	tok, err := ls.Release()
	if err != nil || tok != 1 {
		t.Fatalf("tok=%d err=%v", tok, err)
	}
	owner, _ := ls.Owner()
	if owner != "" {
		t.Fatalf("owner=%q", owner)
	}
	if err := ls.CheckOwnerAndFence("owner_a", 0); err == nil {
		t.Fatal("stale claim after release")
	}
}
