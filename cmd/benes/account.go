package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
)

type accountRow struct {
	Provider string `json:"provider"`
	Type     string `json:"type"`
	ID       string `json:"id"`
	Label    string `json:"label,omitempty"`
	Masked   string `json:"masked,omitempty"`
	Active   bool   `json:"active"`
	Needs    bool   `json:"needsReauth,omitempty"`
	Priority int    `json:"priority,omitempty"`
}

func runAccount(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	if len(args) == 0 {
		fmt.Fprintln(stderr, "benes: usage: account list [provider] [--json] [--all]")
		fmt.Fprintln(stderr, "             account current <provider> [--json]")
		fmt.Fprintln(stderr, "             account add-key <provider> [--label <label>] [--json]")
		fmt.Fprintln(stderr, "             account use <provider> <id> [--json]")
		fmt.Fprintln(stderr, "             account alias <provider> <id> <alias|-> [--json]")
		fmt.Fprintln(stderr, "             account refresh <provider> [--json]")
		fmt.Fprintln(stderr, "             account auto-switch <provider> <on|off|status|threshold <0-100>> [--json]")
		fmt.Fprintln(stderr, "             account priority <provider> <id|main> [first|earlier|normal|later|last|reset|<-100..100>] [--json]")
		fmt.Fprintln(stderr, "             account clear-cooldown <provider> <id|main> [--json]")
		fmt.Fprintln(stderr, "             account remove <provider> <id> --yes [--json]")
		fmt.Fprintln(stderr, "             account login <provider> [--id <account-id>] [--reauth] [--code -] [--no-wait] [--json]")
		fmt.Fprintln(stderr, "             account reauth <provider> [--id <account-id>] [--json]")
		fmt.Fprintln(stderr, "             account code <provider> [--flow <flow-id>] [--json]")
		fmt.Fprintln(stderr, "             account cancel <provider> [--flow <flow-id>] [--json]")
		fmt.Fprintln(stderr, "             account reset-credits <account-id|main> [--consume --yes] [--json]")
		fmt.Fprintln(stderr, "             account import <provider> --format <format> (--file <path>|--stdin) [--json]")
		fmt.Fprintln(stderr, "             account main doctor|list|register|add|switch|recover")
		return 2
	}
	switch args[0] {
	case "list":
		return runAccountList(args[1:], jsonOut, stdout, stderr, deps)
	case "current":
		return runAccountCurrent(args[1:], jsonOut, stdout, stderr, deps)
	case "add-key":
		return runAccountAddKey(args[1:], jsonOut, stdout, stderr, deps)
	case "use":
		return runAccountUse(args[1:], jsonOut, stdout, stderr, deps)
	case "alias":
		return runAccountAlias(args[1:], jsonOut, stdout, stderr, deps)
	case "refresh":
		return runAccountRefresh(args[1:], jsonOut, stdout, stderr, deps)
	case "auto-switch":
		return runAccountAutoSwitch(args[1:], jsonOut, stdout, stderr, deps)
	case "priority":
		return runAccountPriority(args[1:], jsonOut, stdout, stderr, deps)
	case "clear-cooldown":
		return runAccountClearCooldown(args[1:], jsonOut, stdout, stderr, deps)
	case "remove":
		return runAccountRemove(args[1:], jsonOut, stdout, stderr, deps)
	case "login", "reauth":
		return runAccountLogin(args[0], args[1:], jsonOut, stdout, stderr, deps)
	case "code":
		return runAccountCode(args[1:], jsonOut, stdout, stderr, deps)
	case "cancel":
		return runAccountCancel(args[1:], jsonOut, stdout, stderr, deps)
	case "reset-credits":
		return runAccountResetCredits(args[1:], jsonOut, stdout, stderr, deps)
	case "import":
		return runAccountImport(args[1:], jsonOut, stdout, stderr, deps)
	case "main":
		return runAccountMain(args[1:], jsonOut, stdout, stderr, deps)
	default:
		fmt.Fprintf(stderr, "benes: unknown account command %s\n", args[0])
		return 2
	}
}

