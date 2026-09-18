package lab

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
)

func TestRebuildWritesProtocolVerdictsWithoutHTTP(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{
		"providers": {
			"openai-apikey": {"adapter":"openai-responses","models":["gpt-5.4","gpt-4o"]},
			"anthropic": {"adapter":"anthropic","models":["claude-sonnet-4"]}
		}
	}`)
	disk, err := decodeDisk(raw)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Rebuild(dir, disk, 1_700_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if result.SubjectCount != 3 || result.VerdictCount != 12 || result.EventCount < 13 {
		t.Fatalf("result=%+v", result)
	}
	proj, err := LoadProjection(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := proj.Verdicts[VerdictKey("openai-apikey/gpt-5.4", "codex")]
	if got.Verdict != "VERIFIED" || got.EvidenceLayer != "protocol_conformance" || got.EventID == "" {
		t.Fatalf("verdict=%+v", got)
	}
	body, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, "sk-") || strings.Contains(text, `Users`) {
		t.Fatalf("ledger leaked secrets or home path")
	}
}

func TestSubjectsSkipComboAndPolicy(t *testing.T) {
	disk, err := decodeDisk([]byte(`{"providers":{"combo":{"adapter":"openai-chat","models":["x"]},"openai-apikey":{"adapter":"openai-chat","models":["m"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	subjects := SubjectsFromConfig(disk)
	if len(subjects) != 1 || subjects[0].ID != "openai-apikey/m" {
		t.Fatalf("subjects=%+v", subjects)
	}
}

func decodeDisk(raw []byte) (config.DiskConfig, error) {
	var root struct {
		Providers map[string]json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return config.DiskConfig{}, err
	}
	return config.DiskConfig{Providers: root.Providers, Raw: raw}, nil
}
