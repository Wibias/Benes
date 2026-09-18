package codexshim

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallRemovesShimAndRestoresBackup(t *testing.T) {
	home := t.TempDir()
	original := filepath.Join(home, "codex")
	wrapper := filepath.Join(home, "codex-wrapper")
	backup := filepath.Join(home, "codex.bak")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\n# benes codex autostart shim\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("real-codex\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(map[string]string{
		"platform":     "linux",
		"wrapperPath":  wrapper,
		"originalPath": original,
		"backupPath":   backup,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(StatePath(home), append(state, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, message, err := Uninstall(home)
	if err != nil {
		t.Fatal(err)
	}
	if !removed || !strings.Contains(message, original) {
		t.Fatalf("removed=%v message=%q", removed, message)
	}
	if _, err := os.Stat(wrapper); !os.IsNotExist(err) {
		t.Fatal("wrapper remained")
	}
	body, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "real-codex\n" {
		t.Fatalf("restored=%q", body)
	}
	if _, err := os.Stat(StatePath(home)); !os.IsNotExist(err) {
		t.Fatal("state remained")
	}
}

func TestUninstallRemovesPESidecarAndWrapper(t *testing.T) {
	home := t.TempDir()
	original := filepath.Join(home, "codex.exe")
	wrapper := filepath.Join(home, "codex-wrapper.exe")
	backup := filepath.Join(home, "codex.benes-real.exe")
	if err := os.WriteFile(wrapper, []byte("MZ-wrapper"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("MZ-real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteSidecar(SidecarPath(wrapper), Sidecar{Marker: Marker, Benes: "benes", Backup: backup}); err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(map[string]string{
		"platform":     "windows",
		"wrapperPath":  wrapper,
		"originalPath": original,
		"backupPath":   backup,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(StatePath(home), append(state, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, message, err := Uninstall(home)
	if err != nil {
		t.Fatal(err)
	}
	if !removed || !strings.Contains(message, original) {
		t.Fatalf("removed=%v message=%q", removed, message)
	}
	if _, err := os.Stat(wrapper); !os.IsNotExist(err) {
		t.Fatal("PE wrapper remained")
	}
	if _, err := os.Stat(SidecarPath(wrapper)); !os.IsNotExist(err) {
		t.Fatal("sidecar remained")
	}
	body, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "MZ-real" {
		t.Fatalf("restored=%q", body)
	}
}

func TestStatusReportsMissingAndPresent(t *testing.T) {
	home := t.TempDir()
	if got := Status(home); got != "Codex autostart shim is not installed." {
		t.Fatalf("absent=%q", got)
	}
	if err := os.WriteFile(StatePath(home), []byte(`{"platform":"linux","wrappers":[{"wrapperPath":"/tmp/w","originalPath":"/tmp/o","backupPath":"/tmp/b"}]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Status(home); !strings.Contains(got, "installed") {
		t.Fatalf("present=%q", got)
	}
}