func runAccountList(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	args, showAll := takeConfigFlag(args, "--all")
	name := ""
	if len(args) == 1 && !strings.HasPrefix(args[0], "-") {
		name = args[0]
		args = args[1:]
	}
	if len(args) > 0 {
		fmt.Fprintln(stderr, "benes: usage: account list [provider] [--json] [--all]")
		return 2
	}
	rows, notes, code := collectAccountRows(name, showAll, stderr, deps)
	if code != 0 {
		return code
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"accounts": rows, "notes": notes})
	}
	if len(rows) > 0 {
		fmt.Fprintln(stdout, formatAccountTable(rows))
	}
	for _, note := range notes {
		fmt.Fprintln(stdout, note)
	}
	if len(rows) == 0 && len(notes) == 0 {
		fmt.Fprintln(stdout, "No stored accounts or keys.")
	}
	return 0
}

func runAccountCurrent(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: account current <provider> [--json]")
		return 2
	}
	rows, notes, code := collectAccountRows(args[0], true, stderr, deps)
	if code != 0 {
		return code
	}
	var current *accountRow
	for i := range rows {
		if rows[i].Active {
			current = &rows[i]
			break
		}
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"current": current, "notes": notes})
	}
	if current == nil {
		fmt.Fprintln(stdout, "No active account.")
		return 0
	}
	fmt.Fprintln(stdout, formatAccountTable([]accountRow{*current}))
	return 0
}

func collectAccountRows(name string, showAll bool, stderr io.Writer, deps commandDependencies) ([]accountRow, []string, int) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return nil, nil, serveFailure(stderr, "load config", err)
	}
	providers := map[string]json.RawMessage{}
	if raw, ok := root["providers"]; ok {
		_ = json.Unmarshal(raw, &providers)
	}
	targets := []string{}
	if name != "" {
		targets = []string{name}
	} else {
		seen := map[string]struct{}{}
		for _, id := range append([]string{"openai"}, sortedKeysRaw(providers)...) {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			targets = append(targets, id)
		}
	}
	var rows []accountRow
	var notes []string
	for _, id := range targets {
		kind := classifyProvider(id, providers[id])
		if kind == "" {
			if name != "" {
				fmt.Fprintf(stderr, "benes: unknown provider %q\n", id)
				return nil, nil, 1
			}
			continue
		}
		family, code := fetchFamilyRows(id, kind, stderr, deps)
		if code != 0 {
			return nil, nil, code
		}
		if len(family) == 0 {
			if showAll {
				notes = append(notes, id+": no stored accounts or keys")
			}
			continue
		}
		rows = append(rows, family...)
	}
	return rows, notes, 0
}

func classifyProvider(name string, raw json.RawMessage) string {
	if name == "openai" {
		return "codex"
	}
	if isPublicOAuthName(name) {
		return "oauth"
	}
	if len(raw) == 0 {
		return ""
	}
	var rec struct {
		AuthMode string `json:"authMode"`
		APIKey   string `json:"apiKey"`
	}
	_ = json.Unmarshal(raw, &rec)
	if rec.AuthMode == "forward" {
		return "codex"
	}
	if rec.AuthMode == "key" || rec.APIKey != "" || rec.AuthMode == "" {
		return "api-key"
	}
	return "api-key"
}

func persistedOAuthProvider(name string) bool {
	return name == "google-antigravity" || name == "cursor" || name == "kimi" || name == "nous" || name == "github-copilot" || name == "xai" || name == "anthropic" || name == "command-code" || name == "kiro"
}

func isPublicOAuthName(name string) bool {
	for _, id := range []string{"command-code", "xai", "anthropic", "kimi", "nous", "kiro", "google-antigravity", "cursor", "github-copilot"} {
		if id == name {
			return true
		}
	}
	return false
}

