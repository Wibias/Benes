package requesthistory

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Wibias/Benes/internal/usageledger"
)

func TestRebuildRepairsAheadCheckpoint(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	meta, err := CatchUp(home)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := l.LogicalSize()
	if err != nil {
		t.Fatal(err)
	}
	if meta.IndexedOffset != auth {
		t.Fatalf("indexed=%d auth=%d", meta.IndexedOffset, auth)
	}
	if err := writeMetaKV(home, map[string]string{
		"indexed_offset": strconv.FormatInt(auth+4096, 10),
		"source_size":    strconv.FormatInt(auth+4096, 10),
	}); err != nil {
		t.Fatal(err)
	}
	st := Status(home)
	if !st.RebuildRequired || st.IndexedOffset <= auth {
		t.Fatalf("expected ahead rebuild-required %#v auth=%d", st, auth)
	}
	rebuilt, err := Rebuild(home)
	if err != nil {
		t.Fatal(err)
	}
	if rebuilt.RebuildRequired || rebuilt.IndexedOffset != auth {
		t.Fatalf("rebuild meta=%#v auth=%d", rebuilt, auth)
	}
	if _, err := Lookup(home, "req_1"); err != nil {
		t.Fatal(err)
	}
	st = Status(home)
	if st.RebuildRequired || st.IndexedOffset != auth {
		t.Fatalf("status after rebuild %#v", st)
	}
}

func TestAheadCheckpointIsNotTreatedAsNewerGeneration(t *testing.T) {
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
	auth, err := l.LogicalSize()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeMetaKV(home, map[string]string{
		"indexed_offset": strconv.FormatInt(auth+8192, 10),
		"source_size":    strconv.FormatInt(auth+8192, 10),
		"indexed_rows":   "1",
	}); err != nil {
		t.Fatal(err)
	}
	offset, ok := installedCheckpoint(filepath.Join(home, dbName), auth)
	if ok || offset <= auth {
		t.Fatalf("ahead checkpoint treated as valid newer gen offset=%d ok=%v", offset, ok)
	}
	rebuilt, err := Rebuild(home)
	if err != nil || rebuilt.IndexedOffset != auth || rebuilt.RebuildRequired {
		t.Fatalf("meta=%#v err=%v", rebuilt, err)
	}
}

func TestRebuildFinalCatchUpIncorporatesBytesDuringTempBuild(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_e1","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
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
	errCh := make(chan error, 1)
	go func() {
		_, err := Rebuild(home)
		errCh <- err
	}()
	<-snapshotOpen
	if err := l.Append([]byte(`{"requestId":"req_e2","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if _, err := Lookup(home, "req_e2"); err != nil {
		t.Fatalf("final install did not catch up under lock: %v", err)
	}
	st := Status(home)
	auth, err := l.LogicalSize()
	if err != nil {
		t.Fatal(err)
	}
	if st.IndexedRows != 2 || st.IndexedOffset != auth || st.RebuildRequired {
		t.Fatalf("%#v auth=%d", st, auth)
	}
}
