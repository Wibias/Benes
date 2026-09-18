package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/Wibias/Benes/internal/claude"
)

const claudeInstallHint = "`claude` CLI not found. Install it first: npm install -g @anthropic-ai/claude-code"

var launchClaude = defaultLaunchClaude

func runClaude(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	if len(args) > 0 && args[0] == "config" {
		return runClaudeConfig(args[1:], stdout, stderr, deps)
	}
	return runClaudeLaunch(args, stdout, stderr, deps)
}

func runClaudeConfig(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	action := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action = strings.ToLower(args[0])
		args = args[1:]
	}
	args, jsonOut := takeConfigFlag(args, "--json")
	switch action {
	case "status", "show":
		if len(args) > 0 {
			fmt.Fprintln(stderr, "benes: usage: claude config status [--json]")
			return 2
		}
		return runObserveProxyGET("/api/claude-code", flagsJSON(jsonOut), stdout, stderr, deps)
	case "set":
		body := map[string]any{}
		enabled, args, err := takeOnOff(args, "--enabled")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 2
		}
		if enabled != nil {
			body["enabled"] = *enabled
		}
		authMode, args := takeOption(args, "--auth-mode")
		if authMode != "" {
			body["authMode"] = authMode
		}
		autoContext, args, err := takeOnOff(args, "--auto-context")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 2
		}
		if autoContext != nil {
			body["autoContext"] = *autoContext
		}
		injectAgents, args, err := takeOnOff(args, "--inject-agents")
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 2
		}
		if injectAgents != nil {
			body["injectAgents"] = *injectAgents
		}
		small, args := takeOption(args, "--small-fast-model")
		if small != "" {
			if small == "-" {
				body["smallFastModel"] = ""
			} else {
				body["smallFastModel"] = small
			}
		}
		if len(args) > 0 {
			fmt.Fprintln(stderr, "benes: usage: claude config set [--enabled on|off] [--auth-mode auto|proxy|subscription] [--json]")
			return 2
		}
		if len(body) == 0 {
			fmt.Fprintln(stderr, "benes: at least one Claude setting is required")
			return 2
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return serveFailure(stderr, "encode claude settings", err)
		}
		base, err := liveProxyBase(deps)
		if err != nil {
			fmt.Fprintln(stderr, "benes: proxy not running. Start it with benes start.")
			return 1
		}
		code, resp, err := accessDo("PUT", strings.TrimRight(base, "/")+"/api/claude-code", payload)
		if err != nil {
			return serveFailure(stderr, "claude config set", err)
		}
		if code < 200 || code >= 300 {
			fmt.Fprintf(stderr, "benes: claude config set failed: HTTP %d\n", code)
			return 1
		}
		if jsonOut {
			_, _ = stdout.Write(resp)
			if len(resp) == 0 || resp[len(resp)-1] != '\n' {
				fmt.Fprintln(stdout)
			}
			return 0
		}
		fmt.Fprintln(stdout, "Claude Code settings updated.")
		return 0
	default:
		fmt.Fprintf(stderr, "benes: unknown Claude config command %s\n", action)
		return 2
	}
}

func flagsJSON(jsonOut bool) []string {
	if jsonOut {
		return []string{"--json"}
	}
	return nil
}

func takeOnOff(args []string, flag string) (*bool, []string, error) {
	raw, rest := takeOption(args, flag)
	if raw == "" {
		return nil, rest, nil
	}
	switch strings.ToLower(raw) {
	case "on", "true", "1":
		v := true
		return &v, rest, nil
	case "off", "false", "0":
		v := false
		return &v, rest, nil
	default:
		return nil, rest, fmt.Errorf("benes: %s must be on or off", flag)
	}
}

