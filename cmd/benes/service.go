package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/servicectl"
	"github.com/Wibias/Benes/internal/winsw"
)

const schedulerTaskName = "benes-proxy"

type schedulerTaskStatus string

const (
	schedulerTaskPresent schedulerTaskStatus = "present"
	schedulerTaskAbsent  schedulerTaskStatus = "absent"
	schedulerTaskUnknown schedulerTaskStatus = "unknown"
)

type schedulerTaskProbe struct {
	Status schedulerTaskStatus
	Detail string
}

var queryScheduler = querySchtasks

func runService(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 {
		args = []string{"status"}
	}
	if len(args) == 2 && args[0] == "install" && args[1] == "--native" {
		return runServiceInstallNative(stdout, stderr, deps)
	}
	if len(args) == 2 && args[0] == "install" && args[1] == "--scheduler" {
		args = args[:1]
	}
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: service status|start|stop|install|uninstall|repair [--native]")
		return 2
	}

	switch args[0] {
	case "status":
		return runServiceStatus(stdout, stderr, deps)
	case "start":
		return runServiceStart(stdout, stderr, deps)
	case "stop":
		return runServiceStop(stdout, stderr, deps)
	case "install":
		return runServiceInstall(stdout, stderr, deps)
	case "repair":
		return runServiceRepair(stdout, stderr, deps)
	case "uninstall", "remove":
		return runServiceUninstall(stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: service status|start|stop|install|uninstall|repair")
		return 2
	}
}

func runServiceRepair(stdout, stderr io.Writer, deps commandDependencies) int {
	task := probeSchedulerTask(schedulerTaskName)
	if task.Status == schedulerTaskUnknown {
		fmt.Fprintln(stderr, "benes: service repair: Task Scheduler state could not be verified")
		return 1
	}
	if task.Status != schedulerTaskPresent {
		fmt.Fprintln(stderr, "benes: service not installed. Run benes service install.")
		return 1
	}
	fmt.Fprintln(stdout, "service repair: refreshing assets.")
	return runServiceInstall(stdout, stderr, deps)
}

func runServiceStart(stdout, stderr io.Writer, deps commandDependencies) int {
	if paths, err := deps.resolvePaths(config.PathOptions{}); err == nil && nativeBackend(paths.Home) {
		if code := runServiceNativeStart(stderr, paths.Home); code != 0 {
			return code
		}
		sleep := deps.sleep
		if sleep == nil {
			sleep = time.Sleep
		}
		for range 40 {
			state, readErr := readRuntimePortState(paths.RuntimePort)
			if readErr == nil && state.PID > 0 && state.Port > 0 {
				fmt.Fprintf(stdout, "service started (PID %d, port %d).\n", state.PID, state.Port)
				return 0
			}
			sleep(50 * time.Millisecond)
		}
		fmt.Fprintln(stderr, "benes: service started but proxy did not publish runtime-port state")
		return 1
	}
	task := probeSchedulerTask(schedulerTaskName)
	if task.Status == schedulerTaskUnknown {
		fmt.Fprintln(stderr, "benes: service start: Task Scheduler state could not be verified")
		return 1
	}
	if task.Status != schedulerTaskPresent {
		fmt.Fprintln(stderr, "benes: service not installed. Run benes service install.")
		return 1
	}
	switch runtime.GOOS {
	case "linux":
		if _, err := querySystemctl([]string{"start", schedulerTaskName}); err != nil {
			fmt.Fprintf(stderr, "benes: start systemd unit: %v\n", err)
			return 1
		}
	case "darwin":
		if err := startLaunchdJob(); err != nil {
			fmt.Fprintf(stderr, "benes: start launchd job: %v\n", err)
			return 1
		}
	default:
		if _, err := queryScheduler([]string{"/run", "/tn", schedulerTaskName}); err != nil {
			fmt.Fprintf(stderr, "benes: run scheduler task: %v\n", err)
			return 1
		}
	}

	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve paths: %v\n", err)
		return 1
	}
	sleep := deps.sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	for range 40 {
		state, readErr := readRuntimePortState(paths.RuntimePort)
		if readErr == nil && state.PID > 0 && state.Port > 0 {
			fmt.Fprintf(stdout, "service started (PID %d, port %d).\n", state.PID, state.Port)
			return 0
		}
		sleep(50 * time.Millisecond)
	}
	fmt.Fprintln(stderr, "benes: service started but proxy did not publish runtime-port state")
	return 1
}

