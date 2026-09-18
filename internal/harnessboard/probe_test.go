package harnessboard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbeUsesRealDetectAndLogDirs(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	detect := filepath.Join(xdg, "opencode")
	logs := filepath.Join(detect, "logs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	UserHome = func() (string, error) { return home, nil }
	LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	ListProcesses = func() ([]Process, error) {
		return []Process{{Name: "opencode", Path: filepath.Join(detect, "opencode")}}, nil
	}
	t.Cleanup(ResetHooks)

	clients, err := Probe(ProbeInput{BenesHome: filepath.Join(home, ".benes")})
	if err != nil {
		t.Fatal(err)
	}
	row := clientByID(t, clients, "opencode")
	if row.DetectPath == nil || *row.DetectPath != detect {
		t.Fatalf("detect=%v", row.DetectPath)
	}
	if row.LogPath == nil || *row.LogPath != logs {
		t.Fatalf("log=%v", row.LogPath)
	}
	if row.Running == nil || !*row.Running {
		t.Fatalf("running=%v", row.Running)
	}
	if row.Token != TokenNone {
		t.Fatalf("token=%s", row.Token)
	}
}

func TestProbeNeverClaimsTokenValid(t *testing.T) {
	home := t.TempDir()
	UserHome = func() (string, error) { return home, nil }
	LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	ListProcesses = func() ([]Process, error) { return nil, nil }
	t.Cleanup(ResetHooks)
	clients, err := Probe(ProbeInput{BenesHome: filepath.Join(home, ".benes"), CodexHome: filepath.Join(home, ".codex")})
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != len(IDs) {
		t.Fatalf("clients=%d", len(clients))
	}
	for _, row := range clients {
		if row.Token == TokenValid {
			t.Fatalf("%s claimed token valid", row.ID)
		}
	}
	if clientByID(t, clients, "claude").Token != TokenMissing {
		t.Fatalf("claude token=%s", clientByID(t, clients, "claude").Token)
	}
	if clientByID(t, clients, "codex").Token != TokenMissing {
		t.Fatalf("codex token=%s", clientByID(t, clients, "codex").Token)
	}
}

func TestProbeProcessListErrorLeavesRunningUnknown(t *testing.T) {
	home := t.TempDir()
	UserHome = func() (string, error) { return home, nil }
	LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	ListProcesses = func() ([]Process, error) { return nil, os.ErrPermission }
	t.Cleanup(ResetHooks)
	clients, err := Probe(ProbeInput{BenesHome: filepath.Join(home, ".benes")})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range clients {
		if row.Running != nil {
			t.Fatalf("%s running=%v", row.ID, row.Running)
		}
	}
}

func clientByID(t *testing.T, clients []Client, id string) Client {
	t.Helper()
	for _, row := range clients {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("missing %s", id)
	return Client{}
}
