package requesthistory

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wibias/Benes/internal/usageledger"
)

func TestCatchUpVsRebuildKeepsLaterRow(t *testing.T) {
	home := t.TempDir()
	keep := []byte(`{"requestId":"req_e1","provider":"openai","model":"gpt-5","status":200}`)
	added := []byte(`{"requestId":"req_e2","provider":"openai","model":"gpt-5","status":200}`)
	writer, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(keep); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	snapshotOpen := make(chan struct{})
	release := make(chan struct{})
	usageledger.SetAfterOpenSourcesTestHook(func() {
		usageledger.SetAfterOpenSourcesTestHook(nil)
		close(snapshotOpen)
		<-release
	})
	defer usageledger.SetAfterOpenSourcesTestHook(nil)
	rebuildErr := make(chan error, 1)
	go func() {
		_, err := Rebuild(home)
		rebuildErr <- err
	}()
	<-snapshotOpen
	if err := writer.Append(added); err != nil {
		t.Fatal(err)
	}
	idx, err := OpenIndexer(home)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if _, err := idx.CatchUp(); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-rebuildErr; err != nil {
		t.Fatal(err)
	}
	if _, err := Lookup(home, "req_e2"); err != nil {
		t.Fatalf("lost E2 after rebuild install err=%v", err)
	}
	st := Status(home)
	if st.IndexedRows != 2 || st.RebuildRequired {
		t.Fatalf("%#v", st)
	}
}

func TestOlderRebuildDoesNotRegressNewerGeneration(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_e1","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	var installs atomic.Int32
	aAtInstall := make(chan struct{})
	aRelease := make(chan struct{})
	SetInstallTestHook(func(tmp, dest string) error {
		if installs.Add(1) == 1 {
			close(aAtInstall)
			<-aRelease
		}
		return nil
	})
	defer SetInstallTestHook(nil)
	aErr := make(chan error, 1)
	go func() {
		_, err := Rebuild(home)
		aErr <- err
	}()
	<-aAtInstall
	if err := l.Append([]byte(`{"requestId":"req_e2","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(home); err != nil {
		t.Fatal(err)
	}
	close(aRelease)
	if err := <-aErr; err != nil {
		t.Fatal(err)
	}
	if _, err := Lookup(home, "req_e2"); err != nil {
		t.Fatalf("older rebuild overwrote newer generation: %v", err)
	}
	if st := Status(home); st.IndexedRows != 2 {
		t.Fatalf("%#v", st)
	}
}

func TestRebuildDoesNotRemoveLiveWriterSidecar(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(home, dbName+"-journal")
	if err := os.WriteFile(journal, []byte("owned-by-writer"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireWriter(home)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	started, release := holdCatchUp(t)
	rebuildErr := make(chan error, 1)
	go func() {
		_, err := Rebuild(home)
		rebuildErr <- err
	}()
	<-started
	if _, err := os.Stat(journal); err != nil {
		t.Fatalf("live writer sidecar removed while rebuild held: %v", err)
	}
	release()
	if _, err := os.Stat(journal); err != nil {
		t.Fatalf("sidecar removed while writer lock still held: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-rebuildErr; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentRebuildsStillUseDistinctTemps(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var paths []string
	SetInstallTestHook(func(tmp, dest string) error {
		mu.Lock()
		paths = append(paths, tmp)
		mu.Unlock()
		return os.ErrInvalid
	})
	defer SetInstallTestHook(nil)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = Rebuild(home)
		}()
	}
	wg.Wait()
	if len(paths) != 2 || paths[0] == paths[1] {
		t.Fatalf("paths=%v", paths)
	}
}
