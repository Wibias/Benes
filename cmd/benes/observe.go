package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/codexlogguard"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/requesthistory"
	"github.com/Wibias/Benes/internal/usage"
)

func runObserve(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	sub := "logs"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = strings.ToLower(args[0])
		args = args[1:]
	}
	switch sub {
	case "logs":
		if len(args) > 0 && (args[0] == "explain" || args[0] == "rebuild-index" || args[0] == "index-status") {
			return runObserveLogsHistory(args, stdout, stderr, deps)
		}
		return runObserveLogs(ctx, args, stdout, stderr, deps)

	case "debug":
		return runObserveProxyGET("/api/debug", args, stdout, stderr, deps)
	case "injection":
		return runObserveProxyGET("/api/debug/injection-logs", args, stdout, stderr, deps)
	case "claude-inbound":
		return runObserveProxyGET("/api/claude/inbound-debug", args, stdout, stderr, deps)
	case "memory":
		return runObserveProxyGET("/api/system/memory", args, stdout, stderr, deps)
	case "storage":
		return runObserveStorage(args, stdout, stderr, deps)
	case "usage":
		return runObserveUsage(args, stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: observe logs|debug|injection|claude-inbound|memory|storage|usage [--json]")
		return 2
	}
}

func runLogs(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	return runObserve(ctx, append([]string{"logs"}, args...), stdout, stderr, deps)
}

func runMemory(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	return runObserve(nil, append([]string{"memory"}, args...), stdout, stderr, deps)
}

func runStorage(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	return runObserve(nil, append([]string{"storage"}, args...), stdout, stderr, deps)
}

func runUsage(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	return runObserve(nil, append([]string{"usage"}, args...), stdout, stderr, deps)
}

func runObserveUsage(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch strings.ToLower(args[0]) {
		case "retention":
			return runObserveUsageRetention(args[1:], stdout, stderr, deps)
		}
	}
	args, jsonOut := takeConfigFlag(args, "--json")
	rangeRaw, args := takeOption(args, "--range")
	surface, args := takeOption(args, "--surface")
	date, args := takeOption(args, "--date")
	start, args := takeOption(args, "--start")
	end, args := takeOption(args, "--end")
	tz, args := takeOption(args, "--tz")
	provider, args := takeOption(args, "--provider")
	model, args := takeOption(args, "--model")
	account, args := takeOption(args, "--account")
	if len(args) > 0 {
		fmt.Fprintln(stderr, "benes: usage: observe usage [--range today|yesterday|7d|30d|all] [--date YYYY-MM-DD] [--start <time>] [--end <time>] [--tz IANA] [--surface all|codex|claude|grok] [--provider id] [--model id] [--account id] [--json]")
		fmt.Fprintln(stderr, "       observe usage retention status|preview|run [--max-bytes N] [--max-age-ms N] [--digest HEX] [--json]")
		return 2
	}
	if rangeRaw == "" {
		rangeRaw = "30d"
	}
	switch rangeRaw {
	case "7d", "30d", "all", "today", "yesterday", "custom":
	default:
		fmt.Fprintln(stderr, "benes: --range must be today, yesterday, 7d, 30d, all, or custom")
		return 2
	}
	if surface == "" {
		surface = "all"
	}
	if surface != "all" && surface != "codex" && surface != "claude" && surface != "grok" {
		fmt.Fprintln(stderr, "benes: --surface must be all, codex, claude, or grok")
		return 2
	}
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	table := usage.DefaultTable()
	if strings.TrimSpace(paths.Config) != "" {
		if disk, err := config.LoadDiskConfig(paths.Config, 0); err == nil {
			table.AddOperatorOverlays(usage.OverlaysFromDiskProviders(disk.Providers))
		}
	}
	query := usage.Query{
		Range:    usage.ParseRange(rangeRaw),
		Surface:  usage.ParseSurface(surface),
		Provider: provider,
		Model:    model,
		Account:  account,
		Location: usage.ParseLocation(tz, ""),
		Date:     date,
		Start:    start,
		End:      end,
	}
	if err := usage.ResolveWindow(query); err != nil {
		fmt.Fprintf(stderr, "benes: %v\n", err)
		return 2
	}
	summary, err := usage.SummarizeHomeQuery(paths.Home, query, table)
	if err != nil {
		return serveFailure(stderr, "usage", err)
	}
	if jsonOut {
		return encodeJSON(stdout, stderr, summary)
	}
	text := usage.FormatMarkdown(summary)
	if _, err := io.WriteString(stdout, text); err != nil {
		return serveFailure(stderr, "write usage", err)
	}
	if !strings.HasSuffix(text, "\n") {
		fmt.Fprintln(stdout)
	}
	return 0
}

