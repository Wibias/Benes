package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

type RefSet map[string]bool

func (r RefSet) HasFile(file classifiedFile) bool {
	if r == nil {
		return false
	}
	if r[file.RelPath] || r[filepath.ToSlash(file.AbsPath)] {
		return true
	}
	if r[strings.ToLower(file.RelPath)] {
		return true
	}
	return r[strings.ToLower(filepath.ToSlash(file.AbsPath))]
}

func referencedPaths(root Root) (RefSet, error) {
	dbPath := filepath.Join(sqliteHome(root.Abs), "state_5.sqlite")
	info, err := os.Lstat(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			return RefSet{}, nil
		}
		return nil, err
	}
	if isReparse(info) {
		return RefSet{}, nil
	}
	db, err := openStateDB(dbPath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	refs := RefSet{}
	tables, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
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
		if err := collectTableRefs(db, name, root, refs); err != nil {
			return nil, err
		}
	}
	return refs, nil
}

func collectTableRefs(db *sql.DB, table string, root Root, refs RefSet) error {
	rows, err := db.Query(`SELECT * FROM "` + table + `"`)
	if err != nil {
		return classifyBusy(err)
	}
	defer rows.Close()
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

func refRel(root Root, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	slash := filepath.ToSlash(value)
	if archivedPrefix(slash) && !filepath.IsAbs(value) && !looksDriveAbs(value) {
		norm, err := normalizeRel(slash)
		if err != nil {
			return ""
		}
		return norm
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(root.Abs, abs)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return ""
	}
	return rel
}

func sqliteText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	default:
		return ""
	}
}

func openStateDB(path string, readOnly bool) (*sql.DB, error) {
	mode := "rw"
	if readOnly {
		mode = "ro"
	}
	dsn := `file:` + filepath.ToSlash(path) + `?mode=` + mode + `&_pragma=busy_timeout(1)&_pragma=query_only=`
	if readOnly {
		dsn += "true"
	} else {
		dsn += "false"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, classifyBusy(err)
	}
	if !readOnly {
		if _, err := db.Exec(`BEGIN IMMEDIATE`); err != nil {
			_ = db.Close()
			return nil, classifyBusy(err)
		}
		if _, err := db.Exec(`COMMIT`); err != nil {
			_ = db.Close()
			return nil, classifyBusy(err)
		}
	} else if _, err := db.Exec(`SELECT 1`); err != nil {
		_ = db.Close()
		return nil, classifyBusy(err)
	}
	return db, nil
}

func classifyBusy(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "database is locked") || strings.Contains(msg, "database is busy") || strings.Contains(msg, "sqlite_busy") || strings.Contains(msg, "sqlite_locked") {
		return errCodexBusy
	}
	return err
}

var (
	errCodexBusy = errors.New(CodeCodexBusy)
	sqliteIdent  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	rootTomlAsgn = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_]+)\s*=\s*("(?:\\.|[^"])*"|'[^']*')`)
)

func sqliteHome(codexHome string) string {
	raw, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err == nil {
		if value := rootTomlString(string(raw), "sqlite_home"); value != "" {
			if filepath.IsAbs(value) {
				return value
			}
			if abs, err := filepath.Abs(value); err == nil {
				return abs
			}
		}
	}
	if env := strings.TrimSpace(os.Getenv("CODEX_SQLITE_HOME")); env != "" {
		if filepath.IsAbs(env) {
			return env
		}
		if abs, err := filepath.Abs(env); err == nil {
			return abs
		}
	}
	return codexHome
}

func rootTomlString(content, key string) string {
	end := strings.Index(content, "[")
	root := content
	if end >= 0 {
		root = content[:end]
	}
	for _, match := range rootTomlAsgn.FindAllStringSubmatch(root, -1) {
		if match[1] != key {
			continue
		}
		raw := match[2]
		if strings.HasPrefix(raw, `"`) {
			var value string
			if json.Unmarshal([]byte(raw), &value) == nil {
				return strings.TrimSpace(value)
			}
		}
		return strings.TrimSpace(strings.Trim(raw, "'"))
	}
	return ""
}
