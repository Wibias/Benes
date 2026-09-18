package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type debugSettingsView struct {
	Enabled         bool            `json:"enabled"`
	Usage           bool            `json:"usage"`
	Injection       bool            `json:"injection"`
	Claude          bool            `json:"claude"`
	RuntimeOverride map[string]any  `json:"runtimeOverride"`
	Env             map[string]bool `json:"env"`
}

func runDebug(ctx context.Context, args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printDebugHelp(stdout)
		return 0
	}
	scope := strings.ToLower(args[0])
	if scope != "provider" && scope != "usage" && scope != "injection" && scope != "claude" {
		printDebugHelp(stderr)
		return 2
	}
	action := "status"
	rest := args[1:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		action = strings.ToLower(rest[0])
		rest = rest[1:]
	}
	switch action {
	case "on", "off":
		key := debugBodyKey(scope)
		body, _ := json.Marshal(map[string]bool{key: action == "on"})
		view, code := debugRequest(http.MethodPut, "/api/debug", body, stderr, deps)
		if code != 0 {
			return code
		}
		printDebugStatus(stdout, scope, view)
		fmt.Fprintf(stdout, "\n%s debug is now %s.\n", scope, map[bool]string{true: "enabled", false: "disabled"}[action == "on"])
		return 0
	case "status":
		view, code := debugRequest(http.MethodGet, "/api/debug", nil, stderr, deps)
		if code != 0 {
			return code
		}
		printDebugStatus(stdout, scope, view)
		return 0
	case "reset":
		reset := scope
		if scope == "provider" {
			reset = "provider"
		}
		body, _ := json.Marshal(map[string]any{"reset": reset})
		view, code := debugRequest(http.MethodPut, "/api/debug", body, stderr, deps)
		if code != 0 {
			return code
		}
		printDebugStatus(stdout, scope, view)
		fmt.Fprintf(stdout, "\nRuntime override cleared for %s; effective value follows env again.\n", scope)
		return 0
	case "logs":
		if scope == "injection" || scope == "claude" {
			if scope == "claude" {
				fmt.Fprintln(stderr, "Use: benes observe claude-inbound")
			} else {
				fmt.Fprintln(stderr, "Injection debug has no buffered log stream; use: benes observe injection")
			}
			return 1
		}
		follow := false
		for _, arg := range rest {
			if arg == "-f" || arg == "--follow" {
				if follow {
					fmt.Fprintf(stderr, "benes: usage: debug %s on|off|status|reset|logs [-f]\n", scope)
					return 2
				}
				follow = true
				continue
			}
			fmt.Fprintf(stderr, "benes: usage: debug %s on|off|status|reset|logs [-f]\n", scope)
			return 2
		}
		path := "/api/debug/logs"
		if scope == "usage" {
			path = "/api/debug/usage-logs"
		}
		base, err := liveProxyBase(deps)
		if err != nil {
			fmt.Fprintf(stderr, "benes: %v. Start it with: benes start\n", err)
			return 1
		}
		after := 0
		for {
			reqPath := path
			if after > 0 {
				reqPath = path + "?after=" + strconv.Itoa(after)
			}
			code, payload, err := accessDo(http.MethodGet, base+reqPath, nil)
			if err != nil {
				fmt.Fprintf(stderr, "benes: debug logs: %v\n", err)
				return 1
			}
			if code < 200 || code >= 300 {
				fmt.Fprintf(stderr, "benes: debug logs failed: HTTP %d\n", code)
				return 1
			}
			var entries []struct {
				Seq  int    `json:"seq"`
				Line string `json:"line"`
			}
			if json.Unmarshal(payload, &entries) != nil {
				fmt.Fprintln(stderr, "benes: debug logs returned an unexpected payload")
				return 1
			}
			for _, entry := range entries {
				fmt.Fprintln(stdout, entry.Line)
				if entry.Seq > after {
					after = entry.Seq
				}
			}
			if !follow {
				return 0
			}
			if !followWait(ctx, deps) {
				return 0
			}
		}
	default:
		if scope == "injection" || scope == "claude" {
			fmt.Fprintf(stderr, "benes: usage: debug %s on|off|status|reset\n", scope)
		} else {
			fmt.Fprintf(stderr, "benes: usage: debug %s on|off|status|reset|logs [-f]\n", scope)
		}
		return 2
	}
}

func debugBodyKey(scope string) string {
	if scope == "provider" {
		return "debug"
	}
	return scope
}

func debugRequest(method, path string, body []byte, stderr io.Writer, deps commandDependencies) (debugSettingsView, int) {
	base, err := liveProxyBase(deps)
	if err != nil {
		fmt.Fprintf(stderr, "benes: %v. Start it with: benes start\n", err)
		return debugSettingsView{}, 1
	}
	code, payload, err := accessDo(method, base+path, body)
	if err != nil {
		fmt.Fprintf(stderr, "benes: debug: %v\n", err)
		return debugSettingsView{}, 1
	}
	if code < 200 || code >= 300 {
		fmt.Fprintf(stderr, "benes: debug failed: HTTP %d\n", code)
		return debugSettingsView{}, 1
	}
	var view debugSettingsView
	if json.Unmarshal(payload, &view) != nil {
		fmt.Fprintln(stderr, "benes: debug returned an unexpected payload")
		return debugSettingsView{}, 1
	}
	return view, 0
}

func printDebugStatus(stdout io.Writer, scope string, view debugSettingsView) {
	override := func(key string) string {
		if view.RuntimeOverride == nil {
			return "env/default"
		}
		value, ok := view.RuntimeOverride[key]
		if !ok {
			return "env/default"
		}
		if enabled, ok := value.(bool); ok && enabled {
			return "on"
		}
		return "off"
	}
	switch scope {
	case "provider":
		fmt.Fprintf(stdout, "Provider debug: %s\n", onOff(view.Enabled))
		fmt.Fprintf(stdout, "  env=%s, runtime=%s\n", onOffLower(view.Env["debug"]), override("debug"))
		fmt.Fprintln(stdout, "  Tail: benes debug provider logs [-f]")
	case "usage":
		fmt.Fprintf(stdout, "Usage debug: %s\n", onOff(view.Usage))
		fmt.Fprintf(stdout, "  env=%s, runtime=%s\n", onOffLower(view.Env["usage"]), override("usage"))
		fmt.Fprintln(stdout, "  Tail: benes debug usage logs [-f] (via running proxy API)")
	case "injection":
		fmt.Fprintf(stdout, "Injection debug: %s\n", onOff(view.Injection))
		fmt.Fprintf(stdout, "  env=%s, runtime=%s\n", onOffLower(view.Env["injection"]), override("injection"))
		fmt.Fprintln(stdout, "  Lines appear on the proxy console when multi-agent guidance is injected.")
	default:
		fmt.Fprintf(stdout, "Claude inbound debug: %s\n", onOff(view.Claude))
		fmt.Fprintf(stdout, "  env=%s, runtime=%s\n", onOffLower(view.Env["claude"]), override("claude"))
		fmt.Fprintln(stdout, "  View: benes observe claude-inbound")
	}
}

func onOff(v bool) string {
	if v {
		return "ON"
	}
	return "off"
}

func onOffLower(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func printDebugHelp(w io.Writer) {
	fmt.Fprintln(w, "Debug commands (proxy must be running):")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  benes debug provider on|off|status|reset|logs [-f]")
	fmt.Fprintln(w, "  benes debug usage on|off|status|reset|logs [-f]")
	fmt.Fprintln(w, "  benes debug injection on|off|status|reset")
	fmt.Fprintln(w, "  benes debug claude on|off|status|reset")
}
