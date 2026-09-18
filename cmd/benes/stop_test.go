package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/codexrestore"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/runtimestate"
)

func muteServiceBackends(t *testing.T) {
	t.Helper()
	prevLaunch := queryLaunchctl
	queryLaunchctl = func([]string) (string, error) { return "", os.ErrNotExist }
	prevSystemd := querySystemctl
	querySystemctl = func([]string) (string, error) { return "not-found\n", nil }
	prevSched := queryScheduler
	queryScheduler = func([]string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() {
		queryLaunchctl = prevLaunch
		querySystemctl = prevSystemd
		queryScheduler = prevSched
	})
}

func TestRunStopReportsMissingProxy(t *testing.T) {
	muteServiceBackends(t)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", t.TempDir())
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, PID: filepath.Join(home, "benes.pid"), RuntimePort: filepath.Join(home, "runtime-port.json")}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runStop(&stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "No running proxy found.") {
		t.Fatalf("stdout=%q", got)
	}
}

func TestRunStopKillsProcessAndClearsPidFiles(t *testing.T) {
	muteServiceBackends(t)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", t.TempDir())
	pidPath := filepath.Join(home, "benes.pid")

	portPath := filepath.Join(home, "runtime-port.json")
	cmd := hungProcess(t)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := cmd.Path
	if exe == "" {
		exe = hungProcess(t).Path
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":`+strconv.Itoa(cmd.Process.Pid)+`,"port":9,"hostname":"127.0.0.1","exe":`+strconv.Quote(exe)+`}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, PID: pidPath, RuntimePort: portPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runStop(&stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "stopped") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	_ = cmd.Wait()
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("pid file still present: %v", err)
	}
	if _, err := os.Stat(portPath); !os.IsNotExist(err) {
		t.Fatalf("runtime-port still present: %v", err)
	}
}

func TestRunStopRestoresInjectedCodexConfig(t *testing.T) {
	muteServiceBackends(t)
	benesHome := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := codexrestore.Inject(codexHome, "http://127.0.0.1:18080/v1"); err != nil {
		t.Fatal(err)
	}

	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: benesHome, PID: filepath.Join(benesHome, "benes.pid"), RuntimePort: filepath.Join(benesHome, "runtime-port.json")}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runStop(&stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	raw, _ := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if strings.Contains(string(raw), "benes") {
		t.Fatalf("config=%s stdout=%s", raw, stdout.String())
	}
}

func TestRunStopDoesNotKillUnrelatedPID(t *testing.T) {
	muteServiceBackends(t)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", t.TempDir())
	pidPath := filepath.Join(home, "benes.pid")
	portPath := filepath.Join(home, "runtime-port.json")
	cmd := hungProcess(t)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":`+strconv.Itoa(cmd.Process.Pid)+`,"port":9,"hostname":"127.0.0.1","exe":"benes"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, PID: pidPath, RuntimePort: portPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runStop(&stdout, &stderr, deps); code != 1 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "not this Benes process") {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if !runtimestate.Inspect(cmd.Process.Pid).Alive {
		t.Fatal("unrelated process was terminated")
	}
}

func hungProcess(t *testing.T) *exec.Cmd {
	t.Helper()
	if runtime.GOOS == "windows" {
		ping := filepath.Join(os.Getenv("WINDIR"), "System32", "ping.exe")
		return exec.Command(ping, "-n", "60", "127.0.0.1")
	}
	return exec.Command("sleep", "60")
}

func TestRunStopUnloadsLaunchdWhenPlistExists(t *testing.T) {
	muteServiceBackends(t)
	home := t.TempDir()
	t.Setenv("CODEX_HOME", t.TempDir())
	plist := filepath.Join(t.TempDir(), launchdLabel+".plist")
	if err := os.WriteFile(plist, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	previousPath := launchdPlistPathFn
	launchdPlistPathFn = func() string { return plist }
	unloaded := false
	queryLaunchctl = func(args []string) (string, error) {
		if len(args) > 0 && args[0] == "unload" {
			unloaded = true
		}
		return "", nil
	}
	t.Cleanup(func() { launchdPlistPathFn = previousPath })
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, PID: filepath.Join(home, "benes.pid"), RuntimePort: filepath.Join(home, "runtime-port.json")}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runStop(&stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if runtime.GOOS == "darwin" && !unloaded {
		t.Fatal("benes stop did not unload the launchd agent")
	}
	if runtime.GOOS != "darwin" && unloaded {
		t.Fatal("non-darwin stop unloaded a launchd plist")
	}
}
