package codexrestore

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreJournalWritesOriginalConfigAndDeletesJournal(t *testing.T) {
	home := t.TempDir()
	original := "model = \"gpt-5\"\n"
	injected := "[model_providers.benes]\nbase_url = \"http://127.0.0.1:23100/v1\"\nmodel_provider = \"benes\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(injected), 0o600); err != nil {
		t.Fatal(err)
	}
	enc := base64.StdEncoding.EncodeToString([]byte(original))
	sum := sha256.Sum256([]byte(injected))
	body, _ := json.Marshal(journal{
		Version:            1,
		OriginalConfig:     enc,
		OriginalProfile:    nil,
		InjectedConfigHash: hex.EncodeToString(sum[:]),
		PID:                1,
	})
	if err := os.WriteFile(filepath.Join(home, "benes-journal.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Restore(home)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ConfigRestored || !strings.Contains(got.Message, "journal") {
		t.Fatalf("result=%#v", got)
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || string(raw) != original {
		t.Fatalf("config=%q err=%v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(home, "benes-journal.json")); !os.IsNotExist(err) {
		t.Fatal("journal remained")
	}
}

func TestRestoreStripsManagedProviderWithoutJournal(t *testing.T) {
	home := t.TempDir()
	injected := "model = \"openai/gpt-5\"\nmodel_provider = \"benes\"\n\n[model_providers.benes]\nbase_url = \"http://127.0.0.1:23100/v1\"\n\n[model_providers.benes.http]\ntimeout = 1\n\n[projects.\"/tmp\"]\ntrust_level = \"trusted\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(injected), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Restore(home)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Stripped {
		t.Fatalf("result=%#v", got)
	}
	raw, _ := os.ReadFile(filepath.Join(home, "config.toml"))

	text := string(raw)
	if strings.Contains(text, "benes") {
		t.Fatalf("benes remained: %s", text)
	}
	if !strings.Contains(text, "[projects.\"/tmp\"]") {
		t.Fatalf("user table lost: %s", text)
	}
}

func TestRestoreMissingConfigIsSuccess(t *testing.T) {
	got, err := Restore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.Stripped || got.ConfigRestored {
		t.Fatalf("result=%#v", got)
	}
}

func TestStripRemovesOrphanSubtableAndKeepsUserBackup(t *testing.T) {
	orphan := strings.Join([]string{
		`model = "gpt-5.5"`,
		``,
		`[agents]`,
		`max_concurrent_threads_per_session = 8`,
		``,
		`[model_providers.benes.env_http_headers]`,
		`"x-benes-api-key" = "BENES_API_AUTH_TOKEN"`,
		``,
		`[model_providers.benes_backup]`,
		`name = "user backup"`,
		``,
	}, "\n")
	got := stripBenes(orphan)
	if strings.Contains(got, "env_http_headers") || strings.Contains(got, "BENES_API_AUTH_TOKEN") {
		t.Fatalf("orphan survived: %s", got)
	}
	if !strings.Contains(got, "[model_providers.benes_backup]") || !strings.Contains(got, "user backup") {
		t.Fatalf("backup lost: %s", got)
	}
	if !strings.Contains(got, "[agents]") || !strings.Contains(got, `model = "gpt-5.5"`) {
		t.Fatalf("user keys lost: %s", got)
	}
}

func TestStripRecognizesTrailingCommentsOnManagedHeaders(t *testing.T) {
	commented := strings.Join([]string{
		`model = "gpt-5.5"`,
		`[model_providers.benes] # managed provider`,
		`name = "Benes Proxy"`,
		`[model_providers.benes.env_http_headers] # managed sub-table`,
		`"x-benes-api-key" = "BENES_API_AUTH_TOKEN"`,
		``,
	}, "\n")
	got := stripBenes(commented)
	if strings.Contains(got, "benes") || strings.Contains(got, "Benes Proxy") {
		t.Fatalf("commented headers survived: %s", got)
	}
}

func TestRestoreDoesNotReplayOrphanedSubtableFromJournal(t *testing.T) {
	home := t.TempDir()
	original := strings.Join([]string{
		`model = "gpt-5"`,
		`[model_providers.benes.env_http_headers]`,
		`"x-benes-api-key" = "BENES_API_AUTH_TOKEN"`,
		``,
	}, "\n")
	injected := original + "[model_providers.benes]\nbase_url = \"http://127.0.0.1:1/v1\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(injected), 0o600); err != nil {
		t.Fatal(err)
	}
	enc := base64.StdEncoding.EncodeToString([]byte(original))
	sum := sha256.Sum256([]byte(injected))
	body, _ := json.Marshal(journal{
		Version:            1,
		OriginalConfig:     enc,
		OriginalProfile:    nil,
		InjectedConfigHash: hex.EncodeToString(sum[:]),
		PID:                1,
	})
	if err := os.WriteFile(filepath.Join(home, "benes-journal.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Restore(home)
	if err != nil || !got.ConfigRestored {
		t.Fatalf("result=%#v err=%v", got, err)
	}
	raw, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	text := string(raw)
	if strings.Contains(text, "benes") {
		t.Fatalf("orphan journaled as user baseline: %s", text)
	}
	if !strings.Contains(text, `model = "gpt-5"`) {
		t.Fatalf("user config lost: %s", text)
	}
}
