package claude

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	ProxyMarker      = "benes-proxy"
	keychainService  = "Claude Code-credentials"
	keychainNotFound = 44
	keychainTimeout  = 1500 * time.Millisecond
)

type Presence string

const (
	PresencePresent Presence = "present"
	PresenceAbsent  Presence = "absent"
	PresenceUnknown Presence = "unknown"
)

type DetectResult struct {
	Presence         Presence
	FoundBy          string
	StaleProxyMarker bool
}

type AuthMode struct {
	MarkerMode string
	Origin     string
}

func ClaudeConfigDir(env map[string]string) string {
	if dir := strings.TrimSpace(env["CLAUDE_CONFIG_DIR"]); dir != "" {
		return dir
	}
	return filepath.Join(homeDir(env), ".claude")
}

func homeDir(env map[string]string) string {
	if v := strings.TrimSpace(env["HOME"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(env["USERPROFILE"]); v != "" {
		return v
	}
	h, _ := os.UserHomeDir()
	return h
}

func DetectAuth(env map[string]string, ownTokens []string) DetectResult {
	sources := []struct {
		id       string
		presence Presence
	}{
		{"claude-json-oauth", detectClaudeJSON(env)},
		{"claude-credentials-file", detectCredentials(env)},
		{"macos-keychain", detectKeychain()},
		{"exported-env", detectExportedEnv(env, ownTokens)},
	}
	stale := strings.TrimSpace(env["ANTHROPIC_AUTH_TOKEN"]) == ProxyMarker
	for _, source := range sources {
		if source.presence == PresencePresent {
			return DetectResult{Presence: PresencePresent, FoundBy: source.id, StaleProxyMarker: stale}
		}
	}
	for _, source := range sources {
		if source.presence == PresenceUnknown {
			return DetectResult{Presence: PresenceUnknown, StaleProxyMarker: stale}
		}
	}
	return DetectResult{Presence: PresenceAbsent, StaleProxyMarker: stale}
}

func ResolveAuthMode(authMode string, detection DetectResult) AuthMode {
	switch strings.ToLower(strings.TrimSpace(authMode)) {
	case "proxy":
		return AuthMode{MarkerMode: "proxy", Origin: "manual"}
	case "subscription":
		return AuthMode{MarkerMode: "subscription", Origin: "manual"}
	}
	switch detection.Presence {
	case PresencePresent:
		return AuthMode{MarkerMode: "subscription", Origin: "auto-present"}
	case PresenceAbsent:
		return AuthMode{MarkerMode: "proxy", Origin: "auto-absent"}
	default:
		return AuthMode{MarkerMode: "subscription", Origin: "auto-unknown"}
	}
}

func detectClaudeJSON(env map[string]string) Presence {
	path := filepath.Join(homeDir(env), ".claude.json")
	if strings.TrimSpace(env["CLAUDE_CONFIG_DIR"]) != "" {
		path = filepath.Join(ClaudeConfigDir(env), "..", ".claude.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return PresenceAbsent
		}
		return PresenceUnknown
	}
	var parsed map[string]any
	if json.Unmarshal(raw, &parsed) != nil {
		return PresenceUnknown
	}
	account, _ := parsed["oauthAccount"].(map[string]any)
	if account == nil {
		return PresenceAbsent
	}
	email, _ := account["emailAddress"].(string)
	if strings.TrimSpace(email) != "" {
		return PresencePresent
	}
	return PresenceAbsent
}

func detectCredentials(env map[string]string) Presence {
	path := filepath.Join(ClaudeConfigDir(env), ".credentials.json")
	st, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return PresenceAbsent
		}
		return PresenceUnknown
	}
	if st.IsDir() {
		return PresenceAbsent
	}
	return PresencePresent
}

func detectKeychain() Presence {
	if runtime.GOOS != "darwin" {
		return PresenceAbsent
	}
	cmd := exec.Command("security", "find-generic-password", "-s", keychainService)
	err := cmd.Start()
	if err != nil {
		return PresenceUnknown
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return PresencePresent
		}
		if exit, ok := err.(*exec.ExitError); ok {
			if exit.ExitCode() == keychainNotFound {
				return PresenceAbsent
			}
		}
		return PresenceUnknown
	case <-time.After(keychainTimeout):
		_ = cmd.Process.Kill()
		return PresenceUnknown
	}
}

func detectExportedEnv(env map[string]string, ownTokens []string) Presence {
	own := map[string]struct{}{ProxyMarker: {}}
	for _, token := range ownTokens {
		if token != "" {
			own[token] = struct{}{}
		}
	}
	if key := strings.TrimSpace(env["ANTHROPIC_API_KEY"]); key != "" {
		if _, isOwn := own[key]; !isOwn {
			return PresencePresent
		}
	}
	if token := strings.TrimSpace(env["ANTHROPIC_AUTH_TOKEN"]); token != "" {
		if _, isOwn := own[token]; !isOwn {
			return PresencePresent
		}
	}
	return PresenceAbsent
}