func runServiceStop(stdout, stderr io.Writer, deps commandDependencies) int {
	if paths, err := deps.resolvePaths(config.PathOptions{}); err == nil && nativeBackend(paths.Home) {
		if code := runServiceNativeStop(stderr, paths.Home); code != 0 {
			return code
		}
		return runStop(stdout, stderr, deps)
	}
	task := probeSchedulerTask(schedulerTaskName)
	if task.Status == schedulerTaskUnknown {
		fmt.Fprintln(stderr, "benes: service stop: Task Scheduler state could not be verified")
		return 1
	}
	if task.Status == schedulerTaskPresent {
		switch runtime.GOOS {
		case "linux":
			_, _ = querySystemctl([]string{"stop", schedulerTaskName})
		case "darwin":
			stopLaunchdIfInstalled()
		default:
			if _, err := queryScheduler([]string{"/End", "/TN", schedulerTaskName}); err != nil {
				fmt.Fprintf(stderr, "benes: end scheduler task: %v\n", err)
				return 1
			}
		}
	}

	return runStop(stdout, stderr, deps)
}

func runServiceStatus(stdout, stderr io.Writer, deps commandDependencies) int {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve paths: %v\n", err)
		return 1
	}
	proxy := "not-running"
	port := 0
	if state, err := readRuntimePortState(paths.RuntimePort); err == nil && state.PID > 0 && state.Port > 0 {
		proxy = "running"
		port = state.Port
	}
	if nativeBackend(paths.Home) {
		fmt.Fprintf(stdout, "native (WinSW %s): %s\n", winsw.Version, queryWinswStatus(paths.Home))
		if proxy == "running" {
			fmt.Fprintf(stdout, "proxy running on port %d.\n", port)
		}
		return 0
	}
	task := probeSchedulerTask(schedulerTaskName)
	fmt.Fprintln(stdout, formatSchedulerServiceStatus(task, proxy, port))
	return 0
}

func probeSchedulerTask(taskName string) schedulerTaskProbe {
	switch runtime.GOOS {
	case "windows":
		return probeWindowsTask(taskName)
	case "linux":
		return probeSystemdUnit(taskName)
	case "darwin":
		return probeLaunchd()
	default:
		return schedulerTaskProbe{Status: schedulerTaskAbsent}
	}
}

func probeWindowsTask(taskName string) schedulerTaskProbe {
	out, err := queryScheduler([]string{"/query", "/tn", taskName})
	if err == nil && strings.Contains(out, taskName) {
		return schedulerTaskProbe{Status: schedulerTaskPresent}
	}
	csv, csvErr := queryScheduler([]string{"/query", "/fo", "CSV"})
	if csvErr == nil {
		if schedulerCSVIncludesTask(csv, taskName) {
			return schedulerTaskProbe{Status: schedulerTaskPresent}
		}
		return schedulerTaskProbe{Status: schedulerTaskAbsent}
	}
	detail := csvErr.Error()
	if err != nil {
		detail = err.Error() + "; " + detail
	}
	return schedulerTaskProbe{Status: schedulerTaskUnknown, Detail: detail}
}

