package claude

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	benesPrefixed = regexp.MustCompile(`^benes_(?:data|admin|session)_`)
	benesHex      = regexp.MustCompile(`^benes_[0-9a-f]{40}$`)
)

type CodeSettings struct {
	Enabled            *bool
	AuthMode           string
	AlwaysEnableEffort bool
	MaxContextTokens   int
	AutoContext        *bool
	AutoCompactWindow  int
	InjectAgents       *bool
	Model              string
	SmallFastModel     string
	Opus               string
	Sonnet             string
	Haiku              string
	Fable              string
}

type BuildInput struct {
	Port           int
	Settings       CodeSettings
	OwnTokens      []string
	ContextWindows map[string]int
	Env            map[string]string
	WarnUnknown    func()
}

func IsProxyAdmissionSecret(token string, ownTokens []string) bool {
	actual := strings.TrimSpace(token)
	if actual == "" {
		return false
	}
	if benesPrefixed.MatchString(actual) || benesHex.MatchString(actual) {
		return true
	}
	for _, own := range ownTokens {
		if actual == own {
			return true
		}
	}
	return false
}

func isLoopbackHost(hostname string) bool {
	normalized := strings.TrimSuffix(strings.ToLower(hostname), ".")
	return normalized == "localhost" || normalized == "127.0.0.1" || normalized == "::1" || normalized == "[::1]"
}

func targetsLocalProxy(value string, port int) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	effectivePort := 80
	if parsed.Port() != "" {
		if n, conv := strconv.Atoi(parsed.Port()); conv == nil {
			effectivePort = n
		}
	}
	return parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname()) && effectivePort == port && parsed.User == nil
}

func setDefault(env map[string]string, name, value string) {
	if value == "" {
		return
	}
	if current, ok := env[name]; ok && current != "" {
		return
	}
	env[name] = value
}

func BuildEnv(in BuildInput) map[string]string {
	env := map[string]string{}
	for k, v := range in.Env {
		env[k] = v
	}
	if strings.TrimSpace(env["ANTHROPIC_AUTH_TOKEN"]) == ProxyMarker {
		delete(env, "ANTHROPIC_AUTH_TOKEN")
	}
	delete(env, "BENES_PRE_ANTHROPIC_ENV")
	delete(env, "BENES_NODE_LAUNCH_CONTEXT")
	setDefault(env, "ANTHROPIC_BASE_URL", "http://127.0.0.1:"+strconv.Itoa(in.Port))
	if existing := env["ANTHROPIC_BASE_URL"]; existing != "" {
		if parsed, err := url.Parse(existing); err == nil {
			effectivePort := 80
			if parsed.Port() != "" {
				if n, conv := strconv.Atoi(parsed.Port()); conv == nil {
					effectivePort = n
				}
			}
			if parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname()) && effectivePort != in.Port {
				env["ANTHROPIC_BASE_URL"] = "http://127.0.0.1:" + strconv.Itoa(in.Port)
			}
		}
	}
	if IsProxyAdmissionSecret(env["ANTHROPIC_API_KEY"], in.OwnTokens) {
		delete(env, "ANTHROPIC_API_KEY")
	}
	hasUserAPIKey := strings.TrimSpace(env["ANTHROPIC_API_KEY"]) != ""
	inheritedTokenIsOurs := IsProxyAdmissionSecret(env["ANTHROPIC_AUTH_TOKEN"], in.OwnTokens)
	local := targetsLocalProxy(env["ANTHROPIC_BASE_URL"], in.Port)
	if inheritedTokenIsOurs && (!local || hasUserAPIKey) {
		delete(env, "ANTHROPIC_AUTH_TOKEN")
	}
	if local && !hasUserAPIKey && len(in.OwnTokens) > 0 {
		setDefault(env, "ANTHROPIC_AUTH_TOKEN", in.OwnTokens[0])
	}
	detection := DetectAuth(env, in.OwnTokens)
	resolved := ResolveAuthMode(in.Settings.AuthMode, detection)
	if env["ANTHROPIC_AUTH_TOKEN"] == "" && !hasUserAPIKey && local && resolved.MarkerMode == "proxy" {
		env["ANTHROPIC_AUTH_TOKEN"] = ProxyMarker
	}
	finalToken := env["ANTHROPIC_AUTH_TOKEN"]
	hostOwns := local && !hasUserAPIKey && (strings.TrimSpace(finalToken) == ProxyMarker || IsProxyAdmissionSecret(finalToken, in.OwnTokens))
	if resolved.Origin == "auto-unknown" && in.WarnUnknown != nil {
		in.WarnUnknown()
	}
	setDefault(env, "CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY", "1")
	if hostOwns {
		setDefault(env, "CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST", "1")
	}
	if in.Settings.AlwaysEnableEffort {
		setDefault(env, "CLAUDE_CODE_ALWAYS_ENABLE_EFFORT", "1")
	}
	if in.Settings.MaxContextTokens > 0 {
		setDefault(env, "CLAUDE_CODE_MAX_CONTEXT_TOKENS", strconv.Itoa(in.Settings.MaxContextTokens))
		setDefault(env, "DISABLE_COMPACT", "1")
	}
	userAuto := ""
	if v := in.Env["CLAUDE_CODE_AUTO_COMPACT_WINDOW"]; v != "" {
		userAuto = v
	}
	auto := ResolveAutoContext(in.Settings, userAuto)
	if auto.Enabled {
		setDefault(env, "CLAUDE_CODE_AUTO_COMPACT_WINDOW", strconv.Itoa(auto.CompactWindow))
	}
	for name, value := range EffectiveModelEnv(in.Settings, in.ContextWindows, auto) {
		setDefault(env, name, value)
	}
	return env
}

