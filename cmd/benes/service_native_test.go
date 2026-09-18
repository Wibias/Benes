package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/winsw"
)

func TestRunServiceInstallNativeMissingBinaryFailsClosed(t *testing.T) {
	home := t.TempDir()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	deps := commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home}, nil
		},
	}
	prevExe := serviceExecutable
	serviceExecutable = func() (string, error) { return filepath.Join(home, "benes"), nil }
	defer func() { serviceExecutable = prevExe }()
	code := runService([]string{"install", "--native"}, stdout, stderr, deps)
	if runtime.GOOS != "windows" {
		if code == 0 || !strings.Contains(stderr.String(), "Windows-only") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
		return
	}
	if code == 0 || !strings.Contains(stderr.String(), "WinSW binary missing") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestNativeStopUnknownStatusFailsClosed(t *testing.T) {
	home := t.TempDir()
	prevRun, prevStatus := runWinsw, queryWinswStatus
	runWinsw = func(string, []string) (string, error) { return "", nil }
	queryWinswStatus = func(string) winsw.Status { return winsw.StatusUnknown }
	defer func() {
		runWinsw = prevRun
		queryWinswStatus = prevStatus
	}()
	stderr := &bytes.Buffer{}
	if code := runServiceNativeStop(stderr, home); code == 0 {
		t.Fatal("unknown stop accepted")
	}
}

func TestLocalSystemInstallRollsBack(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("native install is Windows-only")
	}
	home := t.TempDir()
	exeDir := winsw.Dir(home)
	if err := os.MkdirAll(exeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(winsw.ExePath(home), []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	uninstalled := false
	prevStatus, prevRun, prevPrompt, prevQC, prevExe, prevPublish := queryWinswStatus, runWinsw, runWinswPrompt, queryServiceQC, serviceExecutable, publishServiceFile
	queryWinswStatus = func(string) winsw.Status { return winsw.StatusNonexistent }
	runWinswPrompt = func(string, []string) error { return nil }
	queryServiceQC = func() (string, error) { return "SERVICE_START_NAME : LocalSystem", nil }
	runWinsw = func(_ string, args []string) (string, error) {
		if len(args) > 0 && args[0] == "uninstall" {
			uninstalled = true
		}
		return "", nil
	}
	serviceExecutable = func() (string, error) { return filepath.Join(home, "benes.exe"), nil }
	publishServiceFile = func(context.Context, string, string, []byte) error { return nil }
	defer func() {
		queryWinswStatus = prevStatus
		runWinsw = prevRun
		runWinswPrompt = prevPrompt
		queryServiceQC = prevQC
		serviceExecutable = prevExe
		publishServiceFile = prevPublish
	}()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	deps := commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home}, nil
		},
	}
	code := runServiceInstallNative(stdout, stderr, deps)
	if code == 0 || !uninstalled || !strings.Contains(stderr.String(), "LocalSystem") {
		t.Fatalf("code=%d uninstalled=%v stderr=%q", code, uninstalled, stderr)
	}
}
