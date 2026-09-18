package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func runAccountRefresh(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: account refresh <provider> [--json]")
		return 2
	}
	name := strings.TrimSpace(args[0])
	if _, code := accountProviderOrOpenAI(name, stderr, deps); code != 0 {
		return code
	}
	status, payload, fail := accountLiveAccess("GET", "/api/provider-quotas?refresh=1", nil, stderr, deps)
	if fail != 0 {
		return fail
	}
	if status != 200 {
		fmt.Fprintf(stderr, "benes: failed to refresh %s: HTTP %d\n", name, status)
		return 1
	}
	var envelope struct {
		Reports []json.RawMessage `json:"reports"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		fmt.Fprintln(stderr, "benes: unexpected provider quota payload")
		return 1
	}
	var report json.RawMessage
	for _, raw := range envelope.Reports {
		var row struct {
			Provider string `json:"provider"`
		}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if row.Provider == name {
			report = raw
			break
		}
	}
	if jsonOut {
		out := map[string]any{"provider": name, "report": nil}
		if len(report) > 0 {
			out["report"] = report
		}
		return encodeJSON(stdout, stderr, out)
	}
	if len(report) == 0 {
		fmt.Fprintf(stdout, "no quota report available for %s\n", name)
		return 0
	}
	fmt.Fprintln(stdout, formatProviderQuotaLine(name, report))
	return 0
}

func formatProviderQuotaLine(name string, raw json.RawMessage) string {
	var report struct {
		Quota struct {
			FiveHourPercent *float64 `json:"fiveHourPercent"`
			WeeklyPercent   *float64 `json:"weeklyPercent"`
			MonthlyPercent  *float64 `json:"monthlyPercent"`
			CustomWindows   []struct {
				Label   string  `json:"label"`
				Percent float64 `json:"percent"`
			} `json:"customWindows"`
		} `json:"quota"`
	}
	if json.Unmarshal(raw, &report) != nil {
		return name
	}
	parts := []string{name}
	add := func(label string, percent *float64) {
		if percent == nil {
			return
		}
		parts = append(parts, fmt.Sprintf("%s %.0f%%", label, *percent))
	}
	add("5h", report.Quota.FiveHourPercent)
	add("weekly", report.Quota.WeeklyPercent)
	add("monthly", report.Quota.MonthlyPercent)
	for _, win := range report.Quota.CustomWindows {
		parts = append(parts, fmt.Sprintf("%s %.0f%%", win.Label, win.Percent))
	}
	return strings.Join(parts, " ")
}

func runAccountAutoSwitch(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) < 2 {
		fmt.Fprintln(stderr, "benes: usage: account auto-switch <provider> <on|off|status|threshold <0-100>> [--json]")
		return 2
	}
	name := strings.TrimSpace(args[0])
	action := strings.ToLower(strings.TrimSpace(args[1]))
	rest := args[2:]
	if name != "openai" {
		fmt.Fprintln(stderr, "benes: auto-switch only applies to the openai Codex account pool")
		return 2
	}
	var threshold *int
	switch action {
	case "on":
		if len(rest) != 0 {
			fmt.Fprintln(stderr, "benes: usage: account auto-switch <provider> <on|off|status|threshold <0-100>> [--json]")
			return 2
		}
		value := 80
		threshold = &value
	case "off":
		if len(rest) != 0 {
			fmt.Fprintln(stderr, "benes: usage: account auto-switch <provider> <on|off|status|threshold <0-100>> [--json]")
			return 2
		}
		value := 0
		threshold = &value
	case "threshold":
		if len(rest) != 1 {
			fmt.Fprintln(stderr, "benes: usage: account auto-switch <provider> threshold <0-100> [--json]")
			return 2
		}
		n, err := strconv.Atoi(rest[0])
		if err != nil || n < 0 || n > 100 {
			fmt.Fprintln(stderr, "benes: threshold must be an integer 0-100")
			return 2
		}
		threshold = &n
	case "status":
		if len(rest) != 0 {
			fmt.Fprintln(stderr, "benes: usage: account auto-switch <provider> status [--json]")
			return 2
		}
	default:
		fmt.Fprintln(stderr, "benes: usage: account auto-switch <provider> <on|off|status|threshold <0-100>> [--json]")
		return 2
	}
	if threshold != nil {
		body, err := json.Marshal(map[string]int{"threshold": *threshold})
		if err != nil {
			return serveFailure(stderr, "encode auto-switch", err)
		}
		status, _, fail := accountLiveAccess("PUT", "/api/codex-auth/auto-switch", body, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 200 {
			fmt.Fprintln(stderr, "benes: failed to update auto-switch")
			return 1
		}
	} else {
		status, payload, fail := accountLiveAccess("GET", "/api/codex-auth/active", nil, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 200 {
			fmt.Fprintln(stderr, "benes: failed to read auto-switch status")
			return 1
		}
		var body struct {
			AutoSwitchThreshold *int `json:"autoSwitchThreshold"`
		}
		if json.Unmarshal(payload, &body) != nil || body.AutoSwitchThreshold == nil {
			fmt.Fprintln(stderr, "benes: failed to read auto-switch status")
			return 1
		}
		threshold = body.AutoSwitchThreshold
	}
	enabled := *threshold > 0
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"provider": name, "autoSwitchThreshold": *threshold, "enabled": enabled})
	}
	if enabled {
		fmt.Fprintf(stdout, "auto-switch: on (threshold %d%%)\n", *threshold)
		return 0
	}
	fmt.Fprintln(stdout, "auto-switch: off")
	return 0
}

func accountProviderOrOpenAI(name string, stderr io.Writer, deps commandDependencies) (string, int) {
	if name == "openai" {
		return "codex", 0
	}
	return accountProviderKind(name, stderr, deps)
}
