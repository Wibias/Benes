package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/wintray"
)

type trayState struct {
	RunValue     string `json:"runValue"`
	RunCommand   string `json:"runCommand"`
	LauncherPath string `json:"launcherPath"`
	Script       string `json:"script"`
}

func uninstallWindowsTray(deps commandDependencies) (bool, string, error) {
	if runtime.GOOS != "windows" {
		return false, "not Windows", nil
	}
	homes := uninstallHomes(deps)
	removed := false
	for _, home := range homes {
		ok, err := uninstallWindowsTrayHome(home)
		if err != nil {
			return false, "", err
		}
		if ok {
			removed = true
		}
	}
	if !removed {
		return false, "not installed", nil
	}
	return true, "Windows tray removed", nil
}

func uninstallWindowsTrayHome(home string) (bool, error) {
	path := filepath.Join(home, "tray-state.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var state trayState
	if json.Unmarshal(raw, &state) != nil || strings.TrimSpace(state.RunValue) == "" {
		return false, fmt.Errorf("tray-state.json is invalid")
	}
	query := exec.Command("reg.exe", "query", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", state.RunValue, "/reg:64")
	out, err := query.CombinedOutput()
	if err == nil {
		if state.RunCommand != "" && !strings.Contains(string(out), state.RunCommand) {
			return false, fmt.Errorf("refusing to remove a foreign HKCU Run value %s", state.RunValue)
		}
		del := exec.Command("reg.exe", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", state.RunValue, "/f", "/reg:64")
		if err := del.Run(); err != nil {
			return false, fmt.Errorf("delete Run value: %w", err)
		}
	}
	owned := []string{
		path,
		filepath.Join(home, "tray-heartbeat.json"),
		state.LauncherPath,
		state.Script,
		filepath.Join(home, "benes-tray.ps1"),
		filepath.Join(home, "benes-tray.vbs"),
	}
	owned = append(owned, wintray.IconPaths(home)...)
	for _, file := range owned {
		file = strings.TrimSpace(file)
		if file == "" {
			continue
		}
		_ = os.Remove(file)
	}
	return true, nil
}

func runTray(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	action := "status"
	if len(args) > 0 {
		action = args[0]
		args = args[1:]
	}
	switch action {
	case "status":
		if len(args) > 0 {
			fmt.Fprintln(stderr, "benes: usage: tray [status|install|start|stop|uninstall]")
			return 2
		}
		return runTrayStatus(stdout, stderr, deps)
	case "install":
		if len(args) > 0 {
			fmt.Fprintln(stderr, "benes: usage: tray install")
			return 2
		}
		return runTrayInstall(stdout, stderr, deps)
	case "start", "stop":
		if len(args) > 0 {
			fmt.Fprintln(stderr, "benes: usage: tray start|stop")
			return 2
		}
		return runTrayMode(action, stdout, stderr, deps)
	case "uninstall", "remove":
		if len(args) > 0 {
			fmt.Fprintln(stderr, "benes: usage: tray uninstall")
			return 2
		}
		ok, summary, err := uninstallWindowsTray(deps)
		if err != nil {
			return serveFailure(stderr, "tray uninstall", err)
		}
		if !ok {
			fmt.Fprintln(stdout, "supported: true")
			fmt.Fprintln(stdout, "installed: false")
			fmt.Fprintln(stdout, "summary: "+summary)
			return 0
		}
		fmt.Fprintln(stdout, "supported: true")
		fmt.Fprintln(stdout, "installed: false")
		fmt.Fprintln(stdout, "summary: "+summary)
		return 0
	default:
		fmt.Fprintln(stderr, "benes: usage: tray [status|install|start|stop|uninstall]")
		return 2
	}
}

func runTrayStatus(stdout, stderr io.Writer, deps commandDependencies) int {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(stdout, "supported: false")
		return 0
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	status := wintray.QueryStatus(paths.Home)
	fmt.Fprintf(stdout, "supported: %t\n", status.Supported)
	fmt.Fprintf(stdout, "installed: %t\n", status.Installed)
	if status.RunValue != "" {
		fmt.Fprintf(stdout, "runValue: %s\n", status.RunValue)
	}
	return 0
}

func runTrayInstall(stdout, stderr io.Writer, deps commandDependencies) int {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(stderr, "benes: tray install is Windows-only")
		return 2
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	codexHome, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve CODEX_HOME", err)
	}
	exe, err := serviceExecutable()
	if err != nil {
		return serveFailure(stderr, "resolve CLI", err)
	}
	status, err := wintray.Install(paths.Home, exe, codexHome)
	if err != nil {
		return serveFailure(stderr, "tray install", err)
	}
	fmt.Fprintf(stdout, "supported: %t\n", status.Supported)
	fmt.Fprintf(stdout, "installed: %t\n", status.Installed)
	fmt.Fprintf(stdout, "summary: %s\n", status.Summary)
	if status.RunValue != "" {
		fmt.Fprintf(stdout, "runValue: %s\n", status.RunValue)
	}
	return 0
}

func runTrayMode(mode string, stdout, stderr io.Writer, deps commandDependencies) int {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(stderr, "benes: tray is Windows-only")
		return 2
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	status := wintray.QueryStatus(paths.Home)
	if !status.Installed {
		fmt.Fprintln(stderr, "benes: the tray is not installed. Run benes tray install.")
		return 1
	}
	exe, err := serviceExecutable()
	if err != nil {
		return serveFailure(stderr, "resolve CLI", err)
	}
	psMode := "Run"
	if mode == "stop" {
		psMode = "Stop"
	}
	codexHome, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve CODEX_HOME", err)
	}
	args, err := wintray.ProcessArgs(wintray.Entry{
		CLI:           exe,
		Script:        wintray.ScriptPath(paths.Home),
		CodexHome:     codexHome,
		BenesHome: paths.Home,
	}, psMode, 0)
	if err != nil {
		return serveFailure(stderr, "tray "+mode, err)
	}
	cmd := exec.Command(wintray.PowerShellPath(), args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return serveFailure(stderr, "tray "+mode, err)
	}
	if mode == "stop" {
		_ = cmd.Wait()
	}
	fmt.Fprintf(stdout, "supported: true\ninstalled: true\nsummary: %s requested\n", mode)
	return 0
}