func probeSystemdUnit(taskName string) schedulerTaskProbe {
	out, err := querySystemctl([]string{"show", taskName, "--property=LoadState", "--value"})
	combined := strings.ToLower(strings.TrimSpace(out))
	if err != nil {
		detail := strings.ToLower(err.Error() + " " + combined)
		if systemdUnavailable(detail) {
			return schedulerTaskProbe{Status: schedulerTaskAbsent, Detail: err.Error()}
		}
		return schedulerTaskProbe{Status: schedulerTaskUnknown, Detail: err.Error()}
	}
	if combined == "" || combined == "not-found" {
		return schedulerTaskProbe{Status: schedulerTaskAbsent}
	}
	return schedulerTaskProbe{Status: schedulerTaskPresent}
}

func systemdUnavailable(detail string) bool {
	needles := []string{
		"executable file not found",
		"no such file",
		"failed to connect",
		"not been booted",
		"host is down",
		"connection refused",
	}
	for _, needle := range needles {
		if strings.Contains(detail, needle) {
			return true
		}
	}
	return false
}

func schedulerCSVIncludesTask(csv, taskName string) bool {
	needle := strings.ToLower(taskName)
	for _, line := range strings.Split(csv, "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, needle) {
			continue
		}
		if strings.Contains(lower, `"\`+needle+`"`) || strings.Contains(lower, `"`+needle+`"`) {
			return true
		}
	}
	return false
}

func formatSchedulerServiceStatus(task schedulerTaskProbe, proxy string, port int) string {
	manager := "Task Scheduler"
	if runtime.GOOS == "linux" {
		manager = "systemd --user"
	}
	if runtime.GOOS == "darwin" {
		manager = "launchd"
	}
	if task.Status == schedulerTaskPresent {
		if proxy == "running" {
			return fmt.Sprintf("service installed (%s); proxy running on port %d.", manager, port)
		}
		if proxy == "not-running" {
			return fmt.Sprintf("service installed (%s); proxy not running.", manager)
		}
		return fmt.Sprintf("service installed (%s); proxy status unknown.", manager)
	}
	if task.Status == schedulerTaskAbsent {
		if proxy == "running" {
			return fmt.Sprintf("service not installed (%s); proxy is running independently on port %d.", manager, port)
		}
		return fmt.Sprintf("service not installed (%s).", manager)
	}
	if proxy == "running" {
		return fmt.Sprintf("%s registration unknown; proxy running on port %d.", manager, port)
	}
	return fmt.Sprintf("service status unknown (%s query failed); proxy not running.", manager)
}

