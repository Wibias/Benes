// Package claudedesktop implements the truthful Benes runtime contract for the
// Claude Desktop harness.
//
// Claude Desktop's supported local native configuration is a JSON document
// whose documented third-party surface is `mcpServers`. That document has no
// supported key for inference routing: it cannot select a model, point the
// desktop application at another base URL, or assign Opus/Sonnet/Haiku/Fable
// routes. This package models exactly what Benes may own on that surface and
// refuses everything else rather than fabricating a projection.
package claudedesktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ClientID is the Harness identity this contract serves.
const ClientID = "claude-desktop"

// nativeConfigFile is the documented Claude Desktop configuration file name on
// every supported host.
const nativeConfigFile = "claude_desktop_config.json"

// mcpServersKey is the documented root key holding MCP server definitions.
const mcpServersKey = "mcpServers"

// ManagedEntryName is the exact mcpServers entry Benes owns. Benes either owns
// this whole entry or none of the document; there is no partial ownership.
const ManagedEntryName = "benes"

// HostSupported reports whether Benes may manage Claude Desktop's native
// configuration on host. Claude Desktop ships for Windows and macOS only, so
// every other host fails closed without touching the filesystem.
func HostSupported(host string) bool {
	switch normalizeHost(host) {
	case "windows", "darwin":
		return true
	default:
		return false
	}
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		host = runtime.GOOS
	}
	return host
}

func getenv(env map[string]string, key string) string {
	if env != nil {
		if value, ok := env[key]; ok {
			return strings.TrimSpace(value)
		}
	}
	return strings.TrimSpace(os.Getenv(key))
}

// ConfigCandidates returns every Claude Desktop configuration file location
// Benes knows for host, most specific first. Only the first entry is ever
// managed; the remaining candidates exist so tooling can reveal or inspect a
// config without re-deriving platform paths of its own.
func ConfigCandidates(host, home string, env map[string]string) []string {
	if home == "" {
		home = userHome()
	}
	switch normalizeHost(host) {
	case "windows":
		roaming := getenv(env, "APPDATA")
		if roaming == "" && home != "" {
			roaming = filepath.Join(home, "AppData", "Roaming")
		}
		if roaming == "" {
			return nil
		}
		return []string{filepath.Join(roaming, "Claude", nativeConfigFile)}
	case "darwin":
		if home == "" {
			return nil
		}
		return []string{filepath.Join(home, "Library", "Application Support", "Claude", nativeConfigFile)}
	default:
		return nil
	}
}

// ConfigPath returns the single native configuration target Benes may manage on
// host. Unsupported hosts return ErrUnsupportedHost so callers fail closed
// before touching the filesystem.
func ConfigPath(host, home string, env map[string]string) (string, error) {
	candidates := ConfigCandidates(host, home, env)
	if len(candidates) == 0 {
		return "", ErrUnsupportedHost
	}
	return candidates[0], nil
}

// ConfigCandidatesForReveal returns every known Claude Desktop configuration
// location in a stable order, independent of the running host.
//
// It exists for allowlisted path reveal, where a human asked to see a known
// file. It never selects a managed target: managed configuration is host
// specific and always resolves through ConfigPath.
func ConfigCandidatesForReveal(home string, env map[string]string) []string {
	out := ConfigCandidates("windows", home, env)
	return append(out, ConfigCandidates("darwin", home, env)...)
}

// AppDataDirs returns the Claude Desktop application data directories Benes
// knows for host. These directories hold the native configuration file and are
// the directory-level evidence of an installation.
func AppDataDirs(host, home string, env map[string]string) []string {
	candidates := ConfigCandidates(host, home, env)
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, filepath.Dir(candidate))
	}
	return out
}

// appCandidates returns executable or application-bundle paths that evidence a
// Claude Desktop installation on host.
func appCandidates(host, home string, env map[string]string) []string {
	switch normalizeHost(host) {
	case "windows":
		local := getenv(env, "LOCALAPPDATA")
		if local == "" && home != "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		if local == "" {
			return nil
		}
		return []string{
			filepath.Join(local, "AnthropicClaude", "claude.exe"),
			filepath.Join(local, "AnthropicClaude", "Claude.exe"),
			filepath.Join(local, "Programs", "Claude", "Claude.exe"),
		}
	case "darwin":
		return []string{"/Applications/Claude.app"}
	default:
		return nil
	}
}

// DetectInstall reports the strongest local evidence that Claude Desktop is
// installed on host, or "" when no evidence exists.
//
// Evidence is deliberately limited to Claude Desktop artifacts. The desktop
// application is never inferred from a bare `claude` command on PATH, because
// that name resolves to the Claude Code CLI on hosts that have no desktop
// installation. The application data directory is accepted as evidence because
// real installations place the desktop application outside the legacy
// executable candidates while still creating that directory.
func DetectInstall(host, home string, env map[string]string, lookPath func(string) (string, error)) string {
	if !HostSupported(host) {
		return ""
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	for _, candidate := range appCandidates(host, home, env) {
		if pathExists(candidate) {
			return candidate
		}
	}
	if path, err := lookPath(ClientID); err == nil {
		if trimmed := strings.TrimSpace(path); trimmed != "" && pathExists(trimmed) {
			return trimmed
		}
	}
	for _, dir := range AppDataDirs(host, home, env) {
		if isDir(dir) {
			return dir
		}
	}
	return ""
}

func userHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func pathExists(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	_, err := os.Lstat(path)
	return err == nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
