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

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/winsw"
)

var (
	queryWinswStatus = queryWinswStatusCmd
	queryServiceQC   = queryServiceQCCmd
	runWinsw         = runWinswCmd
	runWinswPrompt   = runWinswPromptCmd
)

func runServiceInstallNative(stdout, stderr io.Writer, deps commandDependencies) int {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(stderr, "benes: service install --native is Windows-only")
		return 1
	}
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
	if err := os.MkdirAll(winsw.Dir(paths.Home), 0o755); err != nil {
		fmt.Fprintf(stderr, "benes: create winsw dir: %v\n", err)
		return 1
	}
	xml := winsw.BuildXML(exe, paths.Home, 23100, envMap())
	if err := publishServiceFile(context.Background(), paths.Home, filepath.Join("winsw", winsw.ServiceID+".xml"), []byte(xml)); err != nil {
		fmt.Fprintf(stderr, "benes: write winsw xml: %v\n", err)
		return 1
	}
	if !winsw.MustExist(winsw.ExePath(paths.Home)) {
		fmt.Fprintf(stderr, "benes: WinSW binary missing at %s. Place the official WinSW.NET461.exe v%s there and retry.\n", winsw.ExePath(paths.Home), winsw.Version)
		return 1
	}
	status := queryWinswStatus(paths.Home)
	if status == winsw.StatusUnknown {
		fmt.Fprintln(stderr, "benes: could not query native service state; refusing to guess")
		return 1
	}
	if status == winsw.StatusNonexistent {
		if err := runWinswPrompt(paths.Home, []string{"install", "/p"}); err != nil {
			fmt.Fprintf(stderr, "benes: winsw install: %v\n", err)
			return 1
		}
		qc, err := queryServiceQC()
		if err != nil {
			fmt.Fprintf(stderr, "benes: sc qc: %v\n", err)
			return 1
		}
		if winsw.LocalSystemStartName(qc) || !winsw.StartNameMatchesUser(qc, os.Getenv("USERNAME")) {
			_, _ = runWinsw(paths.Home, []string{"uninstall"})
			fmt.Fprintln(stderr, "benes: native service registered as LocalSystem or unexpected account; rolled back")
			return 1
		}
	} else {
		_, _ = runWinsw(paths.Home, []string{"stopwait"})
	}
	if _, err := runWinsw(paths.Home, []string{"start"}); err != nil {
		fmt.Fprintf(stderr, "benes: winsw start: %v\n", err)
		return 1
	}
	state, _ := json.Marshal(map[string]any{"version": 2, "backend": "native"})
	if err := publishServiceFile(context.Background(), paths.Home, "service-state.json", append(state, '\n')); err != nil {
		fmt.Fprintf(stderr, "benes: write service state: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "native service installed.")
	return 0
}

func runServiceNativeStart(stderr io.Writer, home string) int {
	if _, err := runWinsw(home, []string{"start"}); err != nil {
		fmt.Fprintf(stderr, "benes: winsw start: %v\n", err)
		return 1
	}
	return 0
}

func runServiceNativeStop(stderr io.Writer, home string) int {
	_, _ = runWinsw(home, []string{"stopwait"})
	status := queryWinswStatus(home)
	if status == winsw.StatusStopped || status == winsw.StatusNonexistent {
		return 0
	}
	if status == winsw.StatusUnknown {
		fmt.Fprintln(stderr, "benes: native service stop could not be verified")
		return 1
	}
	fmt.Fprintln(stderr, "benes: native service is still running after stop")
	return 1
}

func runServiceNativeUninstall(stderr io.Writer, home string) int {
	if !winsw.MustExist(winsw.ExePath(home)) {
		fmt.Fprintln(stderr, "benes: WinSW binary missing; refusing to guess SCM state")
		return 1
	}
	_, _ = runWinsw(home, []string{"stopwait"})
	if _, err := runWinsw(home, []string{"uninstall"}); err != nil && !strings.Contains(strings.ToLower(err.Error()), "nonexistent") {
		fmt.Fprintf(stderr, "benes: winsw uninstall: %v\n", err)
		return 1
	}
	return 0
}

func nativeBackend(home string) bool {
	raw, err := os.ReadFile(filepath.Join(home, "service-state.json"))
	if err != nil {
		return false
	}
	var state map[string]any
	if json.Unmarshal(raw, &state) != nil {
		return false
	}
	backend, _ := state["backend"].(string)
	return strings.EqualFold(strings.TrimSpace(backend), "native")
}

func envMap() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			out[k] = v
		}
	}
	return out
}

func queryWinswStatusCmd(home string) winsw.Status {
	out, err := runWinsw(home, []string{"status"})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "nonexistent") {
			return winsw.StatusNonexistent
		}
		return winsw.StatusUnknown
	}
	return winsw.ParseStatus(out)
}

func runWinswCmd(home string, args []string) (string, error) {
	cmd := exec.Command(winsw.ExePath(home), args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func runWinswPromptCmd(home string, args []string) error {
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return fmt.Errorf("WinSW install requires an interactive console")
	}
	cmd := exec.Command(winsw.ExePath(home), args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func queryServiceQCCmd() (string, error) {
	sc := filepath.Join(os.Getenv("SystemRoot"), "System32", "sc.exe")
	if sc == string(filepath.Separator)+"System32"+string(filepath.Separator)+"sc.exe" {
		sc = "sc.exe"
	}
	cmd := exec.Command(sc, "qc", winsw.ServiceID)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
