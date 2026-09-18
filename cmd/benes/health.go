package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/Wibias/Benes/internal/config"
)

func runHealth(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	jsonOut := false
	if len(args) == 1 && args[0] == "--json" {
		jsonOut = true
	} else if len(args) > 0 {
		fmt.Fprintln(stderr, "benes: usage: health [--json]")
		return 2
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve paths: %v\n", err)
		return 1
	}
	state, err := readRuntimePortState(paths.RuntimePort)
	verdict := evaluateRuntime(state, err, deps)
	ok := verdict.Running()
	if jsonOut {
		payload := map[string]any{"ok": ok, "pid": nil, "port": nil}
		if ok {
			payload["pid"] = state.PID
			payload["port"] = state.Port
		}
		_ = json.NewEncoder(stdout).Encode(payload)
		if ok {
			return 0
		}
		return 1
	}
	if ok {
		fmt.Fprintf(stdout, "Proxy healthy (PID %d, port %d)\n", state.PID, state.Port)
		return 0
	}
	fmt.Fprintln(stdout, "Proxy not healthy")
	return 1
}
