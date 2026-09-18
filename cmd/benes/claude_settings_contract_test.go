package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/claude"
	"github.com/Wibias/Benes/internal/config"
)

func TestParseClaudeCodeGETConsumesOnlyFamilyModelMapKeys(t *testing.T) {
	got := parseClaudeCodeGET([]byte(`{
		"enabled":true,
		"authMode":"proxy",
		"autoContext":true,
		"injectAgents":true,
		"smallFastModel":"gpt-5.4-mini",
		"autoCompactWindow":100000,
		"modelMap":{
			"opus":"gpt-5.6-sol",
			"sonnet":"gpt-5.6-sol",
			"haiku":"gpt-5.6-mini",
			"fable":"gpt-5.6-terra",
			"claude-3-opus-20240229":"intercepted"
		}
	}`))
	if got.Opus != "gpt-5.6-sol" || got.Sonnet != "gpt-5.6-sol" || got.Haiku != "gpt-5.6-mini" || got.Fable != "gpt-5.6-terra" {
		t.Fatalf("family=%+v", got)
	}
	if got.SmallFastModel != "gpt-5.4-mini" {
		t.Fatalf("helper=%s", got.SmallFastModel)
	}
	if got.AutoCompactWindow != 100000 {
		t.Fatalf("live GET must keep stored compact window, got %d", got.AutoCompactWindow)
	}
}

func TestParseClaudeCodeGETPrefersTierModelsOverModelMap(t *testing.T) {
	got := parseClaudeCodeGET([]byte(`{
		"tierModels":{"opus":"canonical-opus","sonnet":"canonical-sonnet"},
		"modelMap":{"opus":"legacy-opus","haiku":"legacy-haiku","claude-3":"intercepted"}
	}`))
	if got.Opus != "canonical-opus" || got.Sonnet != "canonical-sonnet" || got.Haiku != "legacy-haiku" {
		t.Fatalf("%+v", got)
	}
	if got.Fable != "" {
		t.Fatalf("unset fable=%s", got.Fable)
	}
}

func TestLoadClaudeLaunchConfigReadsDiskTierModelsNotArbitraryMap(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	payload := `{
		"subagentModels":["openai-apikey/gpt-5.5"],
		"claudeCode":{
			"enabled":true,
			"authMode":"subscription",
			"autoContext":false,
			"autoCompactWindow":350000,
			"injectAgents":true,
			"smallFastModel":"gpt-5.4-mini",
			"modelMap":{"opus":"from-map","claude-3":"intercepted"},
			"tierModels":{"opus":"disk-opus","sonnet":"disk-sonnet","haiku":"disk-haiku","fable":"disk-fable"}
		}
	}`
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, _, roster, rosterSet, err := loadClaudeLaunchConfig(commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, Config: configPath}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Opus != "disk-opus" || settings.Sonnet != "disk-sonnet" || settings.Haiku != "disk-haiku" || settings.Fable != "disk-fable" {
		t.Fatalf("disk launch prefers canonical tierModels, got %+v", settings)
	}
	if settings.AutoCompactWindow != 350000 || settings.SmallFastModel != "gpt-5.4-mini" {
		t.Fatalf("disk compact/helper=%+v", settings)
	}
	if !rosterSet || strings.Join(roster, ",") != "openai-apikey/gpt-5.5" {
		t.Fatalf("roster=%v set=%v", roster, rosterSet)
	}
}

func TestLoadClaudeLaunchConfigFallsBackToModelMapFamilies(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	payload := `{"claudeCode":{"modelMap":{"opus":"legacy-opus","claude-3":"intercepted","fable":"legacy-fable"}}}`
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, _, _, _, err := loadClaudeLaunchConfig(commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, Config: configPath}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if settings.Opus != "legacy-opus" || settings.Fable != "legacy-fable" || settings.Sonnet != "" {
		t.Fatalf("legacy family fallback=%+v", settings)
	}
}

func TestLoadClaudeLaunchConfigAgreesWithLiveGETProjection(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	payload := `{"claudeCode":{"tierModels":{"opus":"gpt-5.6-sol","haiku":"gpt-5.6-mini"},"modelMap":{"opus":"stale","claude-3":"intercepted"}}}`
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	disk, _, _, _, err := loadClaudeLaunchConfig(commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, Config: configPath}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	live := parseClaudeCodeGET([]byte(`{
		"tierModels":{"opus":"gpt-5.6-sol","haiku":"gpt-5.6-mini"},
		"modelMap":{"opus":"gpt-5.6-sol","haiku":"gpt-5.6-mini"}
	}`))
	if disk.Opus != live.Opus || disk.Haiku != live.Haiku || disk.Sonnet != live.Sonnet || disk.Fable != live.Fable {
		t.Fatalf("disk=%+v live=%+v", disk, live)
	}
}