func querySchtasks(args []string) (string, error) {
	cmd := exec.Command("schtasks.exe", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var querySystemctl = querySystemctlCmd

func querySystemctlCmd(args []string) (string, error) {
	cmd := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

var publishServiceFile = servicectl.PublishUnit
var serviceExecutable = os.Executable

func stopInstalledServiceIfPresent(home string) {
	if strings.TrimSpace(home) != "" && nativeBackend(home) {
		_ = runServiceNativeStop(io.Discard, home)
	}
	switch runtime.GOOS {
	case "linux":
		if _, err := os.Stat(systemdUnitPath()); err == nil {
			_, _ = querySystemctl([]string{"stop", schedulerTaskName})
		}
	case "darwin":
		stopLaunchdIfInstalled()
	default:
		task := probeWindowsTask(schedulerTaskName)
		if task.Status == schedulerTaskPresent {
			_, _ = queryScheduler([]string{"/End", "/TN", schedulerTaskName})
		}
	}
}

func runServiceUninstall(stdout, stderr io.Writer, deps commandDependencies) int {
	if paths, err := deps.resolvePaths(config.PathOptions{}); err == nil && nativeBackend(paths.Home) {
		if code := runServiceNativeUninstall(stderr, paths.Home); code != 0 {
			return code
		}
		_ = os.Remove(filepath.Join(paths.Home, "service-state.json"))
		fmt.Fprintln(stdout, "native service uninstalled.")
		return 0
	}
	task := probeSchedulerTask(schedulerTaskName)
	if task.Status == schedulerTaskUnknown {
		fmt.Fprintln(stderr, "benes: service uninstall: Task Scheduler state could not be verified")
		return 1
	}
	if task.Status == schedulerTaskPresent {
		switch runtime.GOOS {
		case "linux":
			_, _ = querySystemctl([]string{"disable", "--now", schedulerTaskName})
			_ = os.Remove(systemdUnitPath())
			_, _ = querySystemctl([]string{"daemon-reload"})
		case "darwin":
			stopLaunchdIfInstalled()
			_ = os.Remove(launchdPlistPathFn())
		default:
			_, _ = queryScheduler([]string{"/End", "/TN", schedulerTaskName})
			if _, err := queryScheduler([]string{"/delete", "/tn", schedulerTaskName, "/f"}); err != nil {
				fmt.Fprintf(stderr, "benes: delete scheduler task: %v\n", err)
				return 1
			}
		}
	}

	code := runStop(stdout, stderr, deps)
	if paths, err := deps.resolvePaths(config.PathOptions{}); err == nil {
		_ = os.Remove(filepath.Join(paths.Home, "service-state.json"))
		_ = os.Remove(filepath.Join(paths.Home, "benes-service.cmd"))
		_ = os.Remove(filepath.Join(paths.Home, "benes-service-launcher.vbs"))
		_ = os.Remove(filepath.Join(paths.Home, "benes-service-task.xml"))
	}
	if code != 0 {
		return code
	}
	fmt.Fprintln(stdout, "service uninstalled.")
	return 0
}

func runServiceInstall(stdout, stderr io.Writer, deps commandDependencies) int {
	switch runtime.GOOS {
	case "windows":
		return runServiceInstallWindows(stdout, stderr, deps)
	case "linux":
		return runServiceInstallLinux(stdout, stderr, deps)
	case "darwin":
		return runServiceInstallDarwin(stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: service install supports Windows Task Scheduler, Linux systemd --user, and macOS launchd")
		return 1
	}
}

func runServiceInstallWindows(stdout, stderr io.Writer, deps commandDependencies) int {

	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve paths: %v\n", err)
		return 1
	}
	exe, err := serviceExecutable()
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve executable: %v\n", err)
		return 1
	}
	cmdPath := filepath.Join(paths.Home, "benes-service.cmd")
	vbsPath := filepath.Join(paths.Home, "benes-service-launcher.vbs")
	xmlPath := filepath.Join(paths.Home, "benes-service-task.xml")
	ctx := context.Background()
	if err := publishServiceFile(ctx, paths.Home, "benes-service.cmd", []byte(buildServiceCmd(exe, 23100, paths.Home))); err != nil {
		fmt.Fprintf(stderr, "benes: write service wrapper: %v\n", err)
		return 1
	}
	if err := publishServiceFile(ctx, paths.Home, "benes-service-launcher.vbs", []byte(buildServiceVBS(cmdPath))); err != nil {
		fmt.Fprintf(stderr, "benes: write service launcher: %v\n", err)
		return 1
	}
	if err := publishServiceFile(ctx, paths.Home, "benes-service-task.xml", encodeUTF16LE(buildServiceTaskXML(vbsPath))); err != nil {
		fmt.Fprintf(stderr, "benes: write service task XML: %v\n", err)
		return 1
	}
	if _, err := queryScheduler([]string{"/create", "/tn", schedulerTaskName, "/xml", xmlPath, "/f"}); err != nil {
		fmt.Fprintf(stderr, "benes: create scheduler task: %v\n", err)
		return 1
	}
	state, _ := json.Marshal(map[string]any{"version": 2, "backend": "scheduler"})
	if err := publishServiceFile(ctx, paths.Home, "service-state.json", append(state, '\n')); err != nil {
		fmt.Fprintf(stderr, "benes: write service state: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "service installed.")
	return runServiceStart(stdout, stderr, deps)
}

func runServiceInstallLinux(stdout, stderr io.Writer, deps commandDependencies) int {

	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve paths: %v\n", err)
		return 1
	}
	exe, err := serviceExecutable()
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve executable: %v\n", err)
		return 1
	}
	dir := systemdUserDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "benes: create systemd user dir: %v\n", err)
		return 1
	}
	unit := buildSystemdUnit(exe)
	if err := os.WriteFile(systemdUnitPath(), []byte(unit), 0o644); err != nil {
		fmt.Fprintf(stderr, "benes: write systemd unit: %v\n", err)
		return 1
	}
	if _, err := querySystemctl([]string{"daemon-reload"}); err != nil {
		fmt.Fprintf(stderr, "benes: systemd daemon-reload: %v\n", err)
		return 1
	}
	if _, err := querySystemctl([]string{"enable", "--now", schedulerTaskName}); err != nil {
		fmt.Fprintf(stderr, "benes: enable systemd unit: %v\n", err)
		return 1
	}
	state, _ := json.Marshal(map[string]any{"version": 2, "backend": "systemd"})
	if err := publishServiceFile(context.Background(), paths.Home, "service-state.json", append(state, '\n')); err != nil {
		fmt.Fprintf(stderr, "benes: write service state: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "service installed.")
	return runServiceStart(stdout, stderr, deps)
}

func systemdUserDir() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "systemd", "user")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "systemd", "user")
	}
	return filepath.Join(home, ".config", "systemd", "user")
}

