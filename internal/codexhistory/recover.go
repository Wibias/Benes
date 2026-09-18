package codexhistory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Result struct {
	Rows  int
	Files int
}

func RecoverLegacyOpenAI(codexHome string) (Result, error) {
	codexHome = strings.TrimSpace(codexHome)
	if codexHome == "" {
		return Result{}, fmt.Errorf("CODEX_HOME is required")
	}
	dbPath := filepath.Join(sqliteHome(codexHome), "state_5.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return Result{}, nil
		}
		return Result{}, err
	}
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		result, err := recoverOnce(dbPath)
		if err == nil {
			return result, nil
		}
		if !isBusy(err) {
			return Result{}, err
		}
		last = err
		time.Sleep(500 * time.Millisecond)
	}
	return Result{}, last
}

func recoverOnce(dbPath string) (Result, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return Result{}, err
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return Result{}, err
	}
	rows, err := db.Query(`
		SELECT id, rollout_path, model_provider, source
		FROM threads
		WHERE model_provider = 'benes'
		  AND trim(coalesce(first_user_message, '')) != ''
	`)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	type thread struct {
		id      string
		rollout string
		source  string
	}
	var threads []thread
	for rows.Next() {
		var row thread
		var provider string
		if err := rows.Scan(&row.id, &row.rollout, &provider, &row.source); err != nil {
			return Result{}, err
		}
		threads = append(threads, row)
	}
	if err := rows.Err(); err != nil {
		return Result{}, err
	}
	files := 0
	for _, row := range threads {
		source := ""
		if row.source == "exec" {
			source = "cli"
		}
		ok, err := updateSessionMeta(row.rollout, row.id, "openai", source)
		if err == nil && ok {
			files++
		}
	}
	tx, err := db.Begin()
	if err != nil {
		return Result{}, err
	}
	stmt, err := tx.Prepare(`
		UPDATE threads
		SET model_provider = 'openai',
		    source = CASE WHEN source = 'exec' THEN 'cli' ELSE source END,
		    has_user_event = 1
		WHERE id = ?
	`)
	if err != nil {
		_ = tx.Rollback()
		return Result{}, err
	}
	defer stmt.Close()
	for _, row := range threads {
		if _, err := stmt.Exec(row.id); err != nil {
			_ = tx.Rollback()
			return Result{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Result{}, err
	}
	return Result{Rows: len(threads), Files: files}, nil
}

func updateSessionMeta(path, expectedID, provider, source string) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	latest, ok := latestSessionMeta(string(raw))
	if !ok {
		return false, nil
	}
	payload, _ := latest["payload"].(map[string]any)
	if payload == nil {
		return false, nil
	}
	id, _ := payload["id"].(string)
	if id != expectedID {
		return false, nil
	}
	changed := false
	if provider != "" {
		current, _ := payload["model_provider"].(string)
		if current != provider {
			payload["model_provider"] = provider
			changed = true
		}
	}
	if source != "" {
		current, _ := payload["source"].(string)
		if current != source {
			payload["source"] = source
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	latest["payload"] = payload
	latest["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	line, err := json.Marshal(latest)
	if err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return false, err
	}
	_ = f.Sync()
	return true, nil
}

func latestSessionMeta(raw string) (map[string]any, bool) {
	lines := strings.Split(raw, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || !strings.Contains(line, `"session_meta"`) {
			continue
		}
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		if record["type"] != "session_meta" {
			continue
		}
		if _, ok := record["payload"].(map[string]any); !ok {
			continue
		}
		return record, true
	}
	return nil, false
}

func sqliteHome(codexHome string) string {
	raw, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err == nil {
		if value := rootTomlString(string(raw), "sqlite_home"); value != "" {
			if filepath.IsAbs(value) {
				return value
			}
			abs, err := filepath.Abs(value)
			if err == nil {
				return abs
			}
		}
	}
	if env := strings.TrimSpace(os.Getenv("CODEX_SQLITE_HOME")); env != "" {
		if filepath.IsAbs(env) {
			return env
		}
		abs, err := filepath.Abs(env)
		if err == nil {
			return abs
		}
	}
	return codexHome
}

var rootTomlAssign = regexp.MustCompile(`(?m)^\s*([A-Za-z0-9_]+)\s*=\s*("(?:\\.|[^"])*"|'[^']*')`)

func rootTomlString(content, key string) string {
	end := strings.Index(content, "[")
	root := content
	if end >= 0 {
		root = content[:end]
	}
	for _, match := range rootTomlAssign.FindAllStringSubmatch(root, -1) {
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

func isBusy(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "database is busy") || strings.Contains(msg, "sqlite_busy") || strings.Contains(msg, "sqlite_locked")
}
