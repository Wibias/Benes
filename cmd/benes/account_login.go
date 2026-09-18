package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

func isCodexLoginName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "openai", "codex", "chatgpt":
		return true
	default:
		return false
	}
}

func runAccountLogin(verb string, args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	args, noWait := takeConfigFlag(args, "--no-wait")
	args, reauth := takeConfigFlag(args, "--reauth")
	if verb == "reauth" {
		reauth = true
	}
	id := ""
	code := ""
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--id":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes: usage: account login <provider> [--id <account-id>] [--reauth] [--code -] [--no-wait] [--json]")
				return 2
			}
			i++
			id = args[i]
		case "--code":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes: --code requires a value or -")
				return 2
			}
			i++
			code = args[i]
		default:
			filtered = append(filtered, args[i])
		}
	}
	if len(filtered) != 1 {
		fmt.Fprintln(stderr, "benes: usage: account login <provider> [--id <account-id>] [--reauth] [--code -] [--no-wait] [--json]")
		return 2
	}
	name := strings.ToLower(strings.TrimSpace(filtered[0]))
	if reauth && id == "" && isCodexLoginName(name) {
		fmt.Fprintln(stderr, "benes: --id is required for Codex reauth")
		return 2
	}
	if isCodexLoginName(name) {
		body := map[string]any{"reauth": reauth}
		if id != "" {
			body["id"] = id
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return serveFailure(stderr, "encode login request", err)
		}
		status, raw, fail := accountLiveAccess("POST", "/api/codex-auth/login", payload, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 200 {
			fmt.Fprintf(stderr, "benes: login failed: HTTP %d\n", status)
			return 1
		}
		var start struct {
			URL          string `json:"url"`
			FlowID       string `json:"flowId"`
			Instructions string `json:"instructions"`
		}
		if json.Unmarshal(raw, &start) != nil || start.FlowID == "" {
			fmt.Fprintln(stderr, "benes: login did not return a flow id")
			return 1
		}
		if !jsonOut {
			if start.URL != "" {
				fmt.Fprintf(stdout, "Open this URL to sign in:\n%s\n", start.URL)
			}
			if start.Instructions != "" {
				fmt.Fprintln(stdout, start.Instructions)
			}
			fmt.Fprintf(stdout, "Flow: %s\n", start.FlowID)
		}
		if code != "" && code != "-" {
			fmt.Fprintln(stderr, "warning: the authorization code was passed as a command-line argument")
			if submit := runAccountCode([]string{name, "--flow", start.FlowID, "--code", code}, jsonOut, stdout, stderr, deps); submit != 0 {
				return submit
			}
		}
		if noWait {
			if jsonOut {
				return encodeJSON(stdout, stderr, start)
			}
			return 0
		}
		return pollCodexLogin(start.FlowID, jsonOut, stdout, stderr, deps)
	}
	kind, codeKind := accountProviderKind(name, stderr, deps)
	if codeKind != 0 {
		return codeKind
	}
	if kind != "oauth" {
		fmt.Fprintf(stderr, "benes: account login is not supported for %s\n", name)
		return 2
	}
	payload, err := json.Marshal(map[string]any{"provider": name, "addAccount": !reauth, "reauth": reauth, "accountId": id})
	if err != nil {
		return serveFailure(stderr, "encode login request", err)
	}
	status, raw, fail := accountLiveAccess("POST", "/api/oauth/login", payload, stderr, deps)
	if fail != 0 {
		return fail
	}
	if status != 200 {
		fmt.Fprintf(stderr, "benes: login failed: HTTP %d\n", status)
		return 1
	}
	var start struct {
		URL      string `json:"url"`
		UserCode string `json:"userCode"`
	}
	_ = json.Unmarshal(raw, &start)
	if !jsonOut {
		if start.URL != "" {
			fmt.Fprintf(stdout, "Open this URL to sign in:\n%s\n", start.URL)
		}
		if start.UserCode != "" {
			fmt.Fprintf(stdout, "Device code: %s\n", start.UserCode)
		}
	}
	if noWait {
		if jsonOut {
			return encodeJSON(stdout, stderr, start)
		}
		return 0
	}
	for attempt := 0; attempt < 100; attempt++ {
		time.Sleep(2 * time.Second)
		status, payload, fail := accountLiveAccess("GET", "/api/oauth/status?provider="+name, nil, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 200 {
			continue
		}
		var state struct {
			LoggedIn bool `json:"loggedIn"`
		}
		if json.Unmarshal(payload, &state) == nil && state.LoggedIn {
			if jsonOut {
				return encodeJSON(stdout, stderr, state)
			}
			fmt.Fprintf(stdout, "Logged in to %s.\n", name)
			return 0
		}
	}
	fmt.Fprintln(stderr, "benes: login timed out")
	return 1
}

func runAccountCode(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	flowID := ""
	code := ""
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--flow":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes: --flow requires a flow id")
				return 2
			}
			i++
			flowID = args[i]
		case "--code":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes: --code requires a value")
				return 2
			}
			i++
			code = args[i]
		default:
			filtered = append(filtered, args[i])
		}
	}
	if len(filtered) < 1 {
		fmt.Fprintln(stderr, "benes: usage: account code <provider> [--flow <flow-id>] [--json]")
		return 2
	}
	name := strings.ToLower(strings.TrimSpace(filtered[0]))
	if len(filtered) > 1 && code == "" {
		code = filtered[1]
	}
	if code == "" {
		fmt.Fprintln(stderr, "benes: provider and redirect/code are required")
		return 2
	}
	if isCodexLoginName(name) {
		if flowID == "" {
			fmt.Fprintln(stderr, "benes: Codex login code requires --flow <flow-id>")
			return 2
		}
		payload, err := json.Marshal(map[string]string{"flowId": flowID, "input": code})
		if err != nil {
			return serveFailure(stderr, "encode login code", err)
		}
		status, _, fail := accountLiveAccess("POST", "/api/codex-auth/login/code", payload, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 202 && status != 200 {
			fmt.Fprintf(stderr, "benes: login code failed: HTTP %d\n", status)
			return 1
		}
		if jsonOut {
			return encodeJSON(stdout, stderr, map[string]any{"ok": true})
		}
		fmt.Fprintln(stdout, "Login code submitted.")
		return 0
	}
	payload, err := json.Marshal(map[string]string{"provider": name, "input": code})
	if err != nil {
		return serveFailure(stderr, "encode login code", err)
	}
	status, _, fail := accountLiveAccess("POST", "/api/oauth/login/code", payload, stderr, deps)
	if fail != 0 {
		return fail
	}
	if status != 200 {
		fmt.Fprintf(stderr, "benes: login code failed: HTTP %d\n", status)
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"ok": true})
	}
	fmt.Fprintln(stdout, "Login code submitted.")
	return 0
}

