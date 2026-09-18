package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Wibias/Benes/internal/codexfeatures"
	"github.com/Wibias/Benes/internal/config"
)

func runV2(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	verb := "status"
	if len(args) > 0 {
		verb = strings.ToLower(strings.TrimSpace(args[0]))
		args = args[1:]
	}
	switch verb {
	case "status":
		if len(args) > 0 {
			fmt.Fprintln(stderr, "benes: usage: v2 status|on|off|mode <v1|default|v2>")
			return 2
		}
		return runV2Status(stdout, stderr, deps)
	case "on", "off":
		if len(args) > 0 {
			fmt.Fprintln(stderr, "benes: usage: v2 on|off")
			return 2
		}
		return runV2Toggle(verb == "on", stdout, stderr)
	case "mode":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "benes: usage: v2 mode <v1|default|v2>")
			return 2
		}
		return runV2Mode(strings.ToLower(strings.TrimSpace(args[0])), stdout, stderr, deps)
	default:
		fmt.Fprintln(stderr, "benes: usage: v2 status|on|off|mode <v1|default|v2>")
		return 2
	}
}

func runV2Status(stdout, stderr io.Writer, deps commandDependencies) int {
	enabled, err := readMultiAgentV2Enabled()
	if err != nil {
		fmt.Fprintf(stderr, "benes: v2 status: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, v2StatusLine(enabled))
	mode := "default"
	keepNative := false
	root, err := loadConfigRawObject(deps)
	if err == nil {
		mode = jsonRawString(root["multiAgentMode"])
		if mode != "v1" && mode != "v2" {
			mode = "default"
		}
		var keep bool
		if json.Unmarshal(root["keepNativeChatGptOnV1"], &keep) == nil {
			keepNative = keep
		}
	}
	fmt.Fprintln(stdout, multiAgentModeLine(mode))
	if keepNative {
		fmt.Fprintln(stdout, "keep_native_chatgpt_on_v1: ON — ChatGPT-native rows stay v1 when mode is v2")
	} else {
		fmt.Fprintln(stdout, "keep_native_chatgpt_on_v1: OFF")
	}
	return 0
}

func runV2Toggle(want bool, stdout, stderr io.Writer) int {
	enabled, err := readMultiAgentV2Enabled()
	if err != nil {
		fmt.Fprintf(stderr, "benes: v2: %v\n", err)
		return 1
	}
	if enabled == want {
		state := "OFF"
		if want {
			state = "ON"
		}
		fmt.Fprintf(stdout, "multi_agent_v2 already %s — nothing to do.\n", state)
		return 0
	}
	action := "disable"
	if want {
		action = "enable"
	}
	if err := codexfeatures.Toggle(want); err != nil {
		fmt.Fprintf(stderr, "benes: codex features %s multi_agent_v2 failed: %v\n", action, err)
		return 1
	}

	fmt.Fprintln(stdout, v2StatusLine(want))
	fmt.Fprintln(stdout, "Applies to NEW sessions; running sessions keep their pinned multi-agent version.")
	return 0
}

func runV2Mode(mode string, stdout, stderr io.Writer, deps commandDependencies) int {
	if mode != "v1" && mode != "default" && mode != "v2" {
		fmt.Fprintln(stderr, "benes: usage: v2 mode <v1|default|v2>")
		return 2
	}
	if mode != "default" {
		if code := runV2Toggle(mode == "v2", stdout, stderr); code != 0 {
			return code
		}
	}
	if err := mutateConfig(stderr, deps, func(tx *config.Transaction) error {
		if mode == "default" {
			return tx.Delete(config.JSONPath("multiAgentMode"))
		}
		raw, err := json.Marshal(mode)
		if err != nil {
			return err
		}
		return tx.Set(config.JSONPath("multiAgentMode"), raw)
	}); err != nil {
		return 1
	}
	fmt.Fprintln(stdout, multiAgentModeLine(mode))
	fmt.Fprintln(stdout, "Applies to NEW sessions; running sessions keep their pinned multi-agent version.")
	return 0
}

func v2StatusLine(enabled bool) string {
	if enabled {
		return "multi_agent_v2: ON — v2 multi-agent surface active"
	}
	return "multi_agent_v2: OFF — v1 multi-agent surface (default install)"
}

func multiAgentModeLine(mode string) string {
	switch mode {
	case "v1":
		return "multi_agent_mode: v1 — ALL models forced to v1 surface (upstream pins overridden)"
	case "v2":
		return "multi_agent_mode: v2 — ALL models forced to v2 surface (upstream pins overridden)"
	default:
		return "multi_agent_mode: default — upstream model pins respected (sol/terra=v2, luna=v1, rest=codex flag)"
	}
}

func readMultiAgentV2Enabled() (bool, error) {
	home, err := config.ResolveCodexHome(config.CodexHomeOptions{})
	if err != nil {
		return false, err
	}
	return codexfeatures.Enabled(home)
}
