package continuation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestLoadOrCreateInstallationSaltIsStableAndOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "replay", "install.salt")
	first, err := LoadOrCreateInstallationSalt(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateInstallationSalt(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 32 || !bytes.Equal(first, second) {
		t.Fatalf("salt not stable/full width: %d equal=%v", len(first), bytes.Equal(first, second))
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("salt permissions too broad: %o", info.Mode().Perm())
		}
	}
}

func TestConcurrentSaltCreationConvergesOnOneValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.salt")
	const workers = 12
	values := make(chan []byte, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := LoadOrCreateInstallationSalt(path)
			if err != nil {
				errs <- err
				return
			}
			values <- value
		}()
	}
	wg.Wait()
	close(values)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var want []byte
	for value := range values {
		if want == nil {
			want = value
			continue
		}
		if !bytes.Equal(want, value) {
			t.Fatal("concurrent salt creators observed different durable identities")
		}
	}
}

func TestLoadSnapshotDropsLegacyFileExplicitly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "continuation.json")
	if err := os.WriteFile(path, []byte(`{"version":3,"entries":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewStore(StoreLimits{}, time.Now)
	err := LoadSnapshot(path, store, 1<<20)
	if !errors.Is(err, ErrSnapshotVersion) {
		t.Fatalf("legacy load = %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("legacy snapshot was not invalidated: %v", statErr)
	}
	if store.Len() != 0 {
		t.Fatal("legacy snapshot populated state")
	}
}

func TestPersisterFlushIsBoundedAndTracksSettledGeneration(t *testing.T) {
	store := NewStore(StoreLimits{}, time.Now)
	owner := mustOwner(t, "p", "https://a.example", "responses", "m", "oauth:a")
	putSmall(t, store, owner, "a")

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	p := newPersister(store, filepath.Join(t.TempDir(), "continuation.json"), func(_ string, _ []byte) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	})
	defer p.Close(context.Background())

	if err := p.Queue(); err != nil {
		t.Fatal(err)
	}
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := p.Flush(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stalled flush ignored deadline: %v", err)
	}
	close(release)
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := p.Flush(ctx2); err != nil {
		t.Fatalf("settled flush = %v", err)
	}
}

func TestPersisterCoalescesQueuedSnapshotsWithoutLosingLatestState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "continuation.json")
	store := NewStore(StoreLimits{}, time.Now)
	owner := mustOwner(t, "p", "https://a.example", "responses", "m", "oauth:a")
	putSmall(t, store, owner, "a")
	p := NewPersister(store, path)
	if err := p.Queue(); err != nil {
		t.Fatal(err)
	}
	putSmall(t, store, owner, "b")
	if err := p.Queue(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}

	restored := NewStore(StoreLimits{}, time.Now)
	if err := LoadSnapshot(path, restored, 64<<20); err != nil {
		t.Fatal(err)
	}
	if restored.Len() != 2 {
		t.Fatalf("latest queued state was not durable: len=%d", restored.Len())
	}
}
