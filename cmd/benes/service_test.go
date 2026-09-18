package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/config"
)

func stubSystemd(t *testing.T, loadState string) {
	t.Helper()
	previous := querySystemctl
	querySystemctl = func([]string) (string, error) {
		return loadState + "\n", nil
	}
	t.Cleanup(func() { querySystemctl = previous })
}

func TestFormatSchedulerServiceStatus(t *testing.T) {
	got := formatSchedulerServiceStatus(schedulerTaskProbe{Status: schedulerTaskPresent}, "running", 18080)
	if !strings.Contains(got, "service installed") || !strings.Contains(got, "18080") {
		t.Fatalf("got=%q", got)
	}
	got = formatSchedulerServiceStatus(schedulerTaskProbe{Status: schedulerTaskAbsent}, "not-running", 0)
	if !strings.Contains(got, "service not installed") {
		t.Fatalf("got=%q", got)
	}
}

func TestBuildSystemdUnitQuotesExecStart(t *testing.T) {
	unit := buildSystemdUnit(`/usr/local/bin/benes`)
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/benes start") {
		t.Fatalf("unit=%q", unit)
	}
	if !strings.Contains(unit, "Environment=BENES_SERVICE=1") {
		t.Fatal(unit)
	}
	quoted := buildSystemdUnit(`/opt/my bin/benes`)
	if !strings.Contains(quoted, `ExecStart="/opt/my bin/benes" start`) {
		t.Fatalf("quoted=%q", quoted)
	}
}

