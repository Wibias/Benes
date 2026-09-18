package codexhistory

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRecoverLegacyOpenAIUpdatesThreadsAndRollout(t *testing.T) {
	home := t.TempDir()
	rollout := filepath.Join(home, "thread.jsonl")
	meta := `{"type":"session_meta","timestamp":"2024-01-01T00:00:00Z","payload":{"id":"thread-1","model_provider":"benes","source":"exec"}}` + "\n"
	if err := os.WriteFile(rollout, []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(home, "state_5.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY,
		rollout_path TEXT,
		model_provider TEXT,
		source TEXT,
		has_user_event INTEGER,
		first_user_message TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads VALUES ('thread-1', ?, 'benes', 'exec', 0, 'hello')`, rollout); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := RecoverLegacyOpenAI(home)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rows != 1 || result.Files != 1 {
		t.Fatalf("result=%#v", result)
	}
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var provider, source string
	var hasUser int
	if err := db.QueryRow(`SELECT model_provider, source, has_user_event FROM threads WHERE id = 'thread-1'`).Scan(&provider, &source, &hasUser); err != nil {
		t.Fatal(err)
	}
	if provider != "openai" || source != "cli" || hasUser != 1 {
		t.Fatalf("provider=%s source=%s hasUser=%d", provider, source, hasUser)
	}
	raw, err := os.ReadFile(rollout)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"model_provider":"openai"`) {
		t.Fatalf("rollout=%s", raw)
	}
}

func TestRecoverLegacyOpenAIMissingDBIsNoop(t *testing.T) {
	result, err := RecoverLegacyOpenAI(t.TempDir())
	if err != nil || result.Rows != 0 || result.Files != 0 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
