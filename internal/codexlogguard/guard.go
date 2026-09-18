package codexlogguard

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	compatTrigger = "benes_log_guard_compat_v1"
	quietTrigger  = "benes_log_guard_quiet_v1"
)

type Mode string

const (
	ModeOff    Mode = "off"
	ModeCompat Mode = "compat"
	ModeQuiet  Mode = "quiet"
)

type Status struct {
	GeneratedAt        int64            `json:"generatedAt"`
	ExternalSqliteHome bool             `json:"externalSqliteHome"`
	Snapshot           string           `json:"snapshot"`
	Files              map[string]int64 `json:"files"`
	Schema             map[string]any   `json:"schema"`
	Capabilities       map[string]any   `json:"capabilities"`
	Metrics            map[string]any   `json:"metrics"`
	Protection         map[string]any   `json:"protection"`
}

type Mutation struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Status Status `json:"status"`
}

func LogsDBPath(codexHome string) string {
	return filepath.Join(sqliteHome(codexHome), "logs_2.sqlite")
}

func Inspect(codexHome string, desired Mode) Status {
	now := time.Now().UnixMilli()
	dbPath := LogsDBPath(codexHome)
	home := strings.TrimSpace(codexHome)
	external := sqliteHome(home) != home
	st := Status{
		GeneratedAt:        now,
		ExternalSqliteHome: external,
		Snapshot:           "checkpointed",
		Files: map[string]int64{
			"databaseBytes": fileSize(dbPath),
			"walBytes":      fileSize(dbPath + "-wal"),
			"shmBytes":      fileSize(dbPath + "-shm"),
		},
		Metrics: nil,
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			st.Schema = map[string]any{"state": "missing", "reason": "database_missing"}
			st.Capabilities = unsupportedCaps("database_missing")
			st.Protection = map[string]any{"desiredMode": desired, "observedMode": "off", "state": "unsupported"}
			return st
		}
		st.Schema = map[string]any{"state": "unreadable", "reason": "database_unreadable"}
		st.Capabilities = unsupportedCaps("database_unreadable")
		st.Protection = map[string]any{"desiredMode": desired, "observedMode": "off", "state": "unsupported"}
		return st
	}
	if !info.Mode().IsRegular() {
		st.Schema = map[string]any{"state": "unreadable", "reason": "database_unreadable"}
		st.Capabilities = unsupportedCaps("database_unreadable")
		st.Protection = map[string]any{"desiredMode": desired, "observedMode": "off", "state": "unsupported"}
		return st
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		st.Schema = map[string]any{"state": "unavailable", "reason": "inspect_failed"}
		st.Capabilities = unsupportedCaps("inspect_failed")
		st.Protection = map[string]any{"desiredMode": desired, "observedMode": "collision", "state": "unknown"}
		return st
	}
	defer db.Close()
	_, _ = db.Exec("PRAGMA query_only = ON")
	_, _ = db.Exec("PRAGMA busy_timeout = 1000")
	if !compatibleSchema(db) {
		st.Schema = map[string]any{"state": "unsupported", "reason": "unknown_schema"}
		st.Capabilities = unsupportedCaps("unknown_schema")
		st.Protection = map[string]any{"desiredMode": desired, "observedMode": "off", "state": "unsupported"}
		return st
	}
	st.Schema = map[string]any{"state": "compatible"}
	st.Capabilities = map[string]any{
		"inspection": map[string]any{"state": "supported"},
		"protection": map[string]any{"state": "supported"},
		"reclaim":    map[string]any{"state": "supported"},
	}
	observed := observeMode(db)
	st.Protection = protectionSummary(desired, observed)
	var total int
	_ = db.QueryRow(`SELECT COUNT(*) FROM logs`).Scan(&total)
	st.Metrics = map[string]any{
		"totalRows":         total,
		"rowsByLevel":       map[string]int{},
		"traceRows":         0,
		"traceShare":        0,
		"topTargets":        []any{},
		"pageSize":          pragmaInt(db, "page_size"),
		"pageCount":         pragmaInt(db, "page_count"),
		"freelistPages":     pragmaInt(db, "freelist_count"),
		"reclaimableBytes":  pragmaInt(db, "page_size") * pragmaInt(db, "freelist_count"),
		"estimatedLogBytes": nil,
	}
	return st
}

func Protect(codexHome string, mode Mode) Mutation {
	if mode != ModeCompat && mode != ModeQuiet && mode != ModeOff {
		return Mutation{Error: "invalid_mode", Status: Inspect(codexHome, ModeOff)}
	}
	dbPath := LogsDBPath(codexHome)
	if err := mutateTriggers(dbPath, mode); err != nil {
		return Mutation{Error: err.Error(), Status: Inspect(codexHome, mode)}
	}
	st := Inspect(codexHome, mode)
	return Mutation{OK: true, Status: st}
}

func Compact(codexHome string) Mutation {
	dbPath := LogsDBPath(codexHome)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return Mutation{Error: "database_error", Status: Inspect(codexHome, ModeOff)}
	}
	defer db.Close()
	_, _ = db.Exec("PRAGMA busy_timeout = 5000")
	if _, err := db.Exec("VACUUM"); err != nil {
		if isBusy(err) {
			return Mutation{Error: "busy", Status: Inspect(codexHome, ModeOff)}
		}
		return Mutation{Error: "database_error", Status: Inspect(codexHome, ModeOff)}
	}
	return Mutation{OK: true, Status: Inspect(codexHome, ModeOff)}
}

