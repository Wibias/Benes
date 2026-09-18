package requesthistory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/usageledger"
)

func holdCatchUp(t *testing.T) (started chan struct{}, release func()) {
	t.Helper()
	started = make(chan struct{})
	block := make(chan struct{})
	var once sync.Once
	release = func() {
		once.Do(func() { close(block) })
	}
	SetHoldCatchUpTestHook(func() {
		select {
		case <-started:
		default:
			close(started)
		}
		<-block
	})
	t.Cleanup(func() {
		SetHoldCatchUpTestHook(nil)
		release()
	})
	return started, release
}

func lookupPendingErr(err error) bool {
	return errors.Is(err, ErrUnavailable) || errors.Is(err, ErrRebuildRequired)
}

func TestCatchUpBestEffortReturnsWhileHeld(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	idx, err := OpenIndexer(home)
	if err != nil {
		t.Fatal(err)
	}
	started, release := holdCatchUp(t)
	defer idx.Close()
	defer release()
	if err := idx.CatchUpBestEffort(); err != nil {
		t.Fatal(err)
	}
	<-started
	release()
	if _, err := idx.CatchUp(); err != nil {
		t.Fatal(err)
	}
	if _, err := Lookup(home, "req_1"); err != nil {
		t.Fatal(err)
	}
}

func TestCatchUpBestEffortIndexesHistoricalLedgerWithoutBlocking(t *testing.T) {
	home := t.TempDir()
	var lines string
	for i := 1; i <= 8; i++ {
		lines += `{"requestId":"req_` + strconv.Itoa(i) + `","provider":"openai","model":"gpt-5","status":200}` + "\n"
	}
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	idx, err := OpenIndexer(home)
	if err != nil {
		t.Fatal(err)
	}
	started, release := holdCatchUp(t)
	defer idx.Close()
	defer release()
	if err := idx.CatchUpBestEffort(); err != nil {
		t.Fatal(err)
	}
	<-started
	release()
	meta, err := idx.CatchUp()
	if err != nil || meta.IndexedRows != 8 || !meta.CaughtUp {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	if _, err := Lookup(home, "req_8"); err != nil {
		t.Fatal(err)
	}
}

func TestIndexerBestEffortCoalescesAppendDuringCatchUp(t *testing.T) {
	home := t.TempDir()
	keep := []byte(`{"requestId":"req_keep","provider":"openai","model":"gpt-5","status":200}`)
	added := []byte(`{"requestId":"req_new","provider":"openai","model":"gpt-5","status":200}`)
	writer, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(keep); err != nil {
		t.Fatal(err)
	}
	idx, err := OpenIndexer(home)
	if err != nil {
		t.Fatal(err)
	}
	snapshotOpen := make(chan struct{})
	release := make(chan struct{})
	var unlockOnce sync.Once
	unlock := func() { unlockOnce.Do(func() { close(release) }) }
	defer idx.Close()
	defer unlock()
	usageledger.SetAfterOpenSourcesTestHook(func() {
		usageledger.SetAfterOpenSourcesTestHook(nil)
		close(snapshotOpen)
		<-release
	})
	defer usageledger.SetAfterOpenSourcesTestHook(nil)
	if err := idx.CatchUpBestEffort(); err != nil {
		t.Fatal(err)
	}
	<-snapshotOpen
	if err := writer.Append(added); err != nil {
		t.Fatal(err)
	}
	if err := idx.CatchUpBestEffort(); err != nil {
		t.Fatal(err)
	}
	unlock()
	if _, err := idx.CatchUp(); err != nil {
		t.Fatal(err)
	}
	if _, err := lookupOnly(home, "req_new"); err != nil {
		t.Fatalf("lost append after coalesced catch-up: %v", err)
	}
}

func TestLookupDuringIncompleteCatchUpDoesNotFullBuild(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	idx, err := OpenIndexer(home)
	if err != nil {
		t.Fatal(err)
	}
	started, release := holdCatchUp(t)
	defer idx.Close()
	defer release()
	if err := idx.CatchUpBestEffort(); err != nil {
		t.Fatal(err)
	}
	<-started
	_, err = idx.Lookup("req_missing")
	if !lookupPendingErr(err) {
		t.Fatalf("missing lookup waited or proved absence err=%v", err)
	}
	_, err = idx.Lookup("req_1")
	if !lookupPendingErr(err) {
		t.Fatalf("unproven id served or blocked err=%v", err)
	}
	release()
	if _, err := idx.CatchUp(); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.Lookup("req_1"); err != nil {
		t.Fatal(err)
	}
	_, err = idx.Lookup("req_missing")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("healthy miss after catch-up err=%v", err)
	}
}