func runClaudeLaunch(args []string, stdout, stderr io.Writer, deps commandDependencies) int {
	_ = stdout
	settings, ownTokens, roster, rosterSet, err := loadClaudeLaunchConfig(deps)
	if err != nil {
		return serveFailure(stderr, "load claude config", err)
	}
	if settings.Enabled != nil && !*settings.Enabled {
		fmt.Fprintln(stderr, "Claude inbound is disabled (config.claudeCode.enabled=false — flip the Claude ON toggle in the GUI or edit config).")
		return 1
	}
	if err := ensureLiveProxy(stderr, deps); err != nil {
		return 1
	}
	base, err := liveProxyBase(deps)
	if err != nil {
		fmt.Fprintln(stderr, "benes: proxy did not become healthy after starting.")
		return 1
	}
	port := claudeProxyPort(base)
	status, payload, err := accessDo("GET", strings.TrimRight(base, "/")+"/api/claude-code", nil)
	if err == nil && status >= 200 && status < 300 {
		live := parseClaudeCodeGET(payload)
		if live.Enabled != nil && !*live.Enabled {
			fmt.Fprintln(stderr, "Claude inbound is disabled (config.claudeCode.enabled=false — flip the Claude ON toggle in the GUI or edit config).")
			return 1
		}
		settings = mergeClaudeSettings(settings, live)
	}
	envMap := claude.EnvMapFromList(os.Environ())
	gatewayModels, windows, staleCache, cacheErr := loadClaudeGatewayModels(base)
	if cacheErr != nil {
		fmt.Fprintf(stderr, "⚠ Gateway model cache could not be refreshed: %v\n", cacheErr)
	}
	var warned bool
	built := claude.BuildEnv(claude.BuildInput{
		Port:           port,
		Settings:       settings,
		OwnTokens:      ownTokens,
		ContextWindows: windows,
		Env:            envMap,
		WarnUnknown: func() {
			if warned {
				return
			}
			warned = true
			fmt.Fprintln(stderr, "⚠ Claude 인증을 확인하지 못했습니다 — 구독 방식으로 진행합니다. GUI에서 인증 모드를 직접 지정하면 이 판단을 덮어쓸 수 있습니다.")
		},
	})
	switch {
	case cacheErr != nil:
	case staleCache:
		fmt.Fprintln(stderr, "⚠ Gateway model cache could not be refreshed; the model picker may be stale.")
	default:
		if _, writeErr := claude.WriteGatewayModelCache("http://127.0.0.1:"+strconv.Itoa(port), gatewayModels, claude.ClaudeConfigDir(built)); writeErr != nil {
			fmt.Fprintf(stderr, "⚠ Gateway model cache could not be refreshed: %v\n", writeErr)
		}
	}
	configDir := claude.ClaudeConfigDir(built)
	if err := claude.SyncAgentDefs(claude.BuildAgentDefs(settings, roster, configDir, rosterSet), configDir); err != nil {
		fmt.Fprintln(stderr, "⚠ Claude agent definitions could not be synced; check ~/.claude/agents permissions.")
	}
	if err := launchClaude(args, claude.EnvList(built)); err != nil {
		if isClaudeMissing(err) {
			fmt.Fprintln(stderr, claudeInstallHint)
			return 1
		}
		fmt.Fprintf(stderr, "benes: failed to launch claude: %v\n", err)
		return 1
	}
	return 0
}

func claudeProxyPort(base string) int {
	u, err := url.Parse(base)
	if err != nil {
		return 23100
	}
	n, conv := strconv.Atoi(u.Port())
	if conv != nil || n <= 0 {
		return 23100
	}
	return n
}

func parseClaudeCodeGET(payload []byte) claude.CodeSettings {
	var body struct {
		Enabled           *bool          `json:"enabled"`
		AuthMode          string         `json:"authMode"`
		AutoContext       *bool          `json:"autoContext"`
		AutoCompactWindow *int           `json:"autoCompactWindow"`
		InjectAgents      *bool          `json:"injectAgents"`
		SmallFastModel    string         `json:"smallFastModel"`
		Model             string         `json:"model"`
		TierModels        map[string]any `json:"tierModels"`
		ModelMap          map[string]any `json:"modelMap"`
	}
	_ = json.Unmarshal(payload, &body)
	settings := claude.CodeSettings{
		Enabled:        body.Enabled,
		AuthMode:       body.AuthMode,
		AutoContext:    body.AutoContext,
		InjectAgents:   body.InjectAgents,
		SmallFastModel: body.SmallFastModel,
		Model:          body.Model,
	}
	if body.AutoCompactWindow != nil {
		settings.AutoCompactWindow = *body.AutoCompactWindow
	}
	claude.ApplyFamilyRoutes(&settings, claude.ReadFamilyRoutes(map[string]any{
		"tierModels": body.TierModels,
		"modelMap":   body.ModelMap,
	}))
	return settings
}

