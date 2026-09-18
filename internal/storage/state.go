package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
)

type stateLock struct {
	root      Root
	db        *sql.DB
	conn      *sql.Conn
	txOpen    bool
	committed bool
}

func lockState(root Root) (*stateLock, error) {
	s := &stateLock{root: root}
	dbPath := filepath.Join(sqliteHome(root.Abs), "state_5.sqlite")
	info, err := os.Lstat(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, classifyBusy(err)
	}
	if isReparse(info) {
		return s, nil
	}
	db, err := sql.Open("sqlite", `file:`+filepath.ToSlash(dbPath)+`?mode=rw&_pragma=busy_timeout(1)&_pragma=query_only=false`)
	if err != nil {
		return nil, classifyBusy(err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		_ = db.Close()
		return nil, classifyBusy(err)
	}
	if _, err := conn.ExecContext(context.Background(), `BEGIN IMMEDIATE`); err != nil {
		_ = conn.Close()
		_ = db.Close()
		return nil, classifyBusy(err)
	}
	s.db = db
	s.conn = conn
	s.txOpen = true
	return s, nil
}

func (s *stateLock) Close() {
	if s == nil {
		return
	}
	if s.txOpen && s.conn != nil && !s.committed {
		_, _ = s.conn.ExecContext(context.Background(), `ROLLBACK`)
		s.txOpen = false
	}
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
	if s.db != nil {
		_ = s.db.Close()
		s.db = nil
	}
}

func (s *stateLock) Commit() error {
	if s == nil || s.conn == nil || !s.txOpen {
		return nil
	}
	if _, err := s.conn.ExecContext(context.Background(), `COMMIT`); err != nil {
		return classifyBusy(err)
	}
	s.txOpen = false
	s.committed = true
	return nil
}

func (s *stateLock) Refs() (RefSet, error) {
	if s == nil || s.conn == nil {
		return RefSet{}, nil
	}
	return referencedPathsOn(s.conn, s.root)
}

func (s *stateLock) SnapshotThreads(files []classifiedFile) ([]map[string]any, error) {
	if s == nil || s.conn == nil {
		return nil, nil
	}
	return snapshotThreadsOn(s.conn, s.root, files)
}

func (s *stateLock) DeleteThreadIDs(ids []string) error {
	if s == nil || s.conn == nil || len(ids) == 0 {
		return nil
	}
	stmt, err := s.conn.PrepareContext(context.Background(), `DELETE FROM threads WHERE id = ?`)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil
		}
		return classifyBusy(err)
	}
	defer stmt.Close()
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, err := stmt.ExecContext(context.Background(), id); err != nil {
			return classifyBusy(err)
		}
	}
	return nil
}

func (s *stateLock) RestoreThreads(rows []map[string]any) error {
	if s == nil || s.conn == nil || len(rows) == 0 {
		return nil
	}
	for _, row := range rows {
		if err := upsertThreadConn(s.conn, row); err != nil {
			return err
		}
	}
	return nil
}

func (s *stateLock) InsertThread(id, rollout string) error {
	if s == nil || s.conn == nil {
		return errCodexBusy
	}
	_, err := s.conn.ExecContext(context.Background(), `INSERT INTO threads (id, rollout_path) VALUES (?, ?)`, id, rollout)
	return classifyBusy(err)
}

func referencedPathsOn(conn *sql.Conn, root Root) (RefSet, error) {
	refs := RefSet{}
	tables, err := conn.QueryContext(context.Background(), `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, classifyBusy(err)
	}
	defer tables.Close()
	var names []string
	for tables.Next() {
		var name string
		if err := tables.Scan(&name); err != nil {
			return nil, err
		}
		if sqliteIdent.MatchString(name) {
			names = append(names, name)
		}
	}
	if err := tables.Err(); err != nil {
		return nil, err
	}
	for _, name := range names {
		if err := collectTableRefsConn(conn, name, root, refs); err != nil {
			return nil, err
		}
	}
	return refs, nil
}

func collectTableRefsConn(conn *sql.Conn, table string, root Root, refs RefSet) error {
	rows, err := conn.QueryContext(context.Background(), `SELECT * FROM "`+table+`"`)
	if err != nil {
		return classifyBusy(err)
	}
	defer rows.Close()
	return scanRefRows(rows, root, refs)
}

func scanRefRows(rows *sql.Rows, root Root, refs RefSet) error {
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for _, value := range raw {
			text := sqliteText(value)
			if text == "" {
				continue
			}
			rel := refRel(root, text)
			if rel == "" || !archivedPrefix(rel) {
				continue
			}
			refs[rel] = true
			refs[strings.ToLower(rel)] = true
			refs[filepath.ToSlash(text)] = true
		}
	}
	return rows.Err()
}

func snapshotThreadsOn(conn *sql.Conn, root Root, files []classifiedFile) ([]map[string]any, error) {
	want := map[string]bool{}
	for _, file := range files {
		want[file.RelPath] = true
		want[filepath.ToSlash(file.AbsPath)] = true
		want[strings.ToLower(file.RelPath)] = true
	}
	rows, err := conn.QueryContext(context.Background(), `SELECT * FROM threads`)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil, nil
		}
		return nil, classifyBusy(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		rec := map[string]any{}
		match := false
		for i, col := range cols {
			text := sqliteText(raw[i])
			if text != "" {
				rec[col] = text
			} else if raw[i] != nil {
				rec[col] = raw[i]
			}
			rel := refRel(root, text)
			if rel != "" && want[rel] {
				match = true
			}
			if want[text] || want[filepath.ToSlash(text)] {
				match = true
			}
		}
		if match {
			out = append(out, rec)
		}
	}
	return out, rows.Err()
}

func upsertThreadConn(conn *sql.Conn, row map[string]any) error {
	id, _ := row["id"].(string)
	if strings.TrimSpace(id) == "" {
		return nil
	}
	cols := make([]string, 0, len(row))
	vals := make([]any, 0, len(row))
	ph := make([]string, 0, len(row))
	for key, value := range row {
		if !sqliteIdent.MatchString(key) {
			continue
		}
		cols = append(cols, `"`+key+`"`)
		vals = append(vals, value)
		ph = append(ph, "?")
	}
	if len(cols) == 0 {
		return nil
	}
	query := `INSERT OR REPLACE INTO threads (` + strings.Join(cols, ",") + `) VALUES (` + strings.Join(ph, ",") + `)`
	_, err := conn.ExecContext(context.Background(), query, vals...)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table") {
		return nil
	}
	return classifyBusy(err)
}

func threadIDs(rows []map[string]any) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		id, _ := row["id"].(string)
		if strings.TrimSpace(id) != "" {
			out = append(out, id)
		}
	}
	return out
}
