package claude

import (
	"strings"
	"testing"
)

func TestResolveAuthModeManualAndAutoOrigins(t *testing.T) {
	proxy := ResolveAuthMode("proxy", DetectResult{Presence: PresencePresent})
	if proxy.MarkerMode != "proxy" || proxy.Origin != "manual" {
		t.Fatalf("proxy=%+v", proxy)
	}
	sub := ResolveAuthMode("subscription", DetectResult{Presence: PresenceAbsent})
	if sub.MarkerMode != "subscription" || sub.Origin != "manual" {
		t.Fatalf("subscription=%+v", sub)
	}
	present := ResolveAuthMode("auto", DetectResult{Presence: PresencePresent})
	if present.MarkerMode != "subscription" || present.Origin != "auto-present" {
		t.Fatalf("auto-present=%+v", present)
	}
	absent := ResolveAuthMode("", DetectResult{Presence: PresenceAbsent})
	if absent.MarkerMode != "proxy" || absent.Origin != "auto-absent" {
		t.Fatalf("auto-absent=%+v", absent)
	}
	unknown := ResolveAuthMode("AUTO", DetectResult{Presence: PresenceUnknown})
	if unknown.MarkerMode != "subscription" || unknown.Origin != "auto-unknown" {
		t.Fatalf("auto-unknown=%+v", unknown)
	}
}