func runObserveStorage(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	if len(args) > 0 {
		if args[0] == "codex-logs" {
			return runCodexLogs(args[1:], jsonOut, stdout, stderr, deps)
		}
		fmt.Fprintln(stderr, "benes: usage: observe storage [--json]")
		return 2
	}
	flags := []string{}
	if jsonOut {
		flags = append(flags, "--json")
	}
	return runObserveProxyGET("/api/storage", flags, stdout, stderr, deps)
}

func runObserveLogs(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	args, jsonl := takeConfigFlag(args, "--jsonl")
	args, follow := takeConfigFlag(args, "--follow")
	if !follow {
		args, follow = takeConfigFlag(args, "-f")
	}
	provider, args := takeOption(args, "--provider")
	model, args := takeOption(args, "--model")
	status, args := takeOption(args, "--status")
	limitRaw, args := takeOption(args, "--limit")
	if len(args) > 0 {
		fmt.Fprintln(stderr, "benes: usage: observe logs [--provider name] [--model id] [--status code] [--limit n] [--follow] [--json|--jsonl]")
		return 2
	}
	if jsonOut && jsonl {
		fmt.Fprintln(stderr, "benes: --json and --jsonl cannot be combined")
		return 2
	}
	if follow && jsonOut {
		fmt.Fprintln(stderr, "benes: --follow uses --jsonl, not --json")
		return 2
	}
	query := url.Values{}
	if provider != "" {
		query.Set("provider", provider)
	}
	if model != "" {
		query.Set("model", model)
	}
	if status != "" {
		query.Set("status", status)
	}
	if limitRaw != "" {
		if n, err := strconv.Atoi(limitRaw); err != nil || n < 1 {
			fmt.Fprintln(stderr, "benes: --limit must be a positive integer")
			return 2
		}
		query.Set("limit", limitRaw)
	}
	path := "/api/logs"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	seen := map[string]struct{}{}
	seenOrder := make([]string, 0)
	for {
		code, payload, err := observeGET(path, stderr, deps)
		if err != nil {
			return 1
		}
		if code != 0 {
			return code
		}
		if !follow && jsonOut {
			_, _ = stdout.Write(payload)
			if len(payload) == 0 || payload[len(payload)-1] != '\n' {
				fmt.Fprintln(stdout)
			}
			return 0
		}
		var entries []map[string]any
		if json.Unmarshal(payload, &entries) != nil {
			fmt.Fprintln(stderr, "benes: observe logs returned an unexpected payload")
			return 1
		}
		for _, row := range entries {
			key := observeLogKey(row)
			if follow {
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				seenOrder = append(seenOrder, key)
			}
			if jsonl {
				line, _ := json.Marshal(row)
				fmt.Fprintln(stdout, string(line))
				continue
			}
			fmt.Fprintln(stdout, formatObserveLog(row))
		}
		if !follow {
			return 0
		}
		if len(seenOrder) > 5000 {
			drop := seenOrder[:len(seenOrder)-2500]
			seenOrder = append([]string{}, seenOrder[len(seenOrder)-2500:]...)
			for _, key := range drop {
				delete(seen, key)
			}
		}
		if !followWait(ctx, deps) {
			return 0
		}
	}
}
func runObserveProxyGET(path string, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	limitRaw, args := takeOption(args, "--limit")
	if len(args) > 0 {
		fmt.Fprintf(stderr, "benes: usage: observe %s [--limit n] [--json]\n", strings.TrimPrefix(path, "/api/"))
		return 2
	}
	if limitRaw != "" {
		if n, err := strconv.Atoi(limitRaw); err != nil || n < 1 {
			fmt.Fprintln(stderr, "benes: --limit must be a positive integer")
			return 2
		}
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		path += sep + "limit=" + url.QueryEscape(limitRaw)
	}
	code, payload, err := observeGET(path, stderr, deps)
	if err != nil {
		return 1
	}
	if code != 0 {
		return code
	}
	if jsonOut {
		_, _ = stdout.Write(payload)
		if len(payload) == 0 || payload[len(payload)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return 0
	}
	fmt.Fprintln(stdout, strings.TrimSpace(string(payload)))
	return 0
}

func observeGET(path string, stderr io.Writer, deps commandDependencies) (int, []byte, error) {
	base, err := liveProxyBase(deps)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v. Start it with: benes start\n", err)
		return 1, nil, err
	}
	code, payload, err := accessDo(http.MethodGet, base+path, nil)
	if err != nil {
		fmt.Fprintf(stderr, "benes: observe: %v\n", err)
		return 1, nil, err
	}
	if code < 200 || code >= 300 {
		fmt.Fprintf(stderr, "benes: observe failed: HTTP %d\n", code)
		return 1, nil, fmt.Errorf("http %d", code)
	}
	return 0, payload, nil
}