func mergeClaudeSettings(base, live claude.CodeSettings) claude.CodeSettings {
	if live.Enabled != nil {
		base.Enabled = live.Enabled
	}
	if live.AuthMode != "" {
		base.AuthMode = live.AuthMode
	}
	if live.AutoContext != nil {
		base.AutoContext = live.AutoContext
	}
	if live.InjectAgents != nil {
		base.InjectAgents = live.InjectAgents
	}
	if live.Model != "" {
		base.Model = live.Model
	}
	if live.SmallFastModel != "" {
		base.SmallFastModel = live.SmallFastModel
	}
	if live.Opus != "" {
		base.Opus = live.Opus
	}
	if live.Sonnet != "" {
		base.Sonnet = live.Sonnet
	}
	if live.Haiku != "" {
		base.Haiku = live.Haiku
	}
	if live.Fable != "" {
		base.Fable = live.Fable
	}
	if live.AutoCompactWindow > 0 {
		base.AutoCompactWindow = live.AutoCompactWindow
	}
	return base
}

func loadClaudeLaunchConfig(deps commandDependencies) (claude.CodeSettings, []string, []string, bool, error) {
	root, err := loadConfigRawObject(deps)
	if err != nil {
		return claude.CodeSettings{}, nil, nil, false, err
	}
	settings := claude.CodeSettings{}
	if raw, ok := root["claudeCode"]; ok {
		var block struct {
			Enabled            *bool          `json:"enabled"`
			AuthMode           string         `json:"authMode"`
			AlwaysEnableEffort bool           `json:"alwaysEnableEffort"`
			MaxContextTokens   int            `json:"maxContextTokens"`
			AutoContext        *bool          `json:"autoContext"`
			AutoCompactWindow  int            `json:"autoCompactWindow"`
			InjectAgents       *bool          `json:"injectAgents"`
			Model              string         `json:"model"`
			SmallFastModel     string         `json:"smallFastModel"`
			TierModels         map[string]any `json:"tierModels"`
			ModelMap           map[string]any `json:"modelMap"`
		}
		if json.Unmarshal(raw, &block) == nil {
			settings.Enabled = block.Enabled
			settings.AuthMode = block.AuthMode
			settings.AlwaysEnableEffort = block.AlwaysEnableEffort
			settings.MaxContextTokens = block.MaxContextTokens
			settings.AutoContext = block.AutoContext
			settings.AutoCompactWindow = block.AutoCompactWindow
			settings.InjectAgents = block.InjectAgents
			settings.Model = block.Model
			settings.SmallFastModel = block.SmallFastModel
			claude.ApplyFamilyRoutes(&settings, claude.ReadFamilyRoutes(map[string]any{
				"tierModels": block.TierModels,
				"modelMap":   block.ModelMap,
			}))
		}
	}
	var own []string
	if raw, ok := root["apiKeys"]; ok {
		var keys []struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(raw, &keys) == nil {
			for _, key := range keys {
				if strings.TrimSpace(key.Key) != "" {
					own = append(own, key.Key)
				}
			}
		}
	}
	for _, envName := range []string{"BENES_API_AUTH_TOKEN", "BENES_API_AUTH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
			own = append(own, v)
		}
	}
	rosterSet := false
	var roster []string
	if raw, ok := root["subagentModels"]; ok {
		rosterSet = true
		_ = json.Unmarshal(raw, &roster)
	}
	return settings, own, roster, rosterSet, nil
}

func loadClaudeGatewayModels(base string) ([]claude.GatewayModel, map[string]int, bool, error) {
	status, payload, err := accessDo("GET", strings.TrimRight(base, "/")+"/v1/models?limit=1000&ids=cli", nil)
	if err != nil {
		return nil, nil, false, err
	}
	windows := claude.ContextWindowsFromModelsJSON(payload)
	if status < 200 || status >= 300 {
		return nil, windows, true, nil
	}
	var envelope struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return nil, windows, true, nil
	}
	models := make([]claude.GatewayModel, 0, len(envelope.Data))
	for _, row := range envelope.Data {
		if strings.TrimSpace(row.ID) == "" {
			continue
		}
		models = append(models, claude.GatewayModel{ID: row.ID, DisplayName: row.DisplayName})
	}
	return models, windows, false, nil
}

func defaultLaunchClaude(args []string, env []string) error {
	name, err := exec.LookPath("claude")
	if err != nil && runtime.GOOS == "windows" {
		name, err = exec.LookPath("claude.cmd")
	}
	if err != nil {
		return err
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	return cmd.Run()
}

func isClaudeMissing(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && runtime.GOOS == "windows" && exit.ExitCode() == 9009 {
		return true
	}
	return false
}