func TestRunClaudeLaunchAppliesFamilyRoutesAndIgnoresRetiredControls(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	agentsDir := filepath.Join(home, "claude")
	payload := `{
		"claudeCode":{
			"enabled":true,
			"authMode":"proxy",
			"systemEnv":true,
			"fastMode":true,
			"blockedSkills":["web-search"],
			"webSearchSidecar":{"backend":"openai","model":"gpt-5.6-luna"},
			"visionSidecar":{"backend":"openai","model":"gpt-5.6-luna"},
			"autoContext":true,
			"autoCompactWindow":350000,
			"injectAgents":true,
			"smallFastModel":"gpt-5.4-mini",
			"tierModels":{"opus":"disk-opus","sonnet":"disk-sonnet","haiku":"disk-haiku","fable":"disk-fable"},
			"modelMap":{"opus":"live-opus","claude-3-opus-20240229":"intercepted"}
		},
		"fastMode":true,
		"subagentModels":["gpt-5.5"]
	}`
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", agentsDir)
	prevDo := accessDo
	t.Cleanup(func() { accessDo = prevDo })
	accessDo = func(_ string, u string, _ []byte) (int, []byte, error) {
		if strings.Contains(u, "/api/claude-code") {
			return 200, []byte(`{
				"enabled":true,
				"authMode":"proxy",
				"injectAgents":true,
				"smallFastModel":"gpt-5.4-mini",
				"autoCompactWindow":350000,
				"tierModels":{
					"opus":"disk-opus",
					"sonnet":"disk-sonnet",
					"haiku":"disk-haiku",
					"fable":"disk-fable"
				},
				"modelMap":{
					"opus":"disk-opus",
					"sonnet":"disk-sonnet",
					"haiku":"disk-haiku",
					"fable":"disk-fable"
				}
			}`), nil
		}
		if strings.Contains(u, "/v1/models") {
			return 200, []byte(`{"data":[{"id":"disk-opus","context_window":900000},{"id":"disk-haiku","context_window":128000}]}`), nil
		}
		t.Fatalf("url=%s", u)
		return 0, nil, nil
	}
	prevLaunch := launchClaude
	t.Cleanup(func() { launchClaude = prevLaunch })
	var gotEnv []string
	launchClaude = func(_ []string, env []string) error {
		gotEnv = append([]string{}, env...)
		return nil
	}
	var stdout, stderr bytes.Buffer
	code := runClaude(nil, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
		},
		spawnStart: func() error {
			t.Fatal("should not start")
			return nil
		},
	})
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	env := claude.EnvMapFromList(gotEnv)
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "disk-opus[1m]" ||
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "disk-sonnet" ||
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "disk-haiku" ||
		env["ANTHROPIC_DEFAULT_FABLE_MODEL"] != "disk-fable" {
		t.Fatalf("canonical family env=%v", env)
	}
	if env["ANTHROPIC_SMALL_FAST_MODEL"] != "disk-haiku" {
		t.Fatalf("haiku also fills helper env, got %s", env["ANTHROPIC_SMALL_FAST_MODEL"])
	}
	if env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "350000" {
		t.Fatalf("disk compact window must survive live GET, got %s", env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"])
	}
	joined := strings.Join(gotEnv, "\n")
	if strings.Contains(joined, "intercepted") || strings.Contains(joined, "claude-3-opus") {
		t.Fatalf("arbitrary interception leaked into launch env: %s", joined)
	}
	if strings.Contains(joined, "web-search") || strings.Contains(joined, "gpt-5.6-luna") {
		t.Fatalf("retired sidecar/blockedSkills leaked: %s", joined)
	}
	self := filepath.Join(agentsDir, "agents", "benes-gpt-5-5.md")
	if _, err := os.Stat(self); err != nil {
		t.Fatalf("injectAgents must sync roster defs: %v", err)
	}
}