func observeLogKey(row map[string]any) string {
	if id := observeString(row["id"]); id != "" {
		return id
	}
	status := observeString(row["status"])
	if status == "" {
		if value, ok := row["status"]; ok && value != nil {
			status = fmt.Sprint(value)
		}
	}
	return strings.Join([]string{
		observeString(row["timestamp"]),
		observeString(row["provider"]),
		observeString(row["model"]),
		status,
	}, ":")
}

func formatObserveLog(row map[string]any) string {
	parts := []string{}
	if ts := observeString(row["timestamp"]); ts != "" {
		parts = append(parts, ts)
	}
	if status, ok := row["status"]; ok && status != nil {
		parts = append(parts, fmt.Sprint(status))
	}
	provider := observeString(row["provider"])
	model := observeString(row["model"])
	route := strings.Trim(provider+"/"+model, "/")
	if route != "" {
		parts = append(parts, route)
	} else if path := observeString(row["path"]); path != "" {
		parts = append(parts, path)
	}
	if ms, ok := row["durationMs"]; ok && ms != nil {
		parts = append(parts, fmt.Sprintf("%vms", ms))
	}
	return strings.Join(parts, "  ")
}

func observeString(value any) string {
	if value == nil {
		return ""
	}
	s, ok := value.(string)
	if !ok {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return s
}

func runObserveLogsHistory(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	args, jsonOut := takeConfigFlag(args, "--json")
	action := args[0]
	args = args[1:]
	paths, err := deps.resolvePaths(config.PathOptions{})
	if err != nil {
		return serveFailure(stderr, "resolve paths", err)
	}
	switch action {
	case "rebuild-index":
		if len(args) > 0 {
			fmt.Fprintln(stderr, "benes: usage: logs rebuild-index [--json]")
			return 2
		}
		meta, err := requesthistory.Rebuild(paths.Home)
		if err != nil {
			return serveFailure(stderr, "rebuild index", err)
		}
		return printHistoryMeta(meta, jsonOut, true, stdout, stderr)
	case "index-status":
		if len(args) > 0 {
			fmt.Fprintln(stderr, "benes: usage: logs index-status [--json]")
			return 2
		}
		return printHistoryMeta(requesthistory.Status(paths.Home), jsonOut, false, stdout, stderr)
	case "explain":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "benes: usage: logs explain <request-id> [--json]")
			return 2
		}
		return runObserveProxyGET("/api/request-history/"+url.PathEscape(args[0])+"/route-decision", flagsJSON(jsonOut), stdout, stderr, deps)
	default:
		return 2
	}
}

