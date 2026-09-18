package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
)

const clientIntegrationUsage = `benes: usage: integration client [status] [--client <id>] [--json]
             integration client <enable|disable> --client <id> [--json]
             integration client history [--client <id>] [--json]
             integration client restore --op <opId> [--confirm-drift] [--json]`

func runClientIntegration(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	action := "status"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
		args = args[1:]
	}
	if err := ensureLiveProxy(stderr, deps); err != nil {
		return 1
	}
	switch action {
	case "status", "show", "list":
		client, args := takeOption(args, "--client")
		if len(args) > 0 {
			fmt.Fprintln(stderr, clientIntegrationUsage)
			return 2
		}
		path := "/api/client-integrations"
		if client != "" {
			path += "/" + url.PathEscape(client)
		}
		status, payload, fail := accountLiveAccess("GET", path, nil, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 200 {
			fmt.Fprintf(stderr, "benes: integration status failed: HTTP %d\n", status)
			return 1
		}
		if jsonOut {
			fmt.Fprintln(stdout, strings.TrimSpace(string(payload)))
			return 0
		}
		var envelope struct {
			ClientID  string `json:"clientId"`
			State     string `json:"state"`
			Installed bool   `json:"installed"`
			Clients   []struct {
				ClientID  string `json:"clientId"`
				State     string `json:"state"`
				Installed bool   `json:"installed"`
			} `json:"clients"`
		}
		if json.Unmarshal(payload, &envelope) != nil {
			fmt.Fprintln(stderr, "benes: unexpected integration status payload")
			return 1
		}
		if envelope.ClientID != "" {
			suffix := ""
			if !envelope.Installed {
				suffix = " (not installed)"
			}
			fmt.Fprintf(stdout, "%s: %s%s\n", envelope.ClientID, envelope.State, suffix)
			return 0
		}
		for _, row := range envelope.Clients {
			suffix := ""
			if !row.Installed {
				suffix = " (not installed)"
			}
			fmt.Fprintf(stdout, "%s: %s%s\n", row.ClientID, row.State, suffix)
		}
		return 0
	case "history", "journal":
		client, args := takeOption(args, "--client")
		if len(args) > 0 {
			fmt.Fprintln(stderr, clientIntegrationUsage)
			return 2
		}
		path := "/api/client-integrations/journal"
		if client != "" {
			path += "?client=" + url.QueryEscape(client)
		}
		status, payload, fail := accountLiveAccess("GET", path, nil, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 200 {
			fmt.Fprintf(stderr, "benes: integration history failed: HTTP %d\n", status)
			return 1
		}
		if jsonOut {
			fmt.Fprintln(stdout, strings.TrimSpace(string(payload)))
			return 0
		}
		var envelope struct {
			Operations []struct {
				OpID     string `json:"opId"`
				ClientID string `json:"clientId"`
				Kind     string `json:"kind"`
				At       string `json:"at"`
				Snapshot string `json:"snapshot"`
			} `json:"operations"`
		}
		_ = json.Unmarshal(payload, &envelope)
		if len(envelope.Operations) == 0 {
			fmt.Fprintln(stdout, "No integration operations recorded yet.")
			return 0
		}
		for _, row := range envelope.Operations {
			backup := "op " + row.OpID
			if row.Snapshot == "expired" {
				backup = "backup expired"
			}
			fmt.Fprintf(stdout, "%s  %s  %s  (%s)\n", row.At, row.ClientID, row.Kind, backup)
		}
		return 0
	case "restore":
		opID, args := takeOption(args, "--op")
		if opID == "" {
			opID, args = takeOption(args, "--op-id")
		}
		args, confirm := takeConfigFlag(args, "--confirm-drift")
		if len(args) > 0 || opID == "" {
			fmt.Fprintln(stderr, clientIntegrationUsage)
			return 2
		}
		body, err := json.Marshal(map[string]any{"opId": opID, "confirmDrift": confirm})
		if err != nil {
			return serveFailure(stderr, "encode restore request", err)
		}
		status, payload, fail := accountLiveAccess("POST", "/api/client-integrations/restore", body, stderr, deps)
		if fail != 0 {
			return fail
		}
		return printIntegrationMutation(jsonOut, status, payload, stdout, stderr)
	case "enable", "disable":
		client, args := takeOption(args, "--client")
		if len(args) > 0 || client == "" {
			fmt.Fprintln(stderr, clientIntegrationUsage)
			return 2
		}
		body, err := json.Marshal(map[string]any{"enabled": action == "enable"})
		if err != nil {
			return serveFailure(stderr, "encode toggle request", err)
		}
		status, payload, fail := accountLiveAccess("PUT", "/api/client-integrations/"+url.PathEscape(client), body, stderr, deps)
		if fail != 0 {
			return fail
		}
		return printIntegrationMutation(jsonOut, status, payload, stdout, stderr)
	default:
		fmt.Fprintln(stderr, clientIntegrationUsage)
		return 2
	}
}

func printIntegrationMutation(jsonOut bool, status int, payload []byte, stdout, stderr io.Writer) int {
	if status < 200 || status >= 300 {
		var envelope struct {
			Message string `json:"message"`
			Error   string `json:"error"`
		}
		_ = json.Unmarshal(payload, &envelope)
		msg := envelope.Message
		if msg == "" {
			msg = envelope.Error
		}
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", status)
		}
		fmt.Fprintf(stderr, "benes: %s\n", msg)
		return 1
	}
	if jsonOut {
		fmt.Fprintln(stdout, strings.TrimSpace(string(payload)))
		return 0
	}
	var envelope struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(payload, &envelope)
	if envelope.Message == "" {
		envelope.Message = "ok"
	}
	fmt.Fprintln(stdout, envelope.Message)
	return 0
}
