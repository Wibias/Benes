package harnessboard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRevealPathLogAndConfig(t *testing.T) {
	home := t.TempDir()
	logs := filepath.Join(home, "AppData", "Roaming", "Claude", "logs")
	config := filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	UserHome = func() (string, error) { return home, nil }
	osStat = os.Stat
	LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(ResetHooks)

	env := map[string]string{
		"APPDATA":     filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA": filepath.Join(home, "AppData", "Local"),
	}
	logPath, err := ResolveRevealPath(RevealInput{
		ClientID: "claude-desktop",
		Target:   RevealLog,
		Env:      env,
	})
	if err != nil {
		t.Fatal(err)
	}
	if logPath != logs {
		t.Fatalf("log=%q want=%q", logPath, logs)
	}
	configPath, err := ResolveRevealPath(RevealInput{
		ClientID: "claude-desktop",
		Target:   RevealConfig,
		Env:      env,
	})
	if err != nil {
		t.Fatal(err)
	}
	if configPath != config {
		t.Fatalf("config=%q want=%q", configPath, config)
	}
}

func TestRevealOpensAllowlistedPath(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	opencodeLogs := filepath.Join(xdg, "opencode", "logs")
	if err := os.MkdirAll(opencodeLogs, 0o700); err != nil {
		t.Fatal(err)
	}
	UserHome = func() (string, error) { return home, nil }
	LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	var opened string
	var openedDir bool
	OpenPath = func(path string, isDir bool) error {
		opened = path
		openedDir = isDir
		return nil
	}
	t.Cleanup(ResetHooks)

	path, err := Reveal(RevealInput{
		ClientID: "opencode",
		Target:   RevealLog,
		Env:      map[string]string{"XDG_CONFIG_HOME": xdg, "HOME": home},
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != opencodeLogs {
		t.Fatalf("path=%q want=%q", path, opencodeLogs)
	}
	if opened != opencodeLogs || !openedDir {
		t.Fatalf("opened=%q dir=%v", opened, openedDir)
	}
}

func TestResolveRevealPathRejectsUnknown(t *testing.T) {
	_, err := ResolveRevealPath(RevealInput{ClientID: "nope", Target: RevealLog})
	if !errors.Is(err, ErrUnknownClient) {
		t.Fatalf("err=%v", err)
	}
	_, err = ResolveRevealPath(RevealInput{ClientID: "opencode", Target: "nope"})
	if !errors.Is(err, ErrUnknownTarget) {
		t.Fatalf("err=%v", err)
	}
}