func TestRunServiceStatusUsesRuntimePort(t *testing.T) {
	stubSystemd(t, "not-found")
	home := t.TempDir()
	runtime := filepath.Join(home, "runtime-port.json")

	if err := os.WriteFile(runtime, []byte(`{"pid":9,"port":18080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := queryScheduler
	queryScheduler = func(args []string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "/tn") {
			return "", errors.New("missing")
		}
		return `"TaskName"` + "\n" + `"\\other"` + "\n", nil
	}
	t.Cleanup(func() { queryScheduler = previous })
	var stdout, stderr bytes.Buffer
	code := runService([]string{"status"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "18080") && !strings.Contains(stdout.String(), "not installed") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunServiceStopEndsPresentTaskThenStopsProxy(t *testing.T) {
	stubSystemd(t, "not-found")
	t.Setenv("CODEX_HOME", t.TempDir())
	home := t.TempDir()

	ended := false
	previous := queryScheduler
	queryScheduler = func(args []string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "/End") {
			ended = true
			return "", nil
		}
		if strings.Contains(joined, "/tn") {
			return schedulerTaskName + " Ready", nil
		}
		return `"TaskName"` + "\n", errors.New("unused")
	}
	t.Cleanup(func() { queryScheduler = previous })
	var stdout, stderr bytes.Buffer
	code := runService([]string{"stop"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, PID: filepath.Join(home, "benes.pid"), RuntimePort: filepath.Join(home, "runtime-port.json")}, nil
		},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if runtime.GOOS == "windows" && !ended {
		t.Fatal("scheduler task was not ended")
	}
	if !strings.Contains(stdout.String(), "No running proxy found.") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunServiceStartRunsPresentTask(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Task Scheduler is Windows-only")
	}
	home := t.TempDir()
	runtime := filepath.Join(home, "runtime-port.json")
	ran := false
	previous := queryScheduler
	queryScheduler = func(args []string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "/run") {
			ran = true
			return "", os.WriteFile(runtime, []byte(`{"pid":12,"port":18080}`), 0o600)
		}
		if strings.Contains(joined, "/tn") {
			return schedulerTaskName + " Ready", nil
		}
		return "", errors.New("unused")
	}
	t.Cleanup(func() { queryScheduler = previous })
	var stdout, stderr bytes.Buffer
	code := runService([]string{"start"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
		sleep: func(time.Duration) {},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !ran {
		t.Fatal("scheduler task was not run")
	}
	if !strings.Contains(stdout.String(), "18080") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunServiceStartRejectsAbsentTask(t *testing.T) {
	stubSystemd(t, "not-found")
	previous := queryScheduler
	queryScheduler = func(args []string) (string, error) {
		return "", errors.New("missing")
	}
	t.Cleanup(func() { queryScheduler = previous })

	var stdout, stderr bytes.Buffer
	code := runService([]string{"start"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: filepath.Join(t.TempDir(), "missing.json")}, nil
		},
	})
	if code != 1 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func stubDarwinInstallFailure(t *testing.T) commandDependencies {
	t.Helper()
	home := t.TempDir()
	plist := filepath.Join(home, "com.benes.proxy.plist")
	prevPlist := launchdPlistPathFn
	prevLaunchctl := queryLaunchctl
	launchdPlistPathFn = func() string { return plist }
	queryLaunchctl = func([]string) (string, error) { return "", errors.New("launchctl unavailable") }
	t.Cleanup(func() {
		launchdPlistPathFn = prevPlist
		queryLaunchctl = prevLaunchctl
	})
	return commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home}, nil
		},
	}
}

func TestRunServiceInstallSchedulerFlagIsAccepted(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
		t.Skip("install is exercised by platform-specific tests")
	}
	var stdout, stderr bytes.Buffer
	if code := runService([]string{"install", "--scheduler"}, &stdout, &stderr, stubDarwinInstallFailure(t)); code != 1 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestRunServiceRejectsUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runService([]string{"bogus"}, &stdout, &stderr, commandDependencies{}); code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunServiceRepairRejectsAbsentTask(t *testing.T) {
	stubSystemd(t, "not-found")
	previous := queryScheduler

	queryScheduler = func(args []string) (string, error) {
		return "", errors.New("missing")
	}
	t.Cleanup(func() { queryScheduler = previous })
	var stdout, stderr bytes.Buffer
	code := runService([]string{"repair"}, &stdout, &stderr, commandDependencies{})
	if code != 1 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestRunServiceUninstallDeletesPresentTask(t *testing.T) {
	stubSystemd(t, "not-found")
	t.Setenv("CODEX_HOME", t.TempDir())
	home := t.TempDir()

	deleted := false
	previous := queryScheduler
	queryScheduler = func(args []string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "/delete") {
			deleted = true
			return "", nil
		}
		if strings.Contains(joined, "/End") {
			return "", nil
		}
		if strings.Contains(joined, "/tn") {
			return schedulerTaskName + " Ready", nil
		}
		return "", errors.New("unused")
	}
	t.Cleanup(func() { queryScheduler = previous })
	var stdout, stderr bytes.Buffer
	code := runService([]string{"uninstall"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, PID: filepath.Join(home, "missing.pid"), RuntimePort: filepath.Join(home, "missing.json")}, nil
		},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if runtime.GOOS == "windows" && !deleted {
		t.Fatal("scheduler task was not deleted")
	}
	if !strings.Contains(stdout.String(), "service uninstalled.") {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunServiceInstallRejectedOffWindows(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
		t.Skip("Windows and Linux install are exercised separately")
	}
	var stdout, stderr bytes.Buffer
	if code := runService([]string{"install"}, &stdout, &stderr, stubDarwinInstallFailure(t)); code != 1 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}

func TestProbeSystemdUnitAbsentAndPresent(t *testing.T) {
	previous := querySystemctl
	t.Cleanup(func() { querySystemctl = previous })
	querySystemctl = func(args []string) (string, error) {
		return "not-found\n", nil
	}
	if got := probeSystemdUnit(schedulerTaskName); got.Status != schedulerTaskAbsent {
		t.Fatalf("absent=%#v", got)
	}
	querySystemctl = func(args []string) (string, error) {
		return "loaded\n", nil
	}
	if got := probeSystemdUnit(schedulerTaskName); got.Status != schedulerTaskPresent {
		t.Fatalf("present=%#v", got)
	}
	querySystemctl = func(args []string) (string, error) {
		return "Failed to connect to bus", errors.New("dial unix /run/user/0/bus: connect: no such file or directory")
	}
	if got := probeSystemdUnit(schedulerTaskName); got.Status != schedulerTaskAbsent {
		t.Fatalf("unavailable=%#v", got)
	}

}

func TestRunServiceStartLinuxStartsUnit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd --user is Linux-only")
	}
	home := t.TempDir()
	runtime := filepath.Join(home, "runtime-port.json")
	started := false
	previous := querySystemctl
	querySystemctl = func(args []string) (string, error) {
		if len(args) > 0 && args[0] == "start" {
			started = true
			return "", os.WriteFile(runtime, []byte(`{"pid":4,"port":18080}`), 0o600)
		}
		return "loaded\n", nil
	}
	t.Cleanup(func() { querySystemctl = previous })
	var stdout, stderr bytes.Buffer
	code := runService([]string{"start"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{RuntimePort: runtime}, nil
		},
		sleep: func(time.Duration) {},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !started {
		t.Fatal("systemd unit was not started")
	}
}

func TestRunServiceInstallLinuxWritesUnit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd --user is Linux-only")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	runtime := filepath.Join(home, "runtime-port.json")
	enabled := false
	previous := querySystemctl
	querySystemctl = func(args []string) (string, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "enable") {
			enabled = true
		}
		if len(args) > 0 && args[0] == "start" {
			return "", os.WriteFile(runtime, []byte(`{"pid":5,"port":18080}`), 0o600)
		}
		if len(args) > 0 && args[0] == "show" {
			return "loaded\n", nil
		}
		return "", nil
	}
	t.Cleanup(func() { querySystemctl = previous })
	var stdout, stderr bytes.Buffer
	code := runService([]string{"install"}, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, RuntimePort: runtime}, nil
		},
		sleep: func(time.Duration) {},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !enabled {
		t.Fatal("systemd unit was not enabled")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "systemd", "user", schedulerTaskName+".service")); err != nil {
		t.Fatalf("unit missing: %v", err)
	}
}

func TestBuildLaunchdPlistPinsGoCLI(t *testing.T) {
	t.Setenv("PATH", "/usr/local/bin:/usr/bin:/bin")
	plist := buildLaunchdPlist("/opt/benes", "/Users/me/.benes")
	for _, want := range []string{
		"<string>com.benes.proxy</string>",
		"<string>/opt/benes</string>",
		"<string>start</string>",
		"<key>KeepAlive</key>",
		"<key>RunAtLoad</key>",
		"<key>StandardOutPath</key>",
		"<string>/Users/me/.benes/service.log</string>",
		"<key>BENES_HOME</key>",
		"<string>/Users/me/.benes</string>",
		"<string>1</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Fatalf("missing %q in %s", want, plist)
		}
	}
	if strings.Contains(plist, "src/cli/index.ts") {
		t.Fatal("launchd plist points at a TypeScript CLI")
	}
}

func TestStartLaunchdJobTreatsAlreadyLoadedAsSuccess(t *testing.T) {
	previous := queryLaunchctl
	queryLaunchctl = func(args []string) (string, error) {
		if len(args) > 0 && args[0] == "load" {
			return "Load failed: 5: Input/output error", errors.New("exit 1")
		}
		return launchdLabel + "\t0\tbenes\n", nil
	}
	t.Cleanup(func() { queryLaunchctl = previous })
	if err := startLaunchdJob(); err != nil {
		t.Fatalf("already-loaded start: %v", err)
	}
}

func TestStopLaunchdIfInstalledUnloadsPlist(t *testing.T) {
	dir := t.TempDir()
	plist := filepath.Join(dir, launchdLabel+".plist")
	if err := os.WriteFile(plist, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	previousPath := launchdPlistPathFn
	launchdPlistPathFn = func() string { return plist }
	unloaded := false
	previous := queryLaunchctl
	queryLaunchctl = func(args []string) (string, error) {
		if len(args) > 0 && args[0] == "unload" {
			unloaded = true
		}
		return "", nil
	}
	t.Cleanup(func() {
		launchdPlistPathFn = previousPath
		queryLaunchctl = previous
	})
	stopLaunchdIfInstalled()
	if !unloaded {
		t.Fatal("installed launchd plist was not unloaded")
	}
}

func TestServiceLifecycleWorkflowBuildsGoCLI(t *testing.T) {
	root := ".."
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		root = "../.."
	}
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "service-lifecycle.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "src/cli/index.ts") {
		t.Fatal("service-lifecycle points at a TypeScript CLI")
	}
	if !strings.Contains(text, "go build -o benes") || !strings.Contains(text, "service_launchd.go") {
		t.Fatal("service-lifecycle does not build the Go CLI")
	}
}