func systemdUnitPath() string {
	return filepath.Join(systemdUserDir(), schedulerTaskName+".service")
}

func buildSystemdUnit(exe string) string {
	if strings.ContainsAny(exe, " \t") {
		exe = `"` + exe + `"`
	}
	return `[Unit]
Description=Benes proxy
After=network-online.target

[Service]
Type=simple
ExecStart=` + exe + ` start
Restart=on-failure
RestartSec=5
Environment=BENES_SERVICE=1

[Install]
WantedBy=default.target
`
}

func buildServiceCmd(exe string, port int, home string) string {

	logPath := filepath.Join(home, "service.log")
	return strings.Join([]string{
		"@echo off",
		"setlocal",
		"chcp 65001 >nul",
		"set BENES_SERVICE=1",
		":loop",
		fmt.Sprintf("\"%s\" start --port %d >>\"%s\" 2>&1", exe, port, logPath),
		"if %ERRORLEVEL% NEQ 0 (",
		"  ping -n 6 127.0.0.1 >nul",
		"  goto loop",
		")",
		"endlocal",
	}, "\r\n") + "\r\n"
}

func buildServiceVBS(script string) string {
	escaped := strings.ReplaceAll(script, `"`, `""`)
	return strings.Join([]string{
		"' Benes service launcher — runs the batch wrapper with a hidden window.",
		`Set shell = CreateObject("WScript.Shell")`,
		fmt.Sprintf(`shell.Run """%s""", 0, True`, escaped),
	}, "\r\n") + "\r\n"
}

func buildServiceTaskXML(launcher string) string {
	wscript := `C:\Windows\System32\wscript.exe`
	args := `/b /nologo "` + launcher + `"`
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>` + xmlEscape("Benes proxy service wrapper") + `</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>false</Hidden>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
  </Settings>

  <Actions Context="Author">
    <Exec>
      <Command>` + xmlEscape(wscript) + `</Command>
      <Arguments>` + xmlEscape(args) + `</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

func xmlEscape(value string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(value)
}

func encodeUTF16LE(s string) []byte {
	u := utf16.Encode([]rune(s))
	out := make([]byte, 2+len(u)*2)
	out[0], out[1] = 0xFF, 0xFE
	for i, r := range u {
		out[2+2*i] = byte(r)
		out[3+2*i] = byte(r >> 8)
	}
	return out
}
