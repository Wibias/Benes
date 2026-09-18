package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Wibias/Benes/internal/config"
)

func runSystem(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	sub := "status"
	if len(args) > 0 {
		sub = strings.ToLower(args[0])
		args = args[1:]
	}
	switch sub {
	case "status":
		if len(args) > 0 {
			fmt.Fprintln(stderr, "benes: usage: system status [--json]")
			return 2
		}
		return runSystemStatus(jsonOut, stdout, stderr, deps)
	case "settings":
		return runSystemSettings(args, jsonOut, stdout, stderr, deps)
	case "startup":
		return runSystemStartup(args, jsonOut, stdout, stderr, deps)
	case "diagnostics":
		if len(args) > 0 {
			fmt.Fprintln(stderr, "benes: usage: system diagnostics [--json]")
			return 2
		}
		return runDoctor(stdout, stderr, deps)
	case "sync":
		if jsonOut {
			fmt.Fprintln(stderr, "benes: system sync --json is not supported")
			return 2
		}
		return runSync(args, stdout, stderr, deps)
	case "update":
		return runUpdate(args, stdout, stderr)
	default:
		fmt.Fprintln(stderr, "benes: usage: system status|settings|startup|diagnostics|sync|update")
		return 2
	}
}

func runSystemStatus(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	settings, err := loadSystemSettings(deps)
	if err != nil {
		return serveFailure(stderr, "load settings", err)
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	state, err := readRuntimePortState(paths.RuntimePort)
	running := evaluateRuntime(state, err, deps).Running()
	host := ""
	if running {
		host = strings.TrimSpace(state.Hostname)
		if host == "" {
			host = "127.0.0.1"
		}
	}
	if jsonOut {
		body := map[string]any{
			"running":  running,
			"settings": settings,
		}
		if running {
			body["pid"] = state.PID
			body["port"] = state.Port
			body["hostname"] = host
		}
		return encodeJSON(stdout, stderr, body)
	}
	if running {
		fmt.Fprintf(stdout, "Proxy running (PID %d, port %d, host %s)\n", state.PID, state.Port, host)
	} else {
		fmt.Fprintln(stdout, "No running proxy found.")
	}
	fmt.Fprintf(stdout, "codexAutoStart: %v\n", settings["codexAutoStart"])
	fmt.Fprintf(stdout, "streamMode: %s\n", settings["streamMode"])
	return 0
}

func runSystemSettings(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	autoStart, args := takeOption(args, "--auto-start")
	streamMode, args := takeOption(args, "--stream-mode")
	if len(args) > 0 {
		fmt.Fprintln(stderr, "benes: usage: system settings [--auto-start on|off] [--stream-mode auto|legacy-tee|eager-relay] [--json]")
		return 2
	}
	if autoStart == "" && streamMode == "" {
		settings, err := loadSystemSettings(deps)
		if err != nil {
			return serveFailure(stderr, "load settings", err)
		}
		if jsonOut {
			return encodeJSON(stdout, stderr, settings)
		}
		fmt.Fprintf(stdout, "codexAutoStart: %v\n", settings["codexAutoStart"])
		fmt.Fprintf(stdout, "streamMode: %s\n", settings["streamMode"])
		return 0
	}
	if autoStart != "" && autoStart != "on" && autoStart != "off" {
		fmt.Fprintln(stderr, "benes: --auto-start must be on or off")
		return 2
	}
	if streamMode != "" && streamMode != "auto" && streamMode != "legacy-tee" && streamMode != "eager-relay" {
		fmt.Fprintln(stderr, "benes: --stream-mode must be auto, legacy-tee, or eager-relay")
		return 2
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		if autoStart != "" {
			encoded, err := json.Marshal(autoStart == "on")
			if err != nil {
				return err
			}
			if err := tx.Set(config.JSONPath("codexAutoStart"), encoded); err != nil {
				return err
			}
		}
		if streamMode != "" {
			encoded, err := json.Marshal(streamMode)
			if err != nil {
				return err
			}
			if err := tx.Set(config.JSONPath("streamMode"), encoded); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return 1
	}
	if jsonOut {
		settings, err := loadSystemSettings(deps)
		if err != nil {
			return serveFailure(stderr, "load settings", err)
		}
		return encodeJSON(stdout, stderr, settings)
	}
	fmt.Fprintln(stdout, "System settings updated.")
	return 0
}

func runSystemStartup(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	action := "health"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
		args = args[1:]
	}
	if len(args) > 0 {
		fmt.Fprintln(stderr, "benes: usage: system startup health|install-service|install-shim [--json]")
		return 2
	}
	switch action {
	case "health", "status":
		if jsonOut {
			return runHealth([]string{"--json"}, stdout, stderr, deps)
		}
		return runHealth(nil, stdout, stderr, deps)
	case "install-service":
		if jsonOut {
			fmt.Fprintln(stderr, "benes: system startup install-service --json is not supported")
			return 2
		}
		return runService([]string{"install"}, stdout, stderr, deps)
	case "install-shim":
		if jsonOut {
			fmt.Fprintln(stderr, "benes: system startup install-shim --json is not supported")
			return 2
		}
		return runCodexShim([]string{"install"}, stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: system startup health|install-service|install-shim")
		return 2
	}
}

func loadSystemSettings(deps commandDependencies) (map[string]any, error) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return nil, err
	}
	autoStart := true
	if raw, ok := root["codexAutoStart"]; ok {
		_ = json.Unmarshal(raw, &autoStart)
	}
	streamMode := "auto"
	if raw, ok := root["streamMode"]; ok {
		var value string
		if json.Unmarshal(raw, &value) == nil && value != "" {
			streamMode = value
		}
	}
	return map[string]any{
		"codexAutoStart": autoStart,
		"streamMode":     streamMode,
	}, nil
}
