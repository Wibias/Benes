package platform

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestProbeSnapshotNeverBlocksOnRefresh(t *testing.T) {
	p := NewProbe()
	if got := p.Snapshot(); got.Complete || got.ServiceManager != ServiceManagerUnknown {
		t.Fatalf("initial=%#v", got)
	}
	started := 0
	for i := 0; i < 8; i++ {
		if p.Refresh(context.Background()) {
			started++
		}
	}
	if started != 1 {
		t.Fatalf("inflight probes=%d", started)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.Snapshot().Complete {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("snapshot did not publish")
}

func TestConcurrentSnapshotReadsDoNotLaunchHelpers(t *testing.T) {
	p := NewProbe()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Snapshot()
		}()
	}
	wg.Wait()
	if p.Snapshot().Complete {
		t.Fatal("snapshot reads must not start discovery")
	}
}
