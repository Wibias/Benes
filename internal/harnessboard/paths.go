package harnessboard

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/claudedesktop"
	"github.com/Wibias/Benes/internal/integrations"
)

var osStat = os.Stat
var UserHome = os.UserHomeDir

func getenv(env map[string]string, key string) string {
	if env != nil {
		if v, ok := env[key]; ok {
			return strings.TrimSpace(v)
		}
	}
	return strings.TrimSpace(os.Getenv(key))
}

func existingFile(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	info, err := osStat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	return path
}

func existingDir(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	info, err := osStat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	return path
}

func firstExisting(paths ...string) string {
	for _, path := range paths {
		if found := existingFile(path); found != "" {
			return found
		}
		if found := existingDir(path); found != "" {
			return found
		}
	}
	return ""
}

func look(id string) string {
	for _, name := range lookNames(id) {
		path, err := LookPath(name)
		if err == nil && strings.TrimSpace(path) != "" {
			if found := existingFile(path); found != "" {
				return found
			}
			return path
		}
	}
	return ""
}

func fileDetect(id string, env map[string]string) (detect string, config string) {
	home, _ := UserHome()
	paths, err := integrations.ResolvePaths(id, home, env)
	if err != nil {
		return look(id), ""
	}
	config = paths.ConfigPath
	if found := look(id); found != "" {
		return found, config
	}
	if dir := existingDir(paths.DetectDir); dir != "" {
		return dir, config
	}
	return paths.DetectDir, config
}

func nativeDetect(id, codexHome string, env map[string]string) string {
	if found := look(id); found != "" && id != "claude-desktop" {
		return found
	}
	home, _ := UserHome()
	switch id {
	case "claude-desktop":
		return claudeDesktopDetect(home, env)
	case "claude":
		if dir := existingDir(filepath.Join(home, ".claude")); dir != "" {
			return dir
		}
		return look(id)
	case "codex":
		if found := look(id); found != "" {
			return found
		}
		if dir := existingDir(codexHome); dir != "" {
			return dir
		}
		return strings.TrimSpace(codexHome)
	case "grok":
		return grokDetect(home, env)
	default:
		return look(id)
	}
}

// claudeDesktopDetect reports the strongest Claude Desktop installation
// evidence. The desktop application is never inferred from a bare `claude`
// command, which resolves to the Claude Code CLI on hosts with no desktop
// installation.
func claudeDesktopDetect(home string, env map[string]string) string {
	return claudedesktop.DetectInstall("", home, env, LookPath)
}

func grokDetect(home string, env map[string]string) string {
	if found := look("grok"); found != "" {
		return found
	}
	local := getenv(env, "LOCALAPPDATA")
	if local == "" && home != "" {
		local = filepath.Join(home, "AppData", "Local")
	}
	return firstExisting(
		filepath.Join(local, "Grok", "Grok.exe"),
		"/Applications/Grok.app",
	)
}

func logPathFor(id, detectPath, codexHome string, env map[string]string) string {
	home, _ := UserHome()
	switch id {
	case "claude-desktop":
		roaming := getenv(env, "APPDATA")
		if roaming == "" && home != "" {
			roaming = filepath.Join(home, "AppData", "Roaming")
		}
		return firstExisting(
			filepath.Join(roaming, "Claude", "logs"),
			filepath.Join(home, "Library", "Logs", "Claude"),
			filepath.Join(home, ".config", "Claude", "logs"),
		)
	case "claude":
		return firstExisting(
			filepath.Join(home, ".claude", "logs"),
			filepath.Join(home, ".claude", "debug"),
		)
	case "codex":
		return firstExisting(
			filepath.Join(codexHome, "log"),
			filepath.Join(codexHome, "logs"),
			filepath.Join(home, ".codex", "log"),
		)
	default:
		if detectPath == "" {
			return ""
		}
		dir := detectPath
		if info, err := osStat(detectPath); err == nil && !info.IsDir() {
			dir = filepath.Dir(detectPath)
		}
		if fileClient(id) {
			paths, err := integrations.ResolvePaths(id, home, env)
			if err == nil && existingDir(paths.DetectDir) != "" {
				dir = paths.DetectDir
			}
		}
		return firstExisting(filepath.Join(dir, "logs"), filepath.Join(dir, "log"))
	}
}

func maybeString(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func ResetHooks() {
	LookPath = lookPathOS
	ListProcesses = listProcessesOS
	UserHome = os.UserHomeDir
	OpenPath = openPathOS
	osStat = os.Stat
}
