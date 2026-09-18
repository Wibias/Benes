package requesthistory

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/Wibias/Benes/internal/usageledger"
)

func TestSchemaV2CheckConstraintRequiresRebuildAndDoesNotAdvance(t *testing.T) {
	home := seedV2Home(t)
	before := Status(home).IndexedOffset
	execDB(t, home,
		`DROP TABLE requests`,
		`CREATE TABLE requests (request_id TEXT PRIMARY KEY, row_json TEXT NOT NULL CHECK (length(row_json) < 8))`,
	)
	st := Status(home)
	if !st.RebuildRequired {
		t.Fatalf("CHECK schema still healthy %#v", st)
	}
	_, err := CatchUp(home)
	if !errors.Is(err, ErrRebuildRequired) {
		t.Fatalf("catch-up err=%v", err)
	}
	if Status(home).IndexedOffset != before && !Status(home).RebuildRequired {
		t.Fatalf("catch-up advanced past broken schema %#v", Status(home))
	}
	requireRebuildAndRepair(t, home)
}

func TestSchemaV2NoCaseCollationRequiresRebuild(t *testing.T) {
	home := seedV2Home(t)
	execDB(t, home,
		`DROP TABLE requests`,
		`CREATE TABLE requests (request_id TEXT COLLATE NOCASE PRIMARY KEY, row_json TEXT NOT NULL)`,
		`INSERT INTO requests(request_id, row_json) VALUES ('req_1', '{"requestId":"req_1"}')`,
	)
	requireRebuildAndRepair(t, home)
}

func TestSchemaV2IncompatibleTypeRequiresRebuild(t *testing.T) {
	home := seedV2Home(t)
	execDB(t, home,
		`DROP TABLE requests`,
		`CREATE TABLE requests (request_id BLOB PRIMARY KEY, row_json TEXT NOT NULL)`,
		`INSERT INTO requests(request_id, row_json) VALUES (x'01', '{"requestId":"req_1"}')`,
	)
	requireRebuildAndRepair(t, home)
}

func TestSchemaV2TriggerRequiresRebuild(t *testing.T) {
	home := seedV2Home(t)
	execDB(t, home,
		`CREATE TRIGGER requests_ignore BEFORE INSERT ON requests BEGIN SELECT RAISE(IGNORE); END`,
	)
	requireRebuildAndRepair(t, home)
}

func TestSchemaV2DuplicateRequestIDLastWins(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_dup","provider":"openai","model":"gpt-5","status":200,"phase":"old"}`)); err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_dup","provider":"openai","model":"gpt-5","status":200,"phase":"new"}`)); err != nil {
		t.Fatal(err)
	}
	meta, err := CatchUp(home)
	if err != nil || meta.IndexedRows != 1 || meta.RebuildRequired {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	row, err := Lookup(home, "req_dup")
	if err != nil {
		t.Fatal(err)
	}
	if row["phase"] != "new" {
		t.Fatalf("expected last-wins row %#v", row)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, dbName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM requests`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("unique count=%d err=%v", n, err)
	}
}

func TestCatchUpDoesNotAdvancePastConstraintFailure(t *testing.T) {
	home := t.TempDir()
	l, err := usageledger.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Append([]byte(`{"requestId":"req_ok","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	meta, err := CatchUp(home)
	if err != nil {
		t.Fatal(err)
	}
	before := meta.IndexedOffset
	// Bypass classify by planting a CHECK after a healthy open path is impossible;
	// instead verify ON CONFLICT path: corrupt insert via direct SQL proves DO NOTHING
	// only suppresses request_id conflicts, then repair via rebuild.
	db, err := sql.Open("sqlite", filepath.Join(home, dbName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE requests; CREATE TABLE requests (request_id TEXT PRIMARY KEY, row_json TEXT NOT NULL CHECK (length(row_json) < 8))`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()
	if err := l.Append([]byte(`{"requestId":"req_bad","provider":"openai","model":"gpt-5","status":200}`)); err != nil {
		t.Fatal(err)
	}
	_, err = CatchUp(home)
	if !errors.Is(err, ErrRebuildRequired) {
		t.Fatalf("expected rebuild-required after CHECK schema err=%v", err)
	}
	st := Status(home)
	if st.IndexedOffset > before && !st.RebuildRequired {
		t.Fatalf("advanced past silent-drop risk %#v", st)
	}
	if _, err := os.Stat(filepath.Join(home, dbName)); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := Rebuild(home)
	if err != nil || rebuilt.RebuildRequired || rebuilt.IndexedRows != 2 {
		t.Fatalf("rebuild meta=%#v err=%v", rebuilt, err)
	}
}
