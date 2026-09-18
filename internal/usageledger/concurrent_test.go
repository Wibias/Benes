package usageledger

import (
	"strconv"
	"sync"
	"testing"
)

func TestConcurrentAppendsCommitEachRowOnce(t *testing.T) {
	home := t.TempDir()
	l := mustOpen(t, home, 256)
	const n = 50
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	wg.Add(n)
	for i := 1; i <= n; i++ {
		i := i
		go func() {
			defer wg.Done()
			errCh <- l.Append([]byte(`{"timestamp":` + strconv.Itoa(i) + `,"requestId":"req_` + strconv.Itoa(i) + `"}`))
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	got := committedIDs(t, home)
	if len(got) != n {
		t.Fatalf("got %d want %d ids=%v", len(got), n, got)
	}
	seen := map[string]int{}
	for _, id := range got {
		seen[id]++
		if seen[id] > 1 {
			t.Fatalf("duplicate %s", id)
		}
	}
	for i := 1; i <= n; i++ {
		id := "req_" + strconv.Itoa(i)
		if seen[id] != 1 {
			t.Fatalf("missing %s", id)
		}
	}
}