func printHistoryMeta(meta requesthistory.Meta, jsonOut, rebuilt bool, stdout, stderr io.Writer) int {
	if jsonOut {
		return encodeJSON(stdout, stderr, meta)
	}
	label := "Request-history index"
	if rebuilt {
		label = "Request-history index rebuilt"
	}
	fmt.Fprintf(stdout, "%s (%s)\n", label, meta.DBPath)
	fmt.Fprintf(stdout, "  schema version: %d\n", meta.SchemaVersion)
	fmt.Fprintf(stdout, "  indexed rows:   %d\n", meta.IndexedRows)
	fmt.Fprintf(stdout, "  source size:    %d bytes\n", meta.SourceSize)
	fmt.Fprintf(stdout, "  indexed offset: %d bytes\n", meta.IndexedOffset)
	fmt.Fprintf(stdout, "  pending bytes:  %d\n", meta.PendingBytes)
	fmt.Fprintf(stdout, "  caught up:      %v\n", meta.CaughtUp)
	fmt.Fprintf(stdout, "  rebuild required: %v\n", meta.RebuildRequired)
	errText := meta.LastError
	if errText == "" {
		errText = "none"
	}
	fmt.Fprintf(stdout, "  last error:     %s\n", errText)
	return 0
}

func runCodexLogs(args []string, jsonOut bool, stdout, stderr io.Writer, deps commandDependencies) int {
	action := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action = strings.ToLower(args[0])
		args = args[1:]
	}
	modeRaw, args := takeOption(args, "--mode")
	if len(args) > 0 {
		fmt.Fprintln(stderr, "benes: usage: observe storage codex-logs [status|protect|unprotect|repair|compact] [--mode compat|quiet] [--json]")
		return 2
	}
	home, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "benes: resolve CODEX_HOME: %v\n", err)
		return 1
	}
	desired := logGuardDesiredFromConfig(deps)
	switch action {
	case "status":
		return printLogGuardStatus(codexlogguard.Inspect(home, desired), jsonOut, stdout, stderr)
	case "protect":
		mode := codexlogguard.Mode(strings.TrimSpace(modeRaw))
		if mode == "" {
			mode = "compat"
		}
		if mode != codexlogguard.ModeCompat && mode != codexlogguard.ModeQuiet {
			fmt.Fprintln(stderr, "benes: --mode must be compat or quiet")
			return 2
		}
		if err := persistLogGuardMode(stderr, deps, mode); err != nil {
			return 1
		}
		result := codexlogguard.Protect(home, mode)
		if !result.OK {
			fmt.Fprintf(stderr, "benes: protect failed: %s\n", result.Error)
			return 1
		}
		return printLogGuardStatus(result.Status, jsonOut, stdout, stderr)
	case "unprotect":
		if err := persistLogGuardMode(stderr, deps, codexlogguard.ModeOff); err != nil {
			return 1
		}
		result := codexlogguard.Protect(home, codexlogguard.ModeOff)
		if !result.OK {
			fmt.Fprintf(stderr, "benes: unprotect failed: %s\n", result.Error)
			return 1
		}
		return printLogGuardStatus(result.Status, jsonOut, stdout, stderr)
	case "repair":
		result := codexlogguard.Protect(home, desired)
		if !result.OK {
			fmt.Fprintf(stderr, "benes: repair failed: %s\n", result.Error)
			return 1
		}
		return printLogGuardStatus(result.Status, jsonOut, stdout, stderr)
	case "compact":
		result := codexlogguard.Compact(home)
		if !result.OK {
			fmt.Fprintf(stderr, "benes: compact failed: %s\n", result.Error)
			return 1
		}
		return printLogGuardStatus(result.Status, jsonOut, stdout, stderr)
	default:
		fmt.Fprintln(stderr, "benes: usage: observe storage codex-logs [status|protect|unprotect|repair|compact] [--mode compat|quiet] [--json]")
		return 2
	}
}

func printLogGuardStatus(status codexlogguard.Status, jsonOut bool, stdout, stderr io.Writer) int {
	if jsonOut {
		return encodeJSON(stdout, stderr, status)
	}
	prot, _ := status.Protection["state"].(string)
	desired, _ := status.Protection["desiredMode"].(codexlogguard.Mode)
	if desired == "" {
		if s, ok := status.Protection["desiredMode"].(string); ok {
			desired = codexlogguard.Mode(s)
		}
	}
	fmt.Fprintf(stdout, "protection: %s (desired %s)\n", prot, desired)
	if schema, _ := status.Schema["state"].(string); schema != "" {
		fmt.Fprintf(stdout, "schema: %s\n", schema)
	}
	return 0
}

