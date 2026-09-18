package requesthistory

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSchemaV2HealthyServesLookup(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := CatchUp(home)
	if err != nil || meta.SchemaVersion != 3 || meta.RebuildRequired || !meta.CaughtUp {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	st := Status(home)
	if st.RebuildRequired || st.SchemaVersion != 3 {
		t.Fatalf("%#v", st)
	}
	if _, err := Lookup(home, "req_1"); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaV2IncompatibleRequestsTableRequiresRebuild(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, dbName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE requests; CREATE TABLE requests (legacy TEXT); INSERT INTO requests(legacy) VALUES ('stale')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st := Status(home)
	if !st.RebuildRequired {
		t.Fatalf("status not rebuild-required %#v", st)
	}
	_, err = CatchUp(home)
	if !errors.Is(err, ErrRebuildRequired) {
		t.Fatalf("err=%v", err)
	}
	_, err = Lookup(home, "req_1")
	if !errors.Is(err, ErrRebuildRequired) {
		t.Fatalf("served incompatible table err=%v", err)
	}
	meta, err := Rebuild(home)
	if err != nil || meta.RebuildRequired || meta.IndexedRows != 1 {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
	if _, err := Lookup(home, "req_1"); err != nil {
		t.Fatal(err)
	}
}

func seedV2Home(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte(`{"requestId":"req_1","provider":"openai","model":"gpt-5","status":200}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CatchUp(home); err != nil {
		t.Fatal(err)
	}
	return home
}

func execDB(t *testing.T, home string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(home, dbName))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
}

func requireRebuildAndRepair(t *testing.T, home string) {
	t.Helper()
	st := Status(home)
	if !st.RebuildRequired {
		t.Fatalf("status not rebuild-required %#v", st)
	}
	_, err := CatchUp(home)
	if !errors.Is(err, ErrRebuildRequired) {
		t.Fatalf("catch-up err=%v", err)
	}
	_, err = Lookup(home, "req_1")
	if !errors.Is(err, ErrRebuildRequired) {
		t.Fatalf("lookup err=%v", err)
	}
	meta, err := Rebuild(home)
	if err != nil || meta.RebuildRequired || meta.IndexedRows != 1 {
		t.Fatalf("rebuild meta=%#v err=%v", meta, err)
	}
	if _, err := Lookup(home, "req_1"); err != nil {
		t.Fatal(err)
	}
}

func TestMissingRequestsTableDoesNotRecreateUnderCheckpoint(t *testing.T) {
	home := seedV2Home(t)
	execDB(t, home, `DROP TABLE requests`)
	names := map[string]bool{}
	db, err := sql.Open("sqlite", filepath.Join(home, dbName))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names[name] = true
	}
	rows.Close()
	db.Close()
	if names["requests"] {
		t.Fatal("fixture still has requests")
	}
	_, err = CatchUp(home)
	if !errors.Is(err, ErrRebuildRequired) {
		t.Fatalf("err=%v", err)
	}
	db, err = sql.Open("sqlite", filepath.Join(home, dbName))
	if err != nil {
		t.Fatal(err)
	}
	var n int
	err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='requests'`).Scan(&n)
	closeErr := db.Close()
	if err != nil || n != 0 {
		t.Fatalf("catch-up recreated requests n=%d err=%v", n, err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	requireRebuildAndRepair(t, home)
}

func TestIndexedRowsPositiveWithEmptyRequestsRequiresRebuild(t *testing.T) {
	home := seedV2Home(t)
	execDB(t, home, `DELETE FROM requests`)
	requireRebuildAndRepair(t, home)
}

func TestIndexedRowsZeroWithRequestsRequiresRebuild(t *testing.T) {
	home := seedV2Home(t)
	if err := writeMetaKV(home, map[string]string{"indexed_rows": "0"}); err != nil {
		t.Fatal(err)
	}
	requireRebuildAndRepair(t, home)
}

func TestCompositeRequestIDPrimaryKeyRequiresRebuild(t *testing.T) {
	home := seedV2Home(t)
	execDB(t, home,
		`DROP TABLE requests`,
		`CREATE TABLE requests (request_id TEXT NOT NULL, shard TEXT NOT NULL, row_json TEXT NOT NULL, PRIMARY KEY(request_id, shard))`,
		`INSERT INTO requests(request_id, shard, row_json) VALUES ('req_1', 'a', '{"requestId":"req_1"}')`,
	)
	requireRebuildAndRepair(t, home)
}

func TestExtraNotNullColumnRequiresRebuild(t *testing.T) {
	home := seedV2Home(t)
	execDB(t, home,
		`DROP TABLE requests`,
		`CREATE TABLE requests (request_id TEXT PRIMARY KEY, row_json TEXT NOT NULL, extra TEXT NOT NULL DEFAULT '')`,
	)
	requireRebuildAndRepair(t, home)
}

func TestExtraUniqueConstraintRequiresRebuild(t *testing.T) {
	home := seedV2Home(t)
	execDB(t, home,
		`DROP TABLE requests`,
		`CREATE TABLE requests (request_id TEXT PRIMARY KEY, row_json TEXT NOT NULL, alt TEXT UNIQUE)`,
		`INSERT INTO requests(request_id, row_json, alt) VALUES ('req_1', '{"requestId":"req_1"}', 'x')`,
	)
	requireRebuildAndRepair(t, home)
}

func TestFreshDBInitializesWithoutRebuild(t *testing.T) {
	home := t.TempDir()
	meta, err := CatchUp(home)
	if err != nil || meta.RebuildRequired || meta.IndexedOffset != 0 || meta.IndexedRows != 0 || !meta.CaughtUp {
		t.Fatalf("meta=%#v err=%v", meta, err)
	}
}

func TestUnrelatedUserTableIsNotFresh(t *testing.T) {
	home := t.TempDir()
	dbPath := filepath.Join(home, dbName)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE foreign_stuff (id TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = CatchUp(home)
	if !errors.Is(err, ErrRebuildRequired) {
		t.Fatalf("catch-up mutated foreign DB err=%v", err)
	}
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_meta'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	var foreign int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='foreign_stuff'`).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("catch-up created schema_meta on foreign DB n=%d", n)
	}
	if foreign != 1 {
		t.Fatalf("foreign table missing after catch-up foreign=%d", foreign)
	}
	if err := os.WriteFile(filepath.Join(home, "usage.jsonl"), []byte("{\"requestId\":\"req_1\",\"provider\":\"openai\",\"model\":\"gpt-5\",\"status\":200}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := Rebuild(home)
	if err != nil || meta.RebuildRequired || meta.IndexedRows != 1 {
		t.Fatalf("rebuild meta=%#v err=%v", meta, err)
	}
	if _, err := Lookup(home, "req_1"); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalPlusUnexpectedUserTableRequiresRebuild(t *testing.T) {
	home := seedV2Home(t)
	execDB(t, home, `CREATE TABLE foreign_stuff (id TEXT)`)
	requireRebuildAndRepair(t, home)
	db, err := sql.Open("sqlite", filepath.Join(home, dbName))
	if err != nil {
		t.Fatal(err)
	}
	var foreign int
	err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='foreign_stuff'`).Scan(&foreign)
	closeErr := db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if foreign != 0 {
		t.Fatalf("rebuild left foreign table foreign=%d", foreign)
	}
}
