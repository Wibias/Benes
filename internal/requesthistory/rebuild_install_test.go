package requesthistory

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Wibias/Benes/internal/usageledger"

	_ "modernc.org/sqlite"
)

func TestRebuildInstallFailureKeepsPrevious(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_old","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	SetInstallTestHook(func(tmp, dest string) error {
		if _, err := os.Stat(dest); err != nil {
			t.Errorf("previous dest missing before failed install: %v", err)
		}
		if _, err := os.Stat(tmp); err != nil {
			t.Errorf("rebuild temp missing: %v", err)
		}
		return errors.New("install boom")
	})
	defer SetInstallTestHook(nil)
	if _, err := Rebuild(home); err == nil {
		t.Fatal("expected install failure")
	}
	if _, err := lookupOnly(home, "req_old"); err != nil {
		t.Fatalf("failed install destroyed previous index: %v", err)
	}
}

func TestRebuildTempPathsAreUnique(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), dbName)
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		p := uniqueRebuildPath(dbPath)
		if seen[p] {
			t.Fatalf("duplicate rebuild temp %s", p)
		}
		seen[p] = true
	}
}

func TestConcurrentRebuildsUseDistinctTemps(t *testing.T) {
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
		return errors.New("stop before install")
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
	if len(paths) != 2 {
		t.Fatalf("paths=%v", paths)
	}
	if paths[0] == paths[1] {
		t.Fatalf("rebuild temps collided %s", paths[0])
	}
	if _, err := lookupOnly(home, "req_1"); err != nil {
		t.Fatalf("concurrent rebuilds destroyed previous index: %v", err)
	}
}

func TestRebuildRepairsEqualOffsetSemanticRowCorruption(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"req_1", "req_2"} {
		if err := l.Append([]byte(`{"requestId":"` + id + `","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
			t.Fatal(err)
		}
	}
	meta, err := CatchUp(home)
	if err != nil || meta.IndexedRows != 2 || meta.RebuildRequired {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	auth, err := l.LogicalSize()
	if err != nil {
		t.Fatal(err)
	}
	if meta.IndexedOffset != auth {
		t.Fatalf("indexed=%d auth=%d", meta.IndexedOffset, auth)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, dbName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE requests SET row_json = ? WHERE request_id = ?`, `{"requestId":"req_1","provider":"openai","model":"CORRUPT","status":200}`, "req_1"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	row, err := Lookup(home, "req_1")
	if err != nil {
		t.Fatal(err)
	}
	if row["model"] != "CORRUPT" {
		t.Fatalf("expected corrupted lookup %#v", row)
	}
	rebuilt, err := Rebuild(home)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.RebuildRequired || rebuilt.IndexedOffset != auth || rebuilt.IndexedRows != 2 {
		t.Fatalf("rebuild meta=%#v auth=%d", rebuilt, auth)
	}
	row, err = Lookup(home, "req_1")
	if err != nil {
		t.Fatal(err)
	}
	if row["model"] != "gpt-5" {
		t.Fatalf("rebuild did not restore ledger row %#v", row)
	}
	st := Status(home)
	if st.RebuildRequired || st.IndexedOffset != auth || st.IndexedRows != 2 {
		t.Fatalf("status %#v auth=%d", st, auth)
	}
}