func TestRunClaudeLaunchSkipsAgentSyncWhenInjectAgentsFalse(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	agentsDir := filepath.Join(home, "claude")
	if err := os.WriteFile(configPath, []byte(`{"claudeCode":{"enabled":true,"authMode":"proxy","injectAgents":false},"subagentModels":["gpt-5.5"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", agentsDir)
	prevDo := accessDo
	t.Cleanup(func() { accessDo = prevDo })
	accessDo = func(_ string, u string, _ []byte) (int, []byte, error) {
		if strings.Contains(u, "/api/claude-code") {
			return 200, []byte(`{"enabled":true,"authMode":"proxy","injectAgents":false}`), nil
		}
		if strings.Contains(u, "/v1/models") {
			return 200, []byte(`{"data":[]}`), nil
		}
		return 0, nil, nil
	}
	prevLaunch := launchClaude
	t.Cleanup(func() { launchClaude = prevLaunch })
	launchClaude = func([]string, []string) error { return nil }
	var stdout, stderr bytes.Buffer
	if code := runClaude(nil, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
		},
	}); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	matches, _ := filepath.Glob(filepath.Join(agentsDir, "agents", "benes-*.md"))
	if len(matches) != 0 {
		t.Fatalf("injectAgents=false must not write owned defs: %v", matches)
	}
}

func TestRunClaudeLaunchUsesDiskFamiliesWhenLiveGETFails(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"claudeCode":{"enabled":true,"authMode":"proxy","modelMap":{"opus":"legacy-opus","claude-3":"intercepted"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	prevDo := accessDo
	t.Cleanup(func() { accessDo = prevDo })
	accessDo = func(_ string, u string, _ []byte) (int, []byte, error) {
		if strings.Contains(u, "/api/claude-code") {
			return 0, nil, errors.New("down")
		}
		if strings.Contains(u, "/v1/models") {
			return 200, []byte(`{"data":[{"id":"legacy-opus","context_window":1000000}]}`), nil
		}
		return 0, nil, nil
	}
	prevLaunch := launchClaude
	t.Cleanup(func() { launchClaude = prevLaunch })
	var gotEnv []string
	launchClaude = func(_ []string, env []string) error {
		gotEnv = append([]string{}, env...)
		return nil
	}
	var stdout, stderr bytes.Buffer
	if code := runClaude(nil, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
		},
	}); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	env := claude.EnvMapFromList(gotEnv)
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "legacy-opus[1m]" {
		t.Fatalf("offline disk fallback env=%v", env)
	}
	if strings.Contains(strings.Join(gotEnv, "\n"), "intercepted") {
		t.Fatal("arbitrary map key leaked into offline launch")
	}
}

func TestRunClaudeLaunchDisablesExtendedContextWhenAutoContextFalse(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	portPath := filepath.Join(home, "runtime-port.json")
	if err := os.WriteFile(configPath, []byte(`{"claudeCode":{"enabled":true,"authMode":"proxy","autoContext":false,"tierModels":{"opus":"gpt-5.6-sol"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(portPath, []byte(`{"pid":1,"port":18080,"hostname":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	prevDo := accessDo
	t.Cleanup(func() { accessDo = prevDo })
	accessDo = func(_ string, u string, _ []byte) (int, []byte, error) {
		if strings.Contains(u, "/api/claude-code") {
			return 200, []byte(`{"enabled":true,"authMode":"proxy","autoContext":false,"tierModels":{"opus":"gpt-5.6-sol"},"modelMap":{"opus":"gpt-5.6-sol"}}`), nil
		}
		if strings.Contains(u, "/v1/models") {
			return 200, []byte(`{"data":[{"id":"gpt-5.6-sol","context_window":900000}]}`), nil
		}
		return 0, nil, nil
	}
	prevLaunch := launchClaude
	t.Cleanup(func() { launchClaude = prevLaunch })
	var gotEnv []string
	launchClaude = func(_ []string, env []string) error {
		gotEnv = append([]string{}, env...)
		return nil
	}
	var stdout, stderr bytes.Buffer
	if code := runClaude(nil, &stdout, &stderr, commandDependencies{
		resolvePaths: func(config.PathOptions) (config.Paths, error) {
			return config.Paths{Home: home, Config: configPath, RuntimePort: portPath}, nil
		},
	}); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	env := claude.EnvMapFromList(gotEnv)
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "gpt-5.6-sol" {
		t.Fatalf("autoContext=false must not mark extended context, env=%v", env)
	}
	if env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "" {
		t.Fatalf("compact leaked=%s", env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"])
	}
}