func mutateTriggers(dbPath string, mode Mode) error {
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("unsupported_schema")
		}
		return fmt.Errorf("database_error")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("database_error")
	}
	defer db.Close()
	_, _ = db.Exec("PRAGMA busy_timeout = 0")
	tx, err := db.Begin()
	if err != nil {
		if isBusy(err) {
			return fmt.Errorf("busy")
		}
		return fmt.Errorf("database_error")
	}
	if !compatibleSchemaTx(tx) && mode != ModeOff {
		_ = tx.Rollback()
		return fmt.Errorf("unsupported_schema")
	}
	for _, name := range []string{compatTrigger, quietTrigger} {
		if _, err := tx.Exec("DROP TRIGGER IF EXISTS " + name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("database_error")
		}
	}
	if mode == ModeCompat {
		if _, err := tx.Exec(compatSQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("database_error")
		}
	}
	if mode == ModeQuiet {
		if _, err := tx.Exec(quietSQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("database_error")
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("database_error")
	}
	return nil
}

func compatibleSchema(db *sql.DB) bool {
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='logs'`).Scan(&name); err != nil {
		return false
	}
	rows, err := db.Query(`PRAGMA table_info(logs)`)
	if err != nil {
		return false
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var colName, ctype string
		var dflt sql.NullString
		if rows.Scan(&cid, &colName, &ctype, &notnull, &dflt, &pk) != nil {
			return false
		}
		cols[colName] = true
	}
	for _, need := range []string{"id", "ts", "ts_nanos", "level", "target", "estimated_bytes"} {
		if !cols[need] {
			return false
		}
	}
	return true
}

func compatibleSchemaTx(tx *sql.Tx) bool {
	var name string
	if err := tx.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='logs'`).Scan(&name); err != nil {
		return false
	}
	return true
}

func observeMode(db *sql.DB) string {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='trigger' AND name IN (?, ?)`, compatTrigger, quietTrigger)
	if err != nil {
		return "collision"
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var name string
		if rows.Scan(&name) != nil {
			return "collision"
		}
		found[name] = true
	}
	if found[compatTrigger] && found[quietTrigger] {
		return "collision"
	}
	if found[compatTrigger] {
		return string(ModeCompat)
	}
	if found[quietTrigger] {
		return string(ModeQuiet)
	}
	return string(ModeOff)
}

func protectionSummary(desired Mode, observed string) map[string]any {
	state := "drifted"
	if observed == "collision" {
		state = "unknown"
	} else if desired == ModeOff && observed == "off" {
		state = "off"
	} else if desired != ModeOff && string(desired) == observed {
		state = "active"
	}
	return map[string]any{"desiredMode": desired, "observedMode": observed, "state": state}
}

func unsupportedCaps(reason string) map[string]any {
	cap := map[string]any{"state": "unsupported", "reason": reason}
	return map[string]any{"inspection": cap, "protection": cap, "reclaim": cap}
}

func pragmaInt(db *sql.DB, name string) int {
	var n int
	_ = db.QueryRow("PRAGMA " + name).Scan(&n)
	return n
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func isBusy(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "database is busy") || strings.Contains(msg, "sqlite_busy")
}

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
	for _, line := range strings.Split(root, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, key) {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != key {
			continue
		}
		raw := strings.TrimSpace(parts[1])
		if strings.HasPrefix(raw, `"`) {
			var value string
			if json.Unmarshal([]byte(raw), &value) == nil {
				return strings.TrimSpace(value)
			}
		}
		return strings.Trim(raw, "'")
	}
	return ""
}

const compatSQL = `CREATE TRIGGER benes_log_guard_compat_v1
BEFORE INSERT ON logs
WHEN
  NEW.target = 'log'
  OR NEW.target = 'codex_otel.log_only'
  OR NEW.target = 'codex_otel.trace_safe'
  OR NEW.target = 'codex_api::responses_websocket_timing'
  OR NEW.target = 'codex_core::post_sampling_token_estimate'
  OR ((NEW.target = 'hyper_util' OR substr(NEW.target, 1, 12) = 'hyper_util::') AND upper(NEW.level) IN ('TRACE', 'DEBUG', 'INFO'))
  OR (((NEW.target = 'codex_rmcp_client' OR substr(NEW.target, 1, 19) = 'codex_rmcp_client::') OR (NEW.target = 'rmcp' OR substr(NEW.target, 1, 6) = 'rmcp::')) AND upper(NEW.level) IN ('TRACE', 'DEBUG'))
  OR (((NEW.target = 'codex_http_client::transport' OR substr(NEW.target, 1, 29) = 'codex_http_client::transport::')
    OR (NEW.target = 'codex_api::sse' OR substr(NEW.target, 1, 15) = 'codex_api::sse::')
    OR (NEW.target = 'codex_tui::streaming::controller' OR substr(NEW.target, 1, 33) = 'codex_tui::streaming::controller::')
    OR (NEW.target = 'codex_tui::streaming::table_holdback' OR substr(NEW.target, 1, 37) = 'codex_tui::streaming::table_holdback::')) AND upper(NEW.level) = 'TRACE')
  OR (NEW.target = 'opentelemetry_sdk' AND upper(NEW.level) IN ('TRACE', 'DEBUG'))
BEGIN
  SELECT RAISE(IGNORE);
END`

const quietSQL = `CREATE TRIGGER benes_log_guard_quiet_v1
BEFORE INSERT ON logs
WHEN upper(NEW.level) = 'TRACE'
BEGIN
  SELECT RAISE(IGNORE);
END`
