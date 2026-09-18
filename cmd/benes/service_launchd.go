package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

const launchdLabel = "com.benes.proxy"

var queryLaunchctl = queryLaunchctlCmd
var launchdPlistPathFn = launchdPlistPath

func queryLaunchctlCmd(args []string) (string, error) {
	cmd := exec.Command("launchctl", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func launchdPlistPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("Library", "LaunchAgents", launchdLabel+".plist")
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
}

func posixJoin(home, file string) string {
	home = strings.ReplaceAll(strings.TrimSpace(home), "\\", "/")
	home = strings.TrimRight(home, "/")
	file = strings.Trim(strings.ReplaceAll(file, "\\", "/"), "/")
	if home == "" {
		return file
	}
	return home + "/" + file
}

func buildLaunchdPlist(exe, home string) string {
	logPath := posixJoin(home, "service.log")
	path := strings.TrimSpace(os.Getenv("PATH"))
	if path == "" {
		path = "/usr/local/bin:/usr/bin:/bin"
	}
	env := "    <key>BENES_SERVICE</key>\n    <string>1</string>\n" +
		"    <key>BENES_HOME</key>\n    <string>" + xmlEscape(home) + "</string>\n" +
		"    <key>PATH</key>\n    <string>" + xmlEscape(path) + "</string>"
	if v := strings.TrimSpace(os.Getenv("CODEX_HOME")); v != "" {
		env += "\n    <key>CODEX_HOME</key>\n    <string>" + xmlEscape(v) + "</string>"
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + xmlEscape(launchdLabel) + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + xmlEscape(exe) + `</string>
    <string>start</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>EnvironmentVariables</key>
  <dict>
` + env + `
  </dict>
  <key>StandardOutPath</key>
  <string>` + xmlEscape(logPath) + `</string>
  <key>StandardErrorPath</key>
  <string>` + xmlEscape(logPath) + `</string>
</dict>
</plist>
`
}

func launchctlLoadFailed(out string) bool {
	return strings.Contains(strings.ToLower(out), "load failed")
}

func startLaunchdJob() error {
	plist := launchdPlistPathFn()
	out, err := queryLaunchctl([]string{"load", "-w", plist})
	if err == nil && !launchctlLoadFailed(out) {
		return nil
	}
	listed, listErr := queryLaunchctl([]string{"list"})
	if listErr == nil && strings.Contains(listed, launchdLabel) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("launchctl load reported failure")
	}
	return fmt.Errorf("%s", out)
}

func stopLaunchdIfInstalled() {
	plist := launchdPlistPathFn()
	if _, err := os.Stat(plist); err != nil {
		return
	}
	_, _ = queryLaunchctl([]string{"unload", plist})
}

func probeLaunchd() schedulerTaskProbe {
	if _, err := os.Stat(launchdPlistPathFn()); err == nil {
		return schedulerTaskProbe{Status: schedulerTaskPresent}
	}
	out, err := queryLaunchctl([]string{"list"})
	if err != nil {
		return schedulerTaskProbe{Status: schedulerTaskUnknown, Detail: err.Error()}
	}
	if strings.Contains(out, launchdLabel) {
		return schedulerTaskProbe{Status: schedulerTaskPresent}
	}
	return schedulerTaskProbe{Status: schedulerTaskAbsent}
}

func runServiceInstallDarwin(stdout, stderr io.Writer, deps commandDependencies) int {
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
	plist := launchdPlistPathFn()
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		fmt.Fprintf(stderr, "benes: create LaunchAgents dir: %v\n", err)
		return 1
	}
	if err := os.WriteFile(plist, []byte(buildLaunchdPlist(exe, paths.Home)), 0o644); err != nil {
		fmt.Fprintf(stderr, "benes: write launchd plist: %v\n", err)
		return 1
	}
	_, _ = queryLaunchctl([]string{"unload", plist})
	if err := startLaunchdJob(); err != nil {
		fmt.Fprintf(stderr, "benes: launchctl load: %v\n", err)
		return 1
	}
	state, _ := json.Marshal(map[string]any{"version": 2, "backend": "launchd"})
	if err := publishServiceFile(context.Background(), paths.Home, "service-state.json", append(state, '\n')); err != nil {
		fmt.Fprintf(stderr, "benes: write service state: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "service installed.")
	return runServiceStart(stdout, stderr, deps)
}
