package codexrestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInjectWritesProviderAndJournalThenRestoreRoundTrips(t *testing.T) {
	home := t.TempDir()
	original := "model = \"gpt-5\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Inject(home, "http://127.0.0.1:18080/v1")
	if err != nil || !got.Changed {
		t.Fatalf("inject=%#v err=%v", got, err)
	}
	raw, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	text := string(raw)
	if !strings.Contains(text, "[model_providers.benes]") || !strings.Contains(text, "http://127.0.0.1:18080/v1") {
		t.Fatalf("injected=%s", text)
	}
	// The injected table also carries the managed harness identity marker, so
	// assert the declared surface header instead of pinning the whole table.
	if !strings.Contains(text, `"X-Benes-Surface" = "codex"`) {
		t.Fatalf("missing declared Codex surface header: %s", text)
	}
	if strings.Contains(text, "env_http_headers") {
		t.Fatalf("surface header used env_http_headers: %s", text)
	}
	if _, err := os.Stat(filepath.Join(home, "benes-journal.json")); err != nil {
		t.Fatal(err)
	}
	restored, err := Restore(home)
	if err != nil || !restored.ConfigRestored {
		t.Fatalf("restore=%#v err=%v", restored, err)
	}
	back, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	if string(back) != original {
		t.Fatalf("roundtrip=%q", back)
	}
}

func TestInjectIsIdempotentForSameBaseURL(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Inject(home, "http://127.0.0.1:18080/v1")
	if err != nil || !first.Changed {
		t.Fatalf("first=%#v err=%v", first, err)
	}
	second, err := Inject(home, "http://127.0.0.1:18080/v1")
	if err != nil || second.Changed {
		t.Fatalf("second=%#v err=%v", second, err)
	}
}

func TestInjectUpdatesExistingProviderBaseURL(t *testing.T) {
	home := t.TempDir()
	existing := "model_provider = \"benes\"\n[model_providers.benes]\nbase_url = \"http://127.0.0.1:1/v1\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Inject(home, "http://127.0.0.1:2/v1")
	if err != nil || !got.Changed {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	raw, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	text := string(raw)
	if !strings.Contains(text, "http://127.0.0.1:2/v1") || strings.Contains(text, "http://127.0.0.1:1/v1") {
		t.Fatalf("config=%s", text)
	}
}

func TestInjectSkipsMissingConfig(t *testing.T) {
	got, err := Inject(t.TempDir(), "http://127.0.0.1:1/v1")
	if err != nil || got.Changed {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestInjectPlacesModelProviderAtRootWhenConfigEndsWithTable(t *testing.T) {
	home := t.TempDir()
	existing := strings.Join([]string{
		`model = "gpt-5.6-sol"`,
		``,
		`[features]`,
		`multi_agent_v2 = true`,
		``,
		`[tui.model_availability_nux]`,
		`"gpt-5.5" = 4`,
		``,
	}, "\n")
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Inject(home, "http://127.0.0.1:23100/v1")
	if err != nil || !got.Changed {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	rootEnd := strings.Index(text, "[")
	if rootEnd < 0 {
		t.Fatalf("no tables:\n%s", text)
	}
	if !strings.Contains(text[:rootEnd], `model_provider = "benes"`) {
		t.Fatalf("model_provider not at root:\n%s", text)
	}
	nux := tableSection(text, "[tui.model_availability_nux]")
	if strings.Contains(nux, "model_provider") {
		t.Fatalf("model_provider nested under last table:\n%s", nux)
	}
	if !strings.Contains(text, "[features]") || !strings.Contains(text, `"gpt-5.5" = 4`) {
		t.Fatalf("user tables lost:\n%s", text)
	}
	if !strings.Contains(text, "[model_providers.benes]") || !strings.Contains(text, "http://127.0.0.1:23100/v1") {
		t.Fatalf("missing provider table:\n%s", text)
	}
}

func tableSection(content, header string) string {
	start := strings.Index(content, header)
	if start < 0 {
		return ""
	}
	rest := content[start:]
	if next := strings.Index(rest[1:], "["); next >= 0 {
		return rest[:next+1]
	}
	return rest
}

func TestInjectStripsOrphanSubtableInsteadOfJournalingIt(t *testing.T) {
	home := t.TempDir()
	existing := strings.Join([]string{
		`model = "gpt-5"`,
		``,
		`[model_providers.benes.env_http_headers]`,
		`"x-benes-api-key" = "BENES_API_AUTH_TOKEN"`,
		``,
		`[model_providers.benes_backup]`,
		`name = "user backup"`,
		``,
	}, "\n")
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Inject(home, "http://127.0.0.1:9/v1")
	if err != nil || !got.Changed {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	raw, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	text := string(raw)
	if strings.Contains(text, "env_http_headers") {
		t.Fatalf("orphan survived inject: %s", text)
	}
	if !strings.Contains(text, "[model_providers.benes_backup]") {
		t.Fatalf("backup lost: %s", text)
	}
	journalRaw, err := os.ReadFile(filepath.Join(home, "benes-journal.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(journalRaw), "env_http_headers") {
		t.Fatalf("orphan journaled: %s", journalRaw)
	}
}
