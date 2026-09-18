package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/codexrouting"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/runtimestate"
)

type runtimePortState struct {
	PID      int    `json:"pid"`
	Port     int    `json:"port"`
	Hostname string `json:"hostname"`
	Exe      string `json:"exe,omitempty"`
}

type codexRoutingStatus struct {
	Route   string
	BaseURL string
	Managed bool
}

func runStatus(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	jsonOut := false
	if len(args) == 1 && args[0] == "--json" {
		jsonOut = true
	} else if len(args) > 0 {
		fmt.Fprintln(stderr, "benes: usage: status [--json]")
		return 2
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve paths: %v\n", err)
		return 1
	}
	codex := inspectCodexRouting()
	state, err := readRuntimePortState(paths.RuntimePort)
	verdict := evaluateRuntime(state, err, deps)
	if !verdict.Running() {
		if jsonOut {
			_ = json.NewEncoder(stdout).Encode(statusJSON(false, runtimePortState{}, "", codex))
			return 0
		}
		if verdict.Class == runtimestate.ClassReused {
			fmt.Fprintf(stdout, "No running proxy found. Stale runtime PID %d belongs to another process.\n", state.PID)
			writeCodexStatusText(stdout, codex)
			return 0
		}
		fmt.Fprintln(stdout, "No running proxy found.")
		writeCodexStatusText(stdout, codex)
		return 0
	}
	host := strings.TrimSpace(state.Hostname)
	if host == "" {
		host = "127.0.0.1"
	}
	if jsonOut {
		_ = json.NewEncoder(stdout).Encode(statusJSON(true, state, host, codex))
		return 0
	}
	fmt.Fprintf(stdout, "Proxy running (PID %d, port %d, host %s)\n", state.PID, state.Port, host)
	writeCodexStatusText(stdout, codex)
	return 0
}

func statusJSON(running bool, state runtimePortState, host string, codex codexRoutingStatus) map[string]any {
	out := map[string]any{
		"running":      running,
		"codexRoute":   codex.Route,
		"codexManaged": codex.Managed,
	}
	if running {
		out["pid"] = state.PID
		out["port"] = state.Port
		out["hostname"] = host
	}
	if codex.BaseURL != "" {
		out["codexBaseUrl"] = codex.BaseURL
	}
	return out
}

func writeCodexStatusText(stdout io.Writer, codex codexRoutingStatus) {
	fmt.Fprintf(stdout, "Codex route: %s\n", codex.Route)
	if codex.BaseURL != "" {
		fmt.Fprintf(stdout, "Codex base URL: %s\n", codex.BaseURL)
	}
}

func inspectCodexRouting() codexRoutingStatus {
	out := codexRoutingStatus{Route: "unknown"}
	home, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err != nil {
		return out
	}
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		return out
	}
	content := string(raw)
	out.Route = codexrouting.PublicRoute(codexrouting.Classify(content))
	out.Managed = strings.Contains(content, codexrouting.Marker)
	out.BaseURL = safeBaseURL(codexrouting.EffectiveBaseURL(content))
	return out
}

func safeBaseURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return raw
}

func readRuntimePortState(path string) (runtimePortState, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return runtimePortState{}, err
	}
	var state runtimePortState
	if err := json.Unmarshal(body, &state); err != nil {
		return runtimePortState{}, err
	}
	return state, nil
}
