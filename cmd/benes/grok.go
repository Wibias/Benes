package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wibias/Benes/internal/grok"
)

const grokUsage = "benes: usage: grok status [--json]\n" +
	"             grok apply [--json]"

func runIntegration(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "benes: usage: integration grok|claude|client ...")
		return 2
	}
	switch args[0] {
	case "grok":
		return runGrok(args[1:], stdout, stderr, deps)
	case "claude":
		return runClaudeConfig(args[1:], stdout, stderr, deps)
	case "client":
		return runClientIntegration(args[1:], stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: integration grok|claude|client ...")
		return 2
	}
}

// runGrok drives the Grok projection the running listener publishes.
//
// There is no Grok-side model list to edit here, and deliberately so: the managed block is a
// projection of the catalogue that listener routes, so both subcommands ask the listener for
// the answer instead of keeping a second copy of it on disk.
func runGrok(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	action := "status"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
		args = args[1:]
	}
	switch action {
	case "status", "show":
		if len(args) > 0 {
			fmt.Fprintln(stderr, grokUsage)
			return 2
		}
		return runGrokStatus(jsonOut, stdout, stderr, deps)
	case "apply":
		if len(args) > 0 {
			fmt.Fprintln(stderr, grokUsage)
			return 2
		}
		return runGrokApply(jsonOut, stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, grokUsage)
		return 2
	}
}

func runGrokStatus(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	code, payload, fail := accountLiveAccess(http.MethodGet, "/api/grok", nil, stderr, deps)
	if fail != 0 {
		return fail
	}
	var registration struct {
		grok.Registration
		// Diagnostic process state, not part of the derived summary.
		LastAutomaticError string `json:"lastAutomaticError,omitempty"`
	}
	if err := json.Unmarshal(payload, &registration); err != nil {
		fmt.Fprintf(stderr, "benes: grok status returned an unexpected payload: %v\n", err)
		return 1
	}
	if code < 200 || code >= 300 {
		fmt.Fprintf(stderr, "benes: grok status failed: HTTP %d\n", code)
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, registration)
	}
	fmt.Fprintf(stdout, "Grok config: %s\n", registration.ConfigPath)
	fmt.Fprintf(stdout, "Registered models: %d of %d\n", registration.Registered, registration.Catalogue)
	switch {
	case registration.Current:
		fmt.Fprintln(stdout, "Configuration: up to date")
	case !registration.Present:
		fmt.Fprintln(stdout, "Configuration: no managed block written yet; run 'benes grok apply'")
	default:
		fmt.Fprintln(stdout, "Configuration: does not match the Benes catalogue; re-apply the Grok Harness or run 'benes grok apply'")
	}
	if msg := strings.TrimSpace(registration.LastAutomaticError); msg != "" {
		fmt.Fprintf(stdout, "Last automatic apply: %s\n", msg)
	}
	return 0
}

func runGrokApply(jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	code, payload, fail := accountLiveAccess(http.MethodPost, "/api/grok/apply", nil, stderr, deps)
	if fail != 0 {
		return fail
	}
	var result struct {
		OK            bool   `json:"ok"`
		Changed       bool   `json:"changed"`
		Message       string `json:"message"`
		SkippedReason string `json:"skippedReason"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		fmt.Fprintf(stderr, "benes: grok apply returned an unexpected payload: %v\n", err)
		return 1
	}
	if code < 200 || code >= 300 || !result.OK {
		message := strings.TrimSpace(result.Message)
		if message == "" {
			message = fmt.Sprintf("HTTP %d", code)
		}
		fmt.Fprintf(stderr, "benes: %s\n", message)
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, result)
	}
	fmt.Fprintln(stdout, result.Message)
	return 0
}