func TestBuildEnvAuthModeLaunchEffect(t *testing.T) {
	proxy := BuildEnv(BuildInput{Port: 23100, Settings: CodeSettings{AuthMode: "proxy"}})
	if proxy["ANTHROPIC_AUTH_TOKEN"] != ProxyMarker {
		t.Fatalf("proxy token=%s", proxy["ANTHROPIC_AUTH_TOKEN"])
	}
	if proxy["CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST"] != "1" {
		t.Fatal("proxy must claim host-managed auth")
	}
	sub := BuildEnv(BuildInput{Port: 23100, Settings: CodeSettings{AuthMode: "subscription"}})
	if sub["ANTHROPIC_AUTH_TOKEN"] != "" {
		t.Fatalf("subscription injected marker=%s", sub["ANTHROPIC_AUTH_TOKEN"])
	}
	if sub["CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST"] == "1" {
		t.Fatal("subscription must not claim host-managed auth")
	}
	if sub["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:23100" {
		t.Fatalf("subscription still targets the local proxy, got %s", sub["ANTHROPIC_BASE_URL"])
	}
}

func TestResolveAutoContextDefaultBoundsAndLegacyMaxContext(t *testing.T) {
	def := ResolveAutoContext(CodeSettings{}, "")
	if !def.Enabled || def.CompactWindow != autoCompactDefault {
		t.Fatalf("default=%+v want enabled %d", def, autoCompactDefault)
	}
	off := ResolveAutoContext(CodeSettings{AutoContext: boolPtr(false)}, "")
	if off.Enabled {
		t.Fatal("explicit autoContext=false must disable compact injection")
	}
	legacy := ResolveAutoContext(CodeSettings{AutoContext: boolPtr(true), MaxContextTokens: 200_000}, "")
	if legacy.Enabled {
		t.Fatal("legacy maxContextTokens must disable auto-context")
	}
	custom := ResolveAutoContext(CodeSettings{AutoCompactWindow: 350_000}, "")
	if !custom.Enabled || custom.CompactWindow != 350_000 {
		t.Fatalf("in-range window=%+v", custom)
	}
	oob := ResolveAutoContext(CodeSettings{AutoCompactWindow: 99_999}, "")
	if !oob.Enabled || oob.CompactWindow != autoCompactDefault {
		t.Fatalf("out-of-range stored window must fall back to default, got %+v", oob)
	}
	env := ResolveAutoContext(CodeSettings{AutoCompactWindow: 350_000}, "250000")
	if !env.Enabled || env.CompactWindow != 250_000 {
		t.Fatalf("valid env override=%+v", env)
	}
	badEnv := ResolveAutoContext(CodeSettings{}, "not-a-number")
	if badEnv.Enabled {
		t.Fatal("invalid CLAUDE_CODE_AUTO_COMPACT_WINDOW must disable auto compact")
	}
}

func TestBuildEnvAutoContextAndCompactWindowRuntime(t *testing.T) {
	def := BuildEnv(BuildInput{Port: 23100, Settings: CodeSettings{AuthMode: "proxy"}})
	if def["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "829800" {
		t.Fatalf("default compact=%s", def["CLAUDE_CODE_AUTO_COMPACT_WINDOW"])
	}
	off := BuildEnv(BuildInput{
		Port:     23100,
		Settings: CodeSettings{AuthMode: "proxy", AutoContext: boolPtr(false)},
	})
	if off["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "" {
		t.Fatalf("disabled autoContext leaked compact=%s", off["CLAUDE_CODE_AUTO_COMPACT_WINDOW"])
	}
	legacy := BuildEnv(BuildInput{
		Port:     23100,
		Settings: CodeSettings{AuthMode: "proxy", MaxContextTokens: 180_000},
	})
	if legacy["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "180000" || legacy["DISABLE_COMPACT"] != "1" {
		t.Fatalf("legacy max-context env=%v", legacy)
	}
	if legacy["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "" {
		t.Fatal("legacy max-context must not also inject auto compact")
	}
}

func TestEffectiveModelEnvMarksOneMillionWhenWindowsQualify(t *testing.T) {
	auto := AutoContext{Enabled: true, CompactWindow: autoCompactDefault}
	env := EffectiveModelEnv(CodeSettings{
		Model:  "gpt-5.6-sol",
		Opus:   "gpt-5.6-sol",
		Sonnet: "short",
	}, map[string]int{"gpt-5.6-sol": 1_000_000, "short": 128_000}, auto)
	if env["ANTHROPIC_MODEL"] != "gpt-5.6-sol[1m]" || env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "gpt-5.6-sol[1m]" {
		t.Fatalf("1m mark missing: %v", env)
	}
	if env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "short" {
		t.Fatalf("small window must stay unmarked: %v", env)
	}
	disabled := EffectiveModelEnv(CodeSettings{Model: "mid"}, map[string]int{"mid": 900_000}, AutoContext{Enabled: false, CompactWindow: autoCompactDefault})
	if disabled["ANTHROPIC_MODEL"] != "mid" {
		t.Fatalf("disabled autoContext must not add [1m]: %v", disabled)
	}
}

func TestEffectiveModelEnvHelperAndFamilyRoutes(t *testing.T) {
	helperOnly := EffectiveModelEnv(CodeSettings{SmallFastModel: "gpt-5.4-mini"}, nil, AutoContext{})
	if helperOnly["ANTHROPIC_SMALL_FAST_MODEL"] != "gpt-5.4-mini" || helperOnly["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "gpt-5.4-mini" {
		t.Fatalf("helper fallback=%v", helperOnly)
	}
	haikuWins := EffectiveModelEnv(CodeSettings{
		SmallFastModel: "gpt-5.4-mini",
		Haiku:          "gpt-5.6-mini",
	}, nil, AutoContext{})
	if haikuWins["ANTHROPIC_SMALL_FAST_MODEL"] != "gpt-5.6-mini" || haikuWins["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "gpt-5.6-mini" {
		t.Fatalf("explicit haiku must win helper fallback=%v", haikuWins)
	}
	empty := EffectiveModelEnv(CodeSettings{}, nil, AutoContext{})
	if empty["ANTHROPIC_SMALL_FAST_MODEL"] != "" || empty["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "" {
		t.Fatalf("unset helper must omit haiku env=%v", empty)
	}
	families := EffectiveModelEnv(CodeSettings{
		Opus:   "gpt-5.6-sol",
		Sonnet: "gpt-5.6-sol",
		Haiku:  "gpt-5.6-mini",
		Fable:  "gpt-5.6-terra",
		Model:  "openai-apikey/gpt-5.5",
	}, nil, AutoContext{})
	if families["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "gpt-5.6-sol" ||
		families["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "gpt-5.6-sol" ||
		families["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "gpt-5.6-mini" ||
		families["ANTHROPIC_DEFAULT_FABLE_MODEL"] != "gpt-5.6-terra" ||
		families["ANTHROPIC_MODEL"] != "openai-apikey/gpt-5.5" {
		t.Fatalf("family env=%v", families)
	}
	joined := ""
	for name := range families {
		joined += name + "\n"
	}
	if strings.Contains(joined, "INTERCEPT") || strings.Contains(joined, "claude-3") {
		t.Fatalf("family env must not invent interception slots: %v", families)
	}
}

func TestContextWindowsFromModelsJSONIndexesFullAndBareIDs(t *testing.T) {
	got := ContextWindowsFromModelsJSON([]byte(`{
		"data":[
			{"id":"openai-apikey/gpt-5.6-sol","context_window":1000000},
			{"id":"short","context_window":128000},
			{"id":"skip","context_window":0}
		]
	}`))
	if got["openai-apikey/gpt-5.6-sol"] != 1_000_000 || got["gpt-5.6-sol"] != 1_000_000 {
		t.Fatalf("%v", got)
	}
	if got["short"] != 128_000 || got["skip"] != 0 {
		t.Fatalf("%v", got)
	}
}

func TestBuildEnvMarksEligibleModelsWhenAutoContextEnabled(t *testing.T) {
	windows := map[string]int{"gpt-5.6-sol": 900_000, "gpt-5.6-mini": 128_000}
	on := BuildEnv(BuildInput{
		Port:           23100,
		Settings:       CodeSettings{AuthMode: "proxy", Opus: "gpt-5.6-sol", Haiku: "gpt-5.6-mini"},
		ContextWindows: windows,
	})
	if on["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "gpt-5.6-sol[1m]" {
		t.Fatalf("eligible model must get [1m], env=%v", on)
	}
	if on["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "gpt-5.6-mini" {
		t.Fatalf("ineligible model must stay unmarked, env=%v", on)
	}
	off := BuildEnv(BuildInput{
		Port:           23100,
		Settings:       CodeSettings{AuthMode: "proxy", AutoContext: boolPtr(false), Opus: "gpt-5.6-sol"},
		ContextWindows: windows,
	})
	if off["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "gpt-5.6-sol" {
		t.Fatalf("autoContext=false must not mark extended-context, env=%v", off)
	}
	if off["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "" {
		t.Fatal("autoContext=false must also omit compact window")
	}
}

func TestBuildEnvDoesNotEmitRetiredClaudeControls(t *testing.T) {
	env := BuildEnv(BuildInput{
		Port: 23100,
		Settings: CodeSettings{
			AuthMode:       "proxy",
			SmallFastModel: "gpt-5.4-mini",
			Opus:           "gpt-5.6-sol",
		},
	})
	for _, name := range []string{
		"BENES_SYSTEM_ENV",
		"CLAUDE_CODE_SYSTEM_ENV",
		"CLAUDE_CODE_FAST_MODE",
		"OPENAI_SERVICE_TIER",
		"BENES_BLOCKED_SKILLS",
		"CLAUDE_CODE_BLOCKED_SKILLS",
	} {
		if env[name] != "" {
			t.Fatalf("retired control leaked %s=%s", name, env[name])
		}
	}
	if _, ok := env["ANTHROPIC_DEFAULT_OPUS_MODEL"]; !ok {
		t.Fatal("supported family route missing")
	}
}
