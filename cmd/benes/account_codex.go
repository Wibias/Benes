package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

var accountPriorityPresets = map[string]int{
	"first":   2,
	"earlier": 1,
	"normal":  0,
	"later":   -1,
	"last":    -2,
}

func fetchCodexFamilyRows(name string, stderr io.Writer, deps commandDependencies) ([]accountRow, int) {
	code, payload, err := observeGET("/api/codex-auth/accounts", stderr, deps)
	if err != nil {
		return nil, 1
	}
	if code != 0 {
		return nil, code
	}
	activeCode, activePayload, err := observeGET("/api/codex-auth/active", stderr, deps)
	if err != nil {
		return nil, 1
	}
	if activeCode != 0 {
		return nil, activeCode
	}
	var accounts struct {
		Accounts []struct {
			ID          string  `json:"id"`
			Alias       *string `json:"alias"`
			Email       string  `json:"email"`
			Plan        *string `json:"plan"`
			Priority    int     `json:"priority"`
			NeedsReauth bool    `json:"needsReauth"`
		} `json:"accounts"`
	}
	var active struct {
		ActiveCodexAccountID any `json:"activeCodexAccountId"`
	}
	if json.Unmarshal(payload, &accounts) != nil || json.Unmarshal(activePayload, &active) != nil {
		return nil, 0
	}
	activeID, _ := active.ActiveCodexAccountID.(string)
	rows := make([]accountRow, 0, len(accounts.Accounts))
	for _, account := range accounts.Accounts {
		label := account.Email
		if account.Alias != nil && strings.TrimSpace(*account.Alias) != "" {
			label = *account.Alias
		} else if account.Plan != nil && strings.TrimSpace(*account.Plan) != "" {
			label = *account.Plan
		}
		rows = append(rows, accountRow{
			Provider: name,
			Type:     "codex",
			ID:       account.ID,
			Label:    label,
			Active:   account.ID == activeID,
			Needs:    account.NeedsReauth,
			Priority: account.Priority,
		})
	}
	return rows, 0
}

func runAccountPriority(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) < 2 || len(args) > 3 {
		fmt.Fprintln(stderr, "benes: usage: account priority <provider> <id|main> [first|earlier|normal|later|last|reset|<-100..100>] [--json]")
		return 2
	}
	name := strings.TrimSpace(args[0])
	id := strings.TrimSpace(args[1])
	kind, code := accountProviderKind(name, stderr, deps)
	if code != 0 {
		return code
	}
	if kind != "codex" {
		fmt.Fprintln(stderr, "benes: selection order only applies to the openai Codex account pool")
		return 2
	}
	if id == "main" {
		id = "__main__"
	}
	if len(args) == 2 {
		rows, fail := fetchCodexFamilyRows(name, stderr, deps)
		if fail != 0 {
			return fail
		}
		var found *accountRow
		for i := range rows {
			if rows[i].ID == id {
				found = &rows[i]
				break
			}
		}
		if found == nil {
			fmt.Fprintf(stderr, "benes: no %s account %s\n", name, args[1])
			return 1
		}
		if jsonOut {
			return encodeJSON(stdout, stderr, map[string]any{"ok": true, "provider": name, "id": id, "priority": found.Priority})
		}
		fmt.Fprintf(stdout, "%s: %s selection order is %d\n", name, args[1], found.Priority)
		return 0
	}
	priority, ok := parseAccountPriorityArg(args[2])
	if !ok {
		fmt.Fprintln(stderr, "benes: selection order must be an integer -100..100, one of first/earlier/normal/later/last, or reset")
		return 2
	}
	body := map[string]any{"id": id, "priority": nil}
	if priority != nil {
		body["priority"] = *priority
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return serveFailure(stderr, "encode priority request", err)
	}
	status, raw, fail := accountLiveAccess("PUT", "/api/codex-auth/accounts/priority", payload, stderr, deps)
	if fail != 0 {
		return fail
	}
	if status != 200 {
		fmt.Fprintf(stderr, "benes: priority failed: HTTP %d\n", status)
		return 1
	}
	applied := 0
	if priority != nil {
		applied = *priority
	}
	var resp struct {
		Priority int `json:"priority"`
	}
	if json.Unmarshal(raw, &resp) == nil {
		applied = resp.Priority
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"ok": true, "provider": name, "id": id, "priority": applied})
	}
	fmt.Fprintf(stdout, "%s: %s selection order is now %d\n", name, args[1], applied)
	fmt.Fprintln(stderr, "Takes effect from the next unbound request; running threads keep their current account until drained.")
	fmt.Fprintln(stderr, "Also releases any manual \"use this account now\" pin, on any account.")
	return 0
}

func runAccountClearCooldown(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "benes: usage: account clear-cooldown <provider> <id|main> [--json]")
		return 2
	}
	name := strings.TrimSpace(args[0])
	id := strings.TrimSpace(args[1])
	kind, code := accountProviderKind(name, stderr, deps)
	if code != 0 {
		return code
	}
	if kind != "codex" {
		fmt.Fprintln(stderr, "benes: cooldown clearing applies to Codex accounts only")
		return 2
	}
	if id == "main" {
		id = "__main__"
	}
	payload, err := json.Marshal(map[string]string{"id": id})
	if err != nil {
		return serveFailure(stderr, "encode clear-cooldown request", err)
	}
	status, raw, fail := accountLiveAccess("POST", "/api/codex-auth/accounts/clear-cooldown", payload, stderr, deps)
	if fail != 0 {
		return fail
	}
	if status != 200 {
		fmt.Fprintf(stderr, "benes: clear-cooldown failed: HTTP %d\n", status)
		return 1
	}
	var resp struct {
		Cleared bool `json:"cleared"`
	}
	_ = json.Unmarshal(raw, &resp)
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"ok": true, "provider": name, "id": id, "cleared": resp.Cleared})
	}
	if resp.Cleared {
		fmt.Fprintf(stdout, "%s: cooldown lifted for %s\n", name, args[1])
		return 0
	}
	fmt.Fprintf(stdout, "%s: no active cooldown for %s\n", name, args[1])
	return 0
}

func parseAccountPriorityArg(raw string) (*int, bool) {
	word := strings.ToLower(strings.TrimSpace(raw))
	if word == "reset" {
		return nil, true
	}
	if value, ok := accountPriorityPresets[word]; ok {
		copy := value
		return &copy, true
	}
	n, err := strconv.Atoi(word)
	if err != nil || n < -100 || n > 100 {
		return nil, false
	}
	return &n, true
}