const (
	oneMillion         = 1_000_000
	autoCompactDefault = 829_800
	autoContextFloor   = 200_000
	autoCompactMin     = 100_000
	autoCompactMax     = oneMillion
)

type AutoContext struct {
	Enabled       bool
	CompactWindow int
}

func ResolveAutoContext(settings CodeSettings, envOverride string) AutoContext {
	off := AutoContext{Enabled: false, CompactWindow: autoCompactDefault}
	if settings.AutoContext != nil && !*settings.AutoContext {
		return off
	}
	if settings.MaxContextTokens > 0 {
		return off
	}
	if envOverride != "" {
		n, err := strconv.Atoi(envOverride)
		if err != nil || n < autoCompactMin || n > autoCompactMax {
			return off
		}
		return AutoContext{Enabled: true, CompactWindow: n}
	}
	window := autoCompactDefault
	if settings.AutoCompactWindow >= autoCompactMin && settings.AutoCompactWindow <= autoCompactMax {
		window = settings.AutoCompactWindow
	}
	return AutoContext{Enabled: true, CompactWindow: window}
}

func shouldMarkOneMillion(window int, auto AutoContext) bool {
	if window <= 0 {
		return false
	}
	if window >= oneMillion {
		return true
	}
	return auto.Enabled && window > autoContextFloor && window >= auto.CompactWindow
}

func withOneMillionMarker(selector string, windows map[string]int, auto AutoContext) string {
	if selector == "" {
		return ""
	}
	if strings.HasSuffix(strings.ToLower(selector), "[1m]") {
		return selector
	}
	bare := strings.TrimSuffix(strings.TrimSuffix(selector, "[1m]"), "[1M]")
	window := 0
	if windows != nil {
		window = windows[bare]
		if window == 0 {
			window = windows[selector]
		}
	}
	if shouldMarkOneMillion(window, auto) {
		return selector + "[1m]"
	}
	return selector
}

func EffectiveModelEnv(settings CodeSettings, windows map[string]int, auto AutoContext) map[string]string {
	out := map[string]string{}
	set := func(name, value string) {
		marked := withOneMillionMarker(value, windows, auto)
		if marked != "" {
			out[name] = marked
		}
	}
	set("ANTHROPIC_MODEL", settings.Model)
	set("ANTHROPIC_DEFAULT_OPUS_MODEL", settings.Opus)
	set("ANTHROPIC_DEFAULT_SONNET_MODEL", settings.Sonnet)
	set("ANTHROPIC_DEFAULT_FABLE_MODEL", settings.Fable)
	haiku := settings.Haiku
	if haiku == "" {
		haiku = settings.SmallFastModel
	}
	set("ANTHROPIC_DEFAULT_HAIKU_MODEL", haiku)
	set("ANTHROPIC_SMALL_FAST_MODEL", haiku)
	return out
}

func EnvList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func EnvMapFromList(list []string) map[string]string {
	out := map[string]string{}
	for _, item := range list {
		k, v, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		out[k] = v
	}
	return out
}
