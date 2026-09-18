package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Wibias/Benes/internal/codexrestore"
	"github.com/Wibias/Benes/internal/config"
)

func runRestore(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	jsonOut := false
	back := false
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] == "--json":
		jsonOut = true
	case len(args) == 1 && args[0] == "back":
		back = true
	default:
		fmt.Fprintln(stderr, "benes: usage: restore [--json|back]")
		return 2
	}
	if back {
		return runRestoreBack(stdout, stderr, deps)
	}
	home, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve CODEX_HOME: %v\n", err)
		return 1
	}
	result, err := codexrestore.Restore(home)
	if err != nil {
		fmt.Fprintf(stderr, "benes: restore: %v\n", err)
		return 1
	}
	if jsonOut {
		_ = json.NewEncoder(stdout).Encode(map[string]any{
			"success":         true,
			"message":         result.Message,
			"configRestored":  result.ConfigRestored,
			"profileRestored": result.ProfileRestored,
			"stripped":        result.Stripped,
		})
		return 0
	}
	fmt.Fprintln(stdout, result.Message)
	return 0
}

func runRestoreBack(stdout, stderr io.Writer, deps commandDependencies) int {
	resolve := deps.resolvePaths
	if resolve == nil {
		resolve = config.ResolvePaths
	}
	paths, err := resolve(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve paths: %v\n", err)
		return 1
	}
	state, err := readRuntimePortState(paths.RuntimePort)
	if err != nil || state.PID <= 0 || state.Port <= 0 {
		fmt.Fprintln(stderr, "benes: no running proxy found. Run 'benes start'.")
		return 1
	}
	home, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve CODEX_HOME: %v\n", err)
		return 1
	}
	host := strings.TrimSpace(state.Hostname)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	result, err := codexrestore.Inject(home, fmt.Sprintf("http://%s:%d/v1", host, state.Port))
	if err != nil {
		fmt.Fprintf(stderr, "benes: restore back: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, result.Message)
	return 0
}
