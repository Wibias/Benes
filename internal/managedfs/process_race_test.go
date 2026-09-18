package managedfs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestTwoProcessesCannotCommitTheSameGeneration(t *testing.T) {
	if os.Getenv("MANAGEDFS_CHILD") == "1" {
		t.Skip("child helper")
	}
	home := t.TempDir()
	ready := filepath.Join(home, "child.ready")
	done := filepath.Join(home, "child.done")
	cmd := exec.Command(os.Args[0], "-test.run=TestManagedfsChildHoldsLock", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"MANAGEDFS_CHILD=1",
		"MANAGEDFS_HOME="+home,
		"MANAGEDFS_READY="+ready,
		"MANAGEDFS_DONE="+done,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not acquire lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
	coord, err := New(home)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := coord.Begin(ctx, "codex"); err == nil {
		t.Fatal("parent stole the child lock")
	}
	_ = os.WriteFile(done, []byte("1"), 0o600)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child=%v", err)
	}
}

func TestManagedfsChildHoldsLock(t *testing.T) {
	if os.Getenv("MANAGEDFS_CHILD") != "1" {
		t.Skip("parent")
	}
	coord, err := New(os.Getenv("MANAGEDFS_HOME"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := coord.Begin(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("MANAGEDFS_READY"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(os.Getenv("MANAGEDFS_DONE")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("parent never released the child")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
