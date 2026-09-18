package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildAgentDefsHonorsInjectAgentsDefaultAndOptOut(t *testing.T) {
	on := true
	off := false
	dir := t.TempDir()
	if defs := BuildAgentDefs(CodeSettings{Model: "gpt-5.5"}, nil, dir, false); len(defs) == 0 {
		t.Fatal("nil injectAgents must default to exposing the roster")
	}
	if defs := BuildAgentDefs(CodeSettings{InjectAgents: &on, Model: "gpt-5.5"}, nil, dir, false); len(defs) == 0 {
		t.Fatal("injectAgents=true must expose the roster")
	}
	if defs := BuildAgentDefs(CodeSettings{InjectAgents: &off, Model: "gpt-5.5"}, nil, dir, false); len(defs) != 0 {
		t.Fatalf("injectAgents=false must skip sync, got %#v", defs)
	}
	if defs := BuildAgentDefs(CodeSettings{Enabled: &off, InjectAgents: &on, Model: "gpt-5.5"}, nil, dir, false); len(defs) != 0 {
		t.Fatalf("disabled Claude inbound must skip agent sync, got %#v", defs)
	}
}

func TestBuildAgentDefsUsesConfiguredRosterWhenSet(t *testing.T) {
	dir := t.TempDir()
	defs := BuildAgentDefs(CodeSettings{Model: "gpt-5.5"}, []string{"openai-apikey/gpt-5.6-sol"}, dir, true)
	if len(defs) != 2 {
		t.Fatalf("roster+self=%d %#v", len(defs), defs)
	}
	if defs[0].Name != "benes-gpt-5-6-sol" || defs[0].Model != "claude-benes-openai-apikey--gpt-5.6-sol" {
		t.Fatalf("roster def=%+v", defs[0])
	}
	if defs[1].Name != "benes-self" || defs[1].Model != "gpt-5.5" {
		t.Fatalf("self def=%+v", defs[1])
	}
	empty := BuildAgentDefs(CodeSettings{Model: "gpt-5.5"}, nil, dir, true)
	if len(empty) != 1 || empty[0].Name != "benes-self" {
		t.Fatalf("empty configured roster must still write self, got %#v", empty)
	}
}

func TestSyncAgentDefsWritesOwnedFilesWithoutUserSecrets(t *testing.T) {
	dir := t.TempDir()
	on := true
	defs := BuildAgentDefs(CodeSettings{Enabled: &on, Model: "gpt-5.5"}, nil, dir, false)
	if len(defs) == 0 {
		t.Fatal("expected defs")
	}
	if err := SyncAgentDefs(defs, dir); err != nil {
		t.Fatal(err)
	}
	self := filepath.Join(dir, "agents", "benes-self.md")
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, generatedMarker) || !strings.Contains(text, "benes-route: gpt-5.5") {
		t.Fatalf("%s", text)
	}
	if strings.Contains(text, "sk-") {
		t.Fatal("secret")
	}
}

func TestSyncAgentDefsDoesNotTouchUserAgents(t *testing.T) {
	dir := t.TempDir()
	agents := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agents, 0o700); err != nil {
		t.Fatal(err)
	}
	user := filepath.Join(agents, "benes-custom.md")
	if err := os.WriteFile(user, []byte("user owned"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SyncAgentDefs(nil, dir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(user)
	if string(got) != "user owned" {
		t.Fatalf("touched user file: %s", got)
	}
}
