package codexshim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallWritesGoOwnedWrapperAndState(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	original := filepath.Join(bin, "codex")
	if runtime.GOOS == "windows" {
		original += ".cmd"
	}
	if err := os.WriteFile(original, []byte("#!/bin/sh\necho fake-codex\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevLook, prevProbe, prevExe := lookPath, probeVersion, benesExecutable
	lookPath = func(string) (string, error) { return original, nil }
	probeVersion = func(string) error { return nil }
	benesExecutable = func() (string, error) { return filepath.Join(bin, "benes"), nil }
	defer func() {
		lookPath = prevLook
		probeVersion = prevProbe
		benesExecutable = prevExe
	}()
	ok, message, err := Install(home)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v message=%q", ok, err, message)
	}
	body, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), Marker) || !strings.Contains(string(body), "benes") {
		t.Fatalf("wrapper=%q", body)
	}
	if _, err := os.Stat(backupPathFor(original)); err != nil {
		t.Fatal(err)
	}
	if got := Status(home); !strings.Contains(got, "installed") {
		t.Fatalf("status=%q", got)
	}
}

func TestInstallRefusesExistingBackup(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	original := filepath.Join(bin, "codex")
	backup := backupPathFor(original)
	if err := os.WriteFile(original, []byte("real\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevLook, prevProbe := lookPath, probeVersion
	lookPath = func(string) (string, error) { return original, nil }
	probeVersion = func(string) error { return nil }
	defer func() {
		lookPath = prevLook
		probeVersion = prevProbe
	}()
	ok, message, err := Install(home)
	if err != nil || ok || !strings.Contains(message, "backup") {
		t.Fatalf("ok=%v err=%v message=%q", ok, err, message)
	}
}

func TestInstallWritesPEWrapperForExe(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	original := filepath.Join(bin, "codex.exe")
	benesPE := filepath.Join(bin, "benes.exe")
	if err := os.WriteFile(original, []byte("MZ-real-codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(benesPE, []byte("MZ-benes-pe-"+Marker), 0o755); err != nil {
		t.Fatal(err)
	}
	prevLook, prevProbe, prevExe := lookPath, probeVersion, benesExecutable
	lookPath = func(name string) (string, error) {
		if name == "codex" {
			return original, nil
		}
		return "", os.ErrNotExist
	}
	probeVersion = func(string) error { return nil }
	benesExecutable = func() (string, error) { return benesPE, nil }
	defer func() {
		lookPath = prevLook
		probeVersion = prevProbe
		benesExecutable = prevExe
	}()
	ok, message, err := Install(home)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v message=%q", ok, err, message)
	}
	body, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 2 || body[0] != 'M' || body[1] != 'Z' {
		t.Fatalf("wrapper is not PE: %q", body)
	}
	if strings.Contains(string(body), "@echo off") {
		t.Fatalf("wrote batch script as .exe: %q", body)
	}
	spec, err := ReadSidecar(SidecarPath(original))
	if err != nil || spec == nil {
		t.Fatalf("sidecar err=%v spec=%v", err, spec)
	}
	if spec.Backup != backupPathFor(original) || spec.Benes != benesPE {
		t.Fatalf("sidecar=%+v", spec)
	}
	if !isShim(original) {
		t.Fatal("PE wrapper not detected as shim")
	}
}

func TestInstallRefusesBatchAsExeWithoutPESource(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	original := filepath.Join(bin, "codex.exe")
	if err := os.WriteFile(original, []byte("MZ-real-codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevLook, prevProbe, prevExe := lookPath, probeVersion, benesExecutable
	lookPath = func(name string) (string, error) {
		if name == "codex" {
			return original, nil
		}
		return "", os.ErrNotExist
	}
	probeVersion = func(string) error { return nil }
	benesExecutable = func() (string, error) { return filepath.Join(bin, "missing-benes"), nil }
	defer func() {
		lookPath = prevLook
		probeVersion = prevProbe
		benesExecutable = prevExe
	}()
	ok, message, err := Install(home)
	if err != nil {
		t.Fatal(err)
	}
	if ok || !strings.Contains(message, "PE") {
		t.Fatalf("ok=%v message=%q", ok, message)
	}
	body, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "MZ-real-codex" {
		t.Fatalf("original not restored: %q", body)
	}
	if _, err := os.Stat(SidecarPath(original)); !os.IsNotExist(err) {
		t.Fatal("sidecar leaked after rollback")
	}
}

func TestInstallRollsBackFailedProbe(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	original := filepath.Join(bin, "codex")
	if err := os.WriteFile(original, []byte("real\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevLook, prevProbe := lookPath, probeVersion
	lookPath = func(string) (string, error) { return original, nil }
	probeVersion = func(string) error { return os.ErrPermission }
	defer func() {
		lookPath = prevLook
		probeVersion = prevProbe
	}()
	ok, message, err := Install(home)
	if err != nil || ok || !strings.Contains(message, "probe") {
		t.Fatalf("ok=%v err=%v message=%q", ok, err, message)
	}
	body, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "real\n" {
		t.Fatalf("original not restored: %q", body)
	}
}