func fetchFamilyRows(name, kind string, stderr io.Writer, deps commandDependencies) ([]accountRow, int) {
	if kind == "codex" {
		return fetchCodexFamilyRows(name, stderr, deps)
	}
	if kind == "oauth" {
		code, payload, err := observeGET("/api/oauth/accounts?provider="+url.QueryEscape(name), stderr, deps)
		if err != nil {
			return nil, 1
		}
		if code != 0 {
			return nil, code
		}
		var body struct {
			ActiveAccountID string `json:"activeAccountId"`
			Accounts        []struct {
				ID          string `json:"id"`
				Label       string `json:"label"`
				Active      bool   `json:"active"`
				NeedsReauth bool   `json:"needsReauth"`
			} `json:"accounts"`
		}
		if json.Unmarshal(payload, &body) != nil {
			return nil, 0
		}
		rows := make([]accountRow, 0, len(body.Accounts))
		for _, account := range body.Accounts {
			rows = append(rows, accountRow{
				Provider: name,
				Type:     "oauth",
				ID:       account.ID,
				Label:    account.Label,
				Active:   account.Active || account.ID == body.ActiveAccountID,
				Needs:    account.NeedsReauth,
			})
		}
		return rows, 0
	}
	code, payload, err := observeGET("/api/providers/keys?name="+url.QueryEscape(name), stderr, deps)
	if err != nil {
		return nil, 1
	}
	if code != 0 {
		return nil, code
	}
	var body struct {
		ActiveID string `json:"activeId"`
		Keys     []struct {
			ID     string `json:"id"`
			Label  string `json:"label"`
			Masked string `json:"masked"`
			Active bool   `json:"active"`
		} `json:"keys"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return nil, 0
	}
	rows := make([]accountRow, 0, len(body.Keys))
	for _, key := range body.Keys {
		rows = append(rows, accountRow{
			Provider: name,
			Type:     "api-key",
			ID:       key.ID,
			Label:    key.Label,
			Masked:   key.Masked,
			Active:   key.Active || key.ID == body.ActiveID,
		})
	}
	return rows, 0
}

func formatAccountTable(rows []accountRow) string {
	header := []string{"PROVIDER", "TYPE", "ID", "PLAN/LABEL", "STATUS"}
	data := make([][]string, 0, len(rows))
	for _, row := range rows {
		label := row.Label
		if row.Type == "api-key" && row.Masked != "" {
			label = row.Masked
		}
		if label == "" {
			label = "-"
		}
		status := ""
		if row.Active {
			status = "active"
		}
		if row.Needs {
			if status != "" {
				status += " "
			}
			status += "needs-reauth"
		}
		data = append(data, []string{row.Provider, row.Type, row.ID, label, status})
	}
	widths := make([]int, len(header))
	for i, col := range header {
		widths[i] = len(col)
	}
	for _, row := range data {
		for i, col := range row {
			if len(col) > widths[i] {
				widths[i] = len(col)
			}
		}
	}
	pad := func(cols []string) string {
		parts := make([]string, len(cols))
		for i, col := range cols {
			parts[i] = col + strings.Repeat(" ", widths[i]-len(col))
		}
		return strings.TrimRight(strings.Join(parts, "  "), " ")
	}
	lines := []string{pad(header)}
	for _, row := range data {
		lines = append(lines, pad(row))
	}
	return strings.Join(lines, "\n")
}

var accountKeyInput io.Reader = os.Stdin

func runAccountAddKey(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	label, args := takeOption(args, "--label")
	if len(args) != 1 {
		fmt.Fprintln(stderr, "benes: usage: account add-key <provider> [--label <label>] [--json]")
		fmt.Fprintln(stderr, "Pipe the API key on stdin.")
		return 2
	}
	name := strings.TrimSpace(args[0])
	kind, code := accountProviderKind(name, stderr, deps)
	if code != 0 {
		return code
	}
	if kind != "api-key" {
		fmt.Fprintln(stderr, "benes: add-key only applies to API-key providers")
		return 2
	}
	raw, err := io.ReadAll(io.LimitReader(accountKeyInput, 64<<10))
	if err != nil {
		fmt.Fprintf(stderr, "benes: read key: %v\n", err)
		return 1
	}
	key := strings.TrimSpace(string(raw))
	if key == "" {
		fmt.Fprintln(stderr, "benes: API key is required on stdin")
		return 2
	}
	req := map[string]string{"name": name, "key": key}
	if strings.TrimSpace(label) != "" {
		req["label"] = strings.TrimSpace(label)
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return serveFailure(stderr, "encode key request", err)
	}
	status, body, fail := accountLiveAccess("POST", "/api/providers/keys", payload, stderr, deps)
	if fail != 0 {
		return fail
	}
	if status != 201 {
		fmt.Fprintf(stderr, "benes: add-key failed: HTTP %d\n", status)
		return 1
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body, &created)
	id := created.ID
	if id == "" {
		id = name
	}
	textOut := ""
	if jsonOut {
		textOut, err = jsonEncode(map[string]any{"ok": true, "provider": name, "id": id})
		if err != nil {
			return serveFailure(stderr, "encode response", err)
		}
	} else {
		textOut = fmt.Sprintf("stored key for %s (%s)\n", name, id)
	}
	fmt.Fprint(stdout, strings.ReplaceAll(textOut, key, "[redacted]"))
	return 0
}

func runAccountRemove(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	args, yes := takeConfigFlag(args, "--yes")
	if !yes || len(args) != 2 {
		fmt.Fprintln(stderr, "benes: usage: account remove <provider> <id> --yes [--json]")
		return 2
	}
	name := strings.TrimSpace(args[0])
	id := strings.TrimSpace(args[1])
	kind, code := accountProviderKind(name, stderr, deps)
	if code != 0 {
		return code
	}
	var (
		path   string
		status int
		fail   int
	)
	switch kind {
	case "api-key":
		path = "/api/providers/keys?name=" + url.QueryEscape(name) + "&id=" + url.QueryEscape(id)
		status, _, fail = accountLiveAccess("DELETE", path, nil, stderr, deps)
	case "oauth":
		if !persistedOAuthProvider(name) {
			fmt.Fprintf(stderr, "benes: oauth account remove for %s is not persisted on the Go data plane\n", name)
			return 2
		}
		path = "/api/oauth/accounts?provider=" + url.QueryEscape(name) + "&id=" + url.QueryEscape(id)
		status, _, fail = accountLiveAccess("DELETE", path, nil, stderr, deps)
	case "codex":
		if id == "main" {
			fmt.Fprintln(stderr, "benes: the main Codex App login cannot be removed")
			return 2
		}
		path = "/api/codex-auth/accounts?id=" + url.QueryEscape(id)
		status, _, fail = accountLiveAccess("DELETE", path, nil, stderr, deps)
	default:
		fmt.Fprintf(stderr, "benes: account remove for %s is not supported\n", name)
		return 2
	}
	if fail != 0 {
		return fail
	}
	if status != 200 {
		fmt.Fprintf(stderr, "benes: remove failed: HTTP %d\n", status)
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"ok": true, "provider": name, "id": id})
	}
	fmt.Fprintf(stdout, "removed %s/%s\n", name, id)
	return 0
}

func runAccountUse(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "benes: usage: account use <provider> <id> [--json]")
		return 2
	}
	name := strings.TrimSpace(args[0])
	id := strings.TrimSpace(args[1])
	kind, code := accountProviderKind(name, stderr, deps)
	if code != 0 {
		return code
	}
	var (
		status int
		fail   int
	)
	switch kind {
	case "codex":
		if id == "main" {
			id = "__main__"
		}
		payload, err := json.Marshal(map[string]string{"accountId": id})
		if err != nil {
			return serveFailure(stderr, "encode use request", err)
		}
		status, _, fail = accountLiveAccess("PUT", "/api/codex-auth/active", payload, stderr, deps)
	case "api-key":
		payload, err := json.Marshal(map[string]string{"name": name, "id": id})
		if err != nil {
			return serveFailure(stderr, "encode use request", err)
		}
		status, _, fail = accountLiveAccess("PUT", "/api/providers/keys/active", payload, stderr, deps)
	case "oauth":
		if !persistedOAuthProvider(name) {
			fmt.Fprintf(stderr, "benes: account use is not supported for %s\n", name)
			return 2
		}
		payload, err := json.Marshal(map[string]string{"provider": name, "id": id})
		if err != nil {
			return serveFailure(stderr, "encode use request", err)
		}
		status, _, fail = accountLiveAccess("PUT", "/api/oauth/accounts/active", payload, stderr, deps)
	default:
		fmt.Fprintf(stderr, "benes: account use is not supported for %s\n", name)
		return 2
	}
	if fail != 0 {
		return fail
	}
	if status != 200 {
		fmt.Fprintf(stderr, "benes: use failed: HTTP %d\n", status)
		return 1
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, map[string]any{"ok": true, "provider": name, "type": kind, "activeId": id})
	}
	kindLabel := "key"
	if kind != "api-key" {
		kindLabel = "account"
	}
	display := id
	if display == "__main__" {
		display = "main"
	}
	fmt.Fprintf(stdout, "%s: active %s is now %s\n", name, kindLabel, display)
	return 0
}

func runAccountAlias(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) != 3 {
		fmt.Fprintln(stderr, "benes: usage: account alias <provider> <id> <alias|-> [--json]")
		return 2
	}
	name := strings.TrimSpace(args[0])
	id := strings.TrimSpace(args[1])
	alias := args[2]
	if alias == "-" {
		alias = ""
	}
	kind, code := accountProviderKind(name, stderr, deps)
	if code != 0 {
		return code
	}
	if kind == "codex" {
		if id == "main" || id == "__main__" {
			fmt.Fprintln(stderr, "benes: the main Codex App login cannot be renamed")
			return 2
		}
		payload, err := json.Marshal(map[string]string{"id": id, "alias": alias})
		if err != nil {
			return serveFailure(stderr, "encode alias request", err)
		}
		status, _, fail := accountLiveAccess("PUT", "/api/codex-auth/accounts/alias", payload, stderr, deps)
		if fail != 0 {
			return fail
		}
		if status != 200 {
			fmt.Fprintf(stderr, "benes: alias failed: HTTP %d\n", status)
			return 1
		}
		if jsonOut {
			var outAlias any
			if alias != "" {
				outAlias = alias
			}
			return encodeJSON(stdout, stderr, map[string]any{"ok": true, "provider": name, "id": id, "alias": outAlias})
		}
		if alias == "" {
			fmt.Fprintf(stdout, "%s: cleared alias for %s\n", name, id)
			return 0
		}
		fmt.Fprintf(stdout, "%s: %s is now %q\n", name, id, alias)
		return 0
	}
	if kind != "api-key" {
		fmt.Fprintf(stderr, "benes: account alias is not supported for %s\n", name)
		return 2
	}
	payload, err := json.Marshal(map[string]string{"name": name, "id": id, "alias": alias})
	if err != nil {
		return serveFailure(stderr, "encode alias request", err)
	}
	status, _, fail := accountLiveAccess("PUT", "/api/providers/keys/alias", payload, stderr, deps)
	if fail != 0 {
		return fail
	}
	if status != 200 {
		fmt.Fprintf(stderr, "benes: alias failed: HTTP %d\n", status)
		return 1
	}
	if jsonOut {
		var outAlias any
		if alias != "" {
			outAlias = alias
		}
		return encodeJSON(stdout, stderr, map[string]any{"ok": true, "provider": name, "id": id, "alias": outAlias})
	}
	if alias == "" {
		fmt.Fprintf(stdout, "%s: cleared alias for %s\n", name, id)
		return 0
	}
	fmt.Fprintf(stdout, "%s: %s is now %q\n", name, id, alias)
	return 0
}

func accountProviderKind(name string, stderr io.Writer, deps commandDependencies) (string, int) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return "", serveFailure(stderr, "load config", err)
	}
	providers := map[string]json.RawMessage{}
	if raw, ok := root["providers"]; ok {
		_ = json.Unmarshal(raw, &providers)
	}
	kind := classifyProvider(name, providers[name])
	if kind == "" {
		fmt.Fprintf(stderr, "benes: unknown provider %q\n", name)
		return "", 1
	}
	return kind, 0
}

func accountLiveAccess(method, path string, body []byte, stderr io.Writer, deps commandDependencies) (int, []byte, int) {
	base, err := liveProxyBase(deps)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v. Start it with: benes start\n", err)
		return 0, nil, 1
	}
	status, payload, err := accessDo(method, strings.TrimRight(base, "/")+path, body)
	if err != nil {
		return 0, nil, serveFailure(stderr, "account request", err)
	}
	return status, payload, 0
}

func jsonEncode(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw) + "\n", nil
}

func sortedKeysRaw(in map[string]json.RawMessage) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