func TestIndexerCloseDoesNotLeaveWorker(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	idx, err := OpenIndexer(home)
	if err != nil {
		t.Fatal(err)
	}
	started, release := holdCatchUp(t)
	defer release()
	defer idx.Close()
	if err := idx.CatchUpBestEffort(); err != nil {
		t.Fatal(err)
	}
	<-started
	done := make(chan struct{})
	go func() {
		idx.Close()
		close(done)
	}()
	release()
	<-done
	SetHoldCatchUpTestHook(func() {
		t.Error("catch-up ran after Close")
	})
	defer SetHoldCatchUpTestHook(nil)
	if err := idx.CatchUpBestEffort(); err != nil {
		t.Fatal(err)
	}
}

func TestIndexerCloseCancelsWriterLockWait(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	owner, err := acquireWriter(home)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()

	waiting := make(chan struct{})
	var waitOnce sync.Once
	SetAcquireWriterWaitingTestHook(func() {
		waitOnce.Do(func() { close(waiting) })
	})
	defer SetAcquireWriterWaitingTestHook(nil)

	catchUpEntered := make(chan struct{}, 1)
	SetHoldCatchUpTestHook(func() {
		select {
		case catchUpEntered <- struct{}{}:
		default:
		}
	})
	defer SetHoldCatchUpTestHook(nil)

	idx, err := OpenIndexer(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.CatchUpBestEffort(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not wait for writer lock")
	}

	done := make(chan struct{})
	go func() {
		_ = idx.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked while waiting for writer lock")
	}

	select {
	case <-catchUpEntered:
		t.Fatal("catch-up wrote after Close canceled lock wait")
	default:
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if lock, err := acquireWriterContext(ctx, home); err == nil {
		_ = lock.Close()
		t.Fatal("Close released the independent owner's writer lock")
	}

	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireWriter(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	st := Status(home)
	if st.IndexedRows != 0 || st.CaughtUp {
		t.Fatalf("canceled catch-up still wrote %#v", st)
	}
}

func TestAcquireWriterContextAlreadyCanceledFreeLock(t *testing.T) {
	home := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lock, err := acquireWriterContext(ctx, home)
	if lock != nil {
		_ = lock.Close()
		t.Fatal("canceled acquire returned a lock on a free path")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	owner, err := acquireWriter(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireWriterContextCancelWinsOverRelease(t *testing.T) {
	home := t.TempDir()
	owner, err := acquireWriter(home)
	if err != nil {
		t.Fatal(err)
	}

	waiting := make(chan struct{})
	var waitOnce sync.Once
	SetAcquireWriterWaitingTestHook(func() {
		waitOnce.Do(func() { close(waiting) })
	})
	defer SetAcquireWriterWaitingTestHook(nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		lock, err := acquireWriterContext(ctx, home)
		if lock != nil {
			_ = lock.Close()
			errCh <- errors.New("canceled waiter returned a lock")
			return
		}
		errCh <- err
	}()
	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		t.Fatal("waiter did not block on writer lock")
	}
	cancel()
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled waiter did not return")
	}
	lock, err := acquireWriter(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestIndexerCloseCancelsBeforeFreeLockAcquire(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}

	atAttempt := make(chan struct{})
	releaseAttempt := make(chan struct{})
	var attemptOnce sync.Once
	SetBeforeAcquireWriterAttemptTestHook(func() {
		attemptOnce.Do(func() { close(atAttempt) })
		<-releaseAttempt
	})
	defer SetBeforeAcquireWriterAttemptTestHook(nil)

	catchUpEntered := make(chan struct{}, 1)
	SetHoldCatchUpTestHook(func() {
		select {
		case catchUpEntered <- struct{}{}:
		default:
		}
	})
	defer SetHoldCatchUpTestHook(nil)

	idx, err := OpenIndexer(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.CatchUpBestEffort(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-atAttempt:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not reach lock acquire attempt")
	}

	stopped := make(chan struct{})
	SetAfterIndexerStopTestHook(func() {
		close(stopped)
	})
	defer SetAfterIndexerStopTestHook(nil)

	done := make(chan struct{})
	go func() {
		_ = idx.Close()
		close(done)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel stopCtx while acquire was paused")
	}
	select {
	case <-done:
		t.Fatal("Close returned while acquire attempt still paused")
	default:
	}
	close(releaseAttempt)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not finish after releasing acquire pause")
	}

	select {
	case <-catchUpEntered:
		t.Fatal("catch-up ran after Close canceled acquire")
	default:
	}
	st := Status(home)
	if st.IndexedRows != 0 || st.CaughtUp {
		t.Fatalf("post-cancel catch-up mutated index %#v", st)
	}
	dbPath := filepath.Join(home, dbName)
	if _, err := os.Stat(dbPath); err == nil {
		t.Fatal("canceled worker created derived SQLite after Close")
	}
}