func logGuardDesiredFromConfig(deps commandDependencies) codexlogguard.Mode {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return codexlogguard.ModeOff
	}
	var block map[string]json.RawMessage
	if json.Unmarshal(root["codexLogGuard"], &block) != nil {
		return codexlogguard.ModeOff
	}
	var mode string
	if json.Unmarshal(block["mode"], &mode) != nil {
		return codexlogguard.ModeOff
	}
	if mode == "compat" || mode == "quiet" {
		return codexlogguard.Mode(mode)
	}
	return codexlogguard.ModeOff
}

func persistLogGuardMode(stderr io.Writer, deps commandDependencies, mode codexlogguard.Mode) error {
	raw, err := json.Marshal(map[string]any{"mode": mode})
	if err != nil {
		return err
	}
	return mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		return tx.Set(config.JSONPath("codexLogGuard"), raw)
	})
}

func runObserveUsageRetention(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	sub := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = strings.ToLower(args[0])
		args = args[1:]
	}
	args, jsonOut := takeConfigFlag(args, "--json")
	maxBytesRaw, args := takeOption(args, "--max-bytes")
	maxAgeRaw, args := takeOption(args, "--max-age-ms")
	digest, args := takeOption(args, "--digest")
	if len(args) > 0 {
		fmt.Fprintln(stderr, "benes: usage: observe usage retention status|preview|run [--max-bytes N] [--max-age-ms N] [--digest HEX] [--json]")
		return 2
	}
	body := map[string]any{}
	if maxBytesRaw != "" {
		n, err := strconv.ParseInt(maxBytesRaw, 10, 64)
		if err != nil || n < 0 {
			fmt.Fprintln(stderr, "benes: --max-bytes must be a non-negative integer")
			return 2
		}
		body["maxBytes"] = n
	}
	if maxAgeRaw != "" {
		n, err := strconv.ParseInt(maxAgeRaw, 10, 64)
		if err != nil || n < 0 {
			fmt.Fprintln(stderr, "benes: --max-age-ms must be a non-negative integer")
			return 2
		}
		body["maxAgeMs"] = n
	}
	switch sub {
	case "status":
		code, payload, err := observeGET("/api/usage/retention", stderr, deps)
		if err != nil {
			return 1
		}
		if code != 0 {
			return code
		}
		return writeObservePayload(stdout, payload, jsonOut)
	case "preview":
		code, payload, err := observeJSON(http.MethodPost, "/api/usage/retention/preview", body, stderr, deps)
		if err != nil {
			return 1
		}
		if code != 0 {
			return code
		}
		return writeObservePayload(stdout, payload, jsonOut)
	case "run":
		if digest == "" {
			fmt.Fprintln(stderr, "benes: observe usage retention run requires --digest from preview")
			return 2
		}
		body["digest"] = digest
		code, payload, err := observeJSON(http.MethodPost, "/api/usage/retention/run", body, stderr, deps)
		if err != nil {
			return 1
		}
		if code != 0 {
			return code
		}
		return writeObservePayload(stdout, payload, jsonOut)
	default:
		fmt.Fprintln(stderr, "benes: usage: observe usage retention status|preview|run [--max-bytes N] [--max-age-ms N] [--digest HEX] [--json]")
		return 2
	}
}

func writeObservePayload(stdout io.Writer, payload []byte, jsonOut bool) int {
	_, _ = stdout.Write(payload)
	if len(payload) == 0 || payload[len(payload)-1] != '\n' {
		fmt.Fprintln(stdout)
	}
	_ = jsonOut
	return 0
}

func observeJSON(method, path string, body map[string]any, stderr io.Writer, deps commandDependencies) (int, []byte, error) {
	base, err := liveProxyBase(deps)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v. Start it with: benes start\n", err)
		return 1, nil, err
	}
	var raw []byte
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			fmt.Fprintf(stderr, "benes: observe: %v\n", err)
			return 1, nil, err
		}
	}
	code, payload, err := accessDo(method, base+path, raw)
	if err != nil {
		fmt.Fprintf(stderr, "benes: observe: %v\n", err)
		return 1, nil, err
	}
	if code < 200 || code >= 300 {
		fmt.Fprintf(stderr, "benes: observe failed: HTTP %d\n%s\n", code, strings.TrimSpace(string(payload)))
		return 1, payload, fmt.Errorf("http %d", code)
	}
	return 0, payload, nil
}
