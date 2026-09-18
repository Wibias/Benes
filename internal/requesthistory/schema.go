package requesthistory

import (
	"database/sql"
	"fmt"
	"strings"
)

type dbKind int

const (
	dbFresh dbKind = iota
	dbUsable
	dbInvalid
)

func classifyDB(query func(string, ...any) (*sql.Rows, error), queryRow func(string, ...any) *sql.Row) (dbKind, dbState, error) {
	names, err := userTables(query)
	if err != nil {
		return dbInvalid, dbState{}, err
	}
	hasMeta := names["schema_meta"]
	hasReq := names["requests"]
	unexpectedTable := false
	for name := range names {
		if name != "schema_meta" && name != "requests" {
			unexpectedTable = true
			break
		}
	}
	if !hasMeta && !hasReq {
		// Fresh only for a genuinely empty/new SQLite with no user-owned tables.
		// Unrelated package-external tables are rebuild-required, not mutated.
		if len(names) != 0 {
			state := dbState{rebuildRequired: true, trustworthy: false, lastError: "request-history index has unexpected user tables"}
			return dbInvalid, state, nil
		}
		return dbFresh, dbState{trustworthy: true, schemaVersion: schemaVersion}, nil
	}
	if !hasMeta || !hasReq {
		state := dbState{rebuildRequired: true, trustworthy: false, lastError: "request-history schema is incomplete"}
		return dbInvalid, state, nil
	}
	if unexpectedTable {
		state := dbState{rebuildRequired: true, trustworthy: false, lastError: "request-history index has unexpected user tables"}
		return dbInvalid, state, nil
	}
	if err := validateExactSchema(query, queryRow); err != nil {
		state := dbState{rebuildRequired: true, trustworthy: false, lastError: err.Error()}
		return dbInvalid, state, nil
	}
	state, err := scanState(func() (*sql.Rows, error) {
		return query(`SELECT key, value FROM schema_meta`)
	}, func() (bool, error) {
		return requestsExist(queryRow)
	}, func() (int, error) {
		return countRequests(queryRow)
	}, false)
	if err != nil {
		return dbInvalid, dbState{}, err
	}
	applyRowConsistency(&state)
	if err := applySchemaShape(query, queryRow, &state); err != nil {
		state.rebuildRequired = true
		state.trustworthy = false
		state.lastError = err.Error()
	}
	if state.rebuildRequired || !state.trustworthy {
		return dbInvalid, state, nil
	}
	return dbUsable, state, nil
}

func userTables(query func(string, ...any) (*sql.Rows, error)) (map[string]bool, error) {
	rows, err := query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names[name] = true
	}
	return names, rows.Err()
}

func applyRowConsistency(state *dbState) {
	if state == nil || state.rebuildRequired || !state.trustworthy {
		return
	}
	if state.schemaVersion != schemaVersion || !state.hasIndexedRows {
		return
	}
	if (state.indexedRows == 0) != (!state.hasRequests) {
		state.rebuildRequired = true
		state.trustworthy = false
		state.lastError = "request-history row count is inconsistent"
	}
}

func validateExactSchema(query func(string, ...any) (*sql.Rows, error), queryRow func(string, ...any) *sql.Row) error {
	if err := validateExactTable(query, "schema_meta", []colNeed{
		{name: "key", pk: true, text: true},
		{name: "value", notnull: true, text: true},
	}); err != nil {
		return err
	}
	if err := validateExactTable(query, "requests", []colNeed{
		{name: "request_id", pk: true, text: true},
		{name: "row_json", notnull: true, text: true},
		{name: "ledger_offset", notnull: true, integer: true},
	}); err != nil {
		return err
	}
	return validateCanonicalWriteShape(query, queryRow)
}

func validateCanonicalWriteShape(query func(string, ...any) (*sql.Rows, error), queryRow func(string, ...any) *sql.Row) error {
	trig, err := query(`SELECT name FROM sqlite_master WHERE type = 'trigger' AND tbl_name IN ('schema_meta', 'requests')`)
	if err != nil {
		return fmt.Errorf("request-history schema is unreadable")
	}
	defer trig.Close()
	if trig.Next() {
		var name string
		_ = trig.Scan(&name)
		return fmt.Errorf("request-history has unexpected trigger %s", name)
	}
	if err := trig.Err(); err != nil {
		return err
	}
	for _, table := range []string{"schema_meta", "requests"} {
		var createSQL sql.NullString
		if err := queryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&createSQL); err != nil {
			return fmt.Errorf("request-history %s is unreadable", table)
		}
		if !createSQL.Valid || strings.TrimSpace(createSQL.String) == "" {
			return fmt.Errorf("request-history %s create SQL is missing", table)
		}
		upper := strings.ToUpper(createSQL.String)
		if strings.Contains(upper, "CHECK") {
			return fmt.Errorf("request-history %s has unexpected CHECK constraint", table)
		}
		if strings.Contains(upper, "COLLATE") {
			return fmt.Errorf("request-history %s has unexpected collation", table)
		}
	}
	return nil
}

func validateExactTable(query func(string, ...any) (*sql.Rows, error), table string, need []colNeed) error {
	var info, indexes string
	switch table {
	case "schema_meta":
		info, indexes = `PRAGMA table_info(schema_meta)`, `PRAGMA index_list(schema_meta)`
	case "requests":
		info, indexes = `PRAGMA table_info(requests)`, `PRAGMA index_list(requests)`
	default:
		return fmt.Errorf("unknown index table")
	}
	rows, err := query(info)
	if err != nil {
		return fmt.Errorf("request-history %s is unreadable", table)
	}
	type colGot struct {
		colNeed
		ctype string
	}
	found := map[string]colGot{}
	pkCols := 0
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		found[name] = colGot{
			colNeed: colNeed{name: name, pk: pk > 0, notnull: notnull > 0 || pk > 0},
			ctype:   ctype,
		}
		if pk > 0 {
			pkCols++
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(found) == 0 {
		return fmt.Errorf("request-history %s is missing", table)
	}
	if len(found) != len(need) {
		return fmt.Errorf("request-history %s has incompatible columns", table)
	}
	wantPK := 0
	for _, want := range need {
		got, ok := found[want.name]
		if !ok {
			return fmt.Errorf("request-history %s is missing %s", table, want.name)
		}
		if want.text && !strings.EqualFold(strings.TrimSpace(got.ctype), "TEXT") {
			return fmt.Errorf("request-history %s.%s must be TEXT", table, want.name)
		}
		if want.integer {
			ct := strings.ToUpper(strings.TrimSpace(got.ctype))
			if ct != "INTEGER" && ct != "INT" {
				return fmt.Errorf("request-history %s.%s must be INTEGER", table, want.name)
			}
		}
		if want.pk {
			wantPK++
			if !got.pk {
				return fmt.Errorf("request-history %s.%s is not unique", table, want.name)
			}
		} else if got.pk {
			return fmt.Errorf("request-history %s.%s is not unique", table, want.name)
		}
		if want.notnull && !got.notnull {
			return fmt.Errorf("request-history %s.%s is nullable", table, want.name)
		}
	}
	if pkCols != wantPK {
		return fmt.Errorf("request-history %s primary key is composite", table)
	}
	idxRows, err := query(indexes)
	if err != nil {
		return fmt.Errorf("request-history %s is unreadable", table)
	}
	defer idxRows.Close()
	for idxRows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := idxRows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return err
		}
		if unique == 1 && !strings.EqualFold(origin, "pk") {
			return fmt.Errorf("request-history %s has extra unique constraint", table)
		}
	}
	return idxRows.Err()
}