func runAccountCancel(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	flowID := ""
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--flow" {
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "benes: --flow requires a flow id")
				return 2
			}
			i++
			flowID = args[i]
			continue
		}
		filtered = append(filtered, args[i])
	}
	if len(filtered) != 1 {
		fmt.Fprintln(stderr, "benes: usage: account cancel <provider> [--flow <flow-id>] [--json]")
		return 2
	}
	name := strings.ToLower(strings.TrimSpace(filtered[0]))
	path := "/api/oauth/login/cancel"
	var payload []byte
	var err error
	if isCodexLoginName(name) {
		path = "/api/codex-auth/login/cancel"
		payload, err = json.Marshal(map[string]string{"flowId": flowID})
	} else {
		payload, err = json.Marshal(map[string]string{"provider": name})
	}
	if err != nil {
		return serveFailure(stderr, "encode cancel", err)
	}
	status, _, fail := accountLiveAccess("POST", path, payload, stderr, deps)
	if fail != 0 {
		return fail
	}
	if status != 200 {
		fmt.Fprintf(stderr, "benes: cancel failed: HTTP %d\n", status)
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"ok": true, "provider": name})
	}
	fmt.Fprintf(stdout, "Cancelled %s login.\n", name)
	return 0
}

func pollCodexLogin(flowID string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	for attempt := 0; attempt < 150; attempt++ {
		time.Sleep(2 * time.Second)
		status, payload, fail := accountLiveAccess("GET", "/api/codex-auth/login-status?flowId="+flowID, nil, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 200 {
			continue
		}
		var state struct {
			Status string `json:"status"`
			Email  string `json:"email"`
			Error  string `json:"error"`
		}
		if json.Unmarshal(payload, &state) != nil {
			continue
		}
		if state.Status == "done" {
			if jsonOut {
				return encodeJSON(stdout, stderr, state)
			}
			if state.Email != "" {
				fmt.Fprintf(stdout, "Logged in as %s.\n", state.Email)
				return 0
			}
			fmt.Fprintln(stdout, "Logged in.")
			return 0
		}
		if state.Status == "error" || state.Status == "expired" {
			fmt.Fprintf(stderr, "benes: login %s\n", strings.TrimSpace(state.Error+" "+state.Status))
			return 1
		}
	}
	fmt.Fprintln(stderr, "benes: login timed out")
	return 1
}
