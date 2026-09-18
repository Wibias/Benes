package codexlogguard

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestProtectQuietAndInspect(t *testing.T) {
	home := t.TempDir()
	dbPath := filepath.Join(home, "logs_2.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts INTEGER NOT NULL,
		ts_nanos INTEGER NOT NULL,
		level TEXT NOT NULL,
		target TEXT NOT NULL,
		estimated_bytes INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	mut := Protect(home, ModeQuiet)
	if !mut.OK {
		t.Fatalf("protect=%#v", mut)
	}
	st := Inspect(home, ModeQuiet)
	if st.Protection["state"] != "active" || st.Protection["observedMode"] != "quiet" {
		t.Fatalf("status=%#v", st.Protection)
	}
	off := Protect(home, ModeOff)
	if !off.OK {
		t.Fatalf("unprotect=%#v", off)
	}
}

func TestInspectMissingDatabase(t *testing.T) {
	st := Inspect(t.TempDir(), ModeOff)
	if st.Schema["state"] != "missing" {
		t.Fatalf("schema=%#v", st.Schema)
	}
}

func TestProtectMissingDatabaseFailsClosed(t *testing.T) {
	mut := Protect(t.TempDir(), ModeCompat)
	if mut.OK {
		t.Fatal("expected failure")
	}
	_ = os.ErrNotExist
}
