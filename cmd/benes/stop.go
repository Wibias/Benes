package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Wibias/Benes/internal/codexrestore"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/grok"
	"github.com/Wibias/Benes/internal/runtimestate"
)

func runStop(stdout, stderr io.Writer, deps commandDependencies) int {
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve paths: %v\n", err)
		return 1
	}
	stopInstalledServiceIfPresent(paths.Home)
	pid, err := readPIDFile(paths.PID)
	state, stateErr := readRuntimePortState(paths.RuntimePort)
	if err != nil || pid <= 0 {
		if stateErr == nil && state.PID > 0 {
			pid = state.PID
		} else {
			fmt.Fprintln(stdout, "No running proxy found.")
			return restoreCodexOnStop(stdout, stderr)
		}
	}
	if pid > 0 {
		state.PID = pid
	}
	verdict := evaluateRuntime(state, nil, deps)
	if verdict.Class == runtimestate.ClassReused {
		fmt.Fprintf(stderr, "benes: runtime PID %d is not this Benes process; not signaling it\n", pid)
		return 1
	}
	if !verdict.CanSignal() {
		clearStopState(paths)
		fmt.Fprintln(stdout, "No running proxy found.")
		return restoreCodexOnStop(stdout, stderr)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(stderr, "benes: find process %d: %v\n", pid, err)
		return 1
	}
	_ = proc.Signal(os.Interrupt)
	time.Sleep(500 * time.Millisecond)
	if err := proc.Kill(); err != nil && !processAlreadyGone(err) {
		fmt.Fprintf(stderr, "benes: stop proxy (PID %d): %v\n", pid, err)
		return 1
	}
	clearStopState(paths)
	fmt.Fprintf(stdout, "Proxy (PID %d) stopped.\n", pid)
	return restoreCodexOnStop(stdout, stderr)
}

func restoreCodexOnStop(stdout, stderr io.Writer) int {
	home, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err == nil {
		result, err := codexrestore.Restore(home)
		if err != nil {
			fmt.Fprintf(stderr, "benes: restore Codex: %v\n", err)
			return 1
		}
		if result.Message != "" {
			fmt.Fprintln(stdout, result.Message)
		}
	}
	stripped := grok.Strip(grok.InjectOptions{})
	if stripped.Changed && stripped.Message != "" {
		fmt.Fprintln(stdout, stripped.Message)
	}
	return 0
}

func readPIDFile(path string) (int, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(body)))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid pid")
	}
	return n, nil
}

func clearStopState(paths config.Paths) {
	if path := strings.TrimSpace(paths.PID); path != "" {
		_ = os.Remove(path)
		_ = os.Remove(filepath.Join(filepath.Dir(path), "benes.pid"))
	}
	if path := strings.TrimSpace(paths.RuntimePort); path != "" {
		_ = os.Remove(path)
	}
}

func processAlreadyGone(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such process") || strings.Contains(msg, "process already finished") || strings.Contains(msg, "the operation completed successfully")
}
