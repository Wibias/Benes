package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRecoverHistoryRequiresLegacyFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runRecoverHistory(nil, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "--legacy-openai") {
		t.Fatalf("help code=%d stdout=%q", code, stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := runRecoverHistory([]string{"nope"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "--legacy-openai") {
		t.Fatalf("bad flag code=%d stderr=%q", code, stderr.String())
	}
}

func TestRunRecoverHistoryRecoversFixture(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	rollout := filepath.Join(home, "thread.jsonl")
	if err := os.WriteFile(rollout, []byte(`{"type":"session_meta","payload":{"id":"t1","model_provider":"benes","source":"cli"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Create sqlite via RecoverLegacyOpenAI's own package test shape.
	var stdout, stderr bytes.Buffer
	if code := runRecoverHistory([]string{"--legacy-openai"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Recovered 0 legacy thread(s)") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}
