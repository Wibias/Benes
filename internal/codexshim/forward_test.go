package codexshim

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestForwardIfShimNoSidecar(t *testing.T) {
	prev := lookupExecutable
	lookupExecutable = func() (string, error) {
		return filepath.Join(t.TempDir(), "codex.exe"), nil
	}
	t.Cleanup(func() { lookupExecutable = prev })
	handled, code := ForwardIfShim(nil)
	if handled || code != 0 {
		t.Fatalf("handled=%v code=%d", handled, code)
	}
}

func TestForwardIfShimRunsBackup(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "codex.exe")
	backup := filepath.Join(dir, "real")
	if runtime.GOOS == "windows" {
		backup += ".cmd"
		if err := os.WriteFile(backup, []byte("@exit 7\r\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(backup, []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteSidecar(SidecarPath(wrapper), Sidecar{Marker: Marker, Backup: backup}); err != nil {
		t.Fatal(err)
	}
	prev := lookupExecutable
	lookupExecutable = func() (string, error) { return wrapper, nil }
	t.Cleanup(func() { lookupExecutable = prev })
	handled, code := ForwardIfShim([]string{"--version"})
	if !handled || code != 7 {
		t.Fatalf("handled=%v code=%d", handled, code)
	}
}

func TestEphemeralGoBinary(t *testing.T) {
	if !isEphemeralGoBinary(`C:\Users\ws\AppData\Local\Temp\go-build3370019911\b458\server.test.exe`) {
		t.Fatal("expected go-build path to be ephemeral")
	}
	if isEphemeralGoBinary(`C:\Users\ws\go\bin\benes.exe`) {
		t.Fatal("installed benes should be stable")
	}
}
