package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type HomeSource string

const (
	HomeSourceBenesEnv HomeSource = "BENES_HOME"
	HomeSourceDefault  HomeSource = "default"
)

type PathOptions struct {
	HomeDir string
	Env     map[string]string
}

type Paths struct {
	Home        string
	Source      HomeSource
	Config      string
	PID         string
	RuntimePort string
}

func ResolveHome(options PathOptions) (string, HomeSource, error) {
	home := options.HomeDir
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", "", fmt.Errorf("resolve user home: %w", err)
		}
	}
	env := options.Env
	if env == nil {
		env = environmentMap()
	}
	if raw := strings.TrimSpace(env["BENES_HOME"]); raw != "" {
		path, err := absoluteUserPath(raw, home)
		return path, HomeSourceBenesEnv, err
	}
	return filepath.Join(home, ".benes"), HomeSourceDefault, nil
}

func ResolvePaths(options PathOptions) (Paths, error) {
	home, source, err := ResolveHome(options)
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		Home:        home,
		Source:      source,
		Config:      filepath.Join(home, "config.json"),
		PID:         filepath.Join(home, "benes.pid"),
		RuntimePort: filepath.Join(home, "runtime-port.json"),
	}, nil
}

func ExpandUserPath(raw, home string) string {
	if raw == "~" {
		return home
	}
	if strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, `~\`) {
		return filepath.Join(home, raw[2:])
	}
	return raw
}

func absoluteUserPath(raw, home string) (string, error) {
	expanded := ExpandUserPath(raw, home)
	path, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", raw, err)
	}
	return filepath.Clean(path), nil
}

type CodexHomeOptions struct {
	HomeDir     string
	Env         map[string]string
	Platform    string
	Release     string
	ProcVersion string
	UsersRoot   string
	WSLConf     string

	Stat         func(string) (os.FileInfo, error)
	ReadDir      func(string) ([]os.DirEntry, error)
	EvalSymlinks func(string) (string, error)
}

func ResolveCodexHome(options CodexHomeOptions) (string, error) {
	home := options.HomeDir
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home: %w", err)
		}
	}
	env := options.Env
	if env == nil {
		env = environmentMap()
	}
	stat := options.Stat
	if stat == nil {
		stat = os.Stat
	}
	eval := options.EvalSymlinks
	if eval == nil {
		eval = filepath.EvalSymlinks
	}

	if raw := strings.TrimSpace(env["CODEX_HOME"]); raw != "" {
		path, err := absoluteUserPath(raw, home)
		if err != nil {
			return "", err
		}
		info, err := stat(path)
		if err != nil {
			return "", fmt.Errorf("CODEX_HOME points to %q but that path could not be read: %w", raw, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("CODEX_HOME points to %q but that path is not a directory", raw)
		}
		real, err := eval(path)
		if err != nil {
			return "", fmt.Errorf("resolve CODEX_HOME %q: %w", raw, err)
		}
		return real, nil
	}

	defaultHome := filepath.Join(home, ".codex")
	if _, err := stat(filepath.Join(defaultHome, "config.toml")); err == nil {
		return defaultHome, nil
	}
	detected := findWSLWindowsCodexHome(options, env, stat, eval)
	if detected != "" {
		return detected, nil
	}
	return defaultHome, nil
}

func findWSLWindowsCodexHome(options CodexHomeOptions, env map[string]string, stat func(string) (os.FileInfo, error), eval func(string) (string, error)) string {
	platform := options.Platform
	if platform == "" {
		platform = runtime.GOOS
	}
	if platform != "linux" {
		return ""
	}
	signature := strings.ToLower(options.Release + "\n" + options.ProcVersion)
	if strings.TrimSpace(env["WSL_DISTRO_NAME"]) == "" && strings.TrimSpace(env["WSL_INTEROP"]) == "" && !strings.Contains(signature, "microsoft") && !strings.Contains(signature, "wsl") {
		return ""
	}

	usersRoot := options.UsersRoot
	if usersRoot == "" {
		usersRoot = filepath.Join(wslAutomountRoot(options.WSLConf), "c", "Users")
	}
	readDir := options.ReadDir
	if readDir == nil {
		readDir = os.ReadDir
	}
	entries, err := readDir(usersRoot)
	if err != nil {
		return ""
	}
	ignored := map[string]bool{"Default": true, "Default User": true, "Public": true, "All Users": true}
	candidates := make([]string, 0, 2)
	for _, entry := range entries {
		if ignored[entry.Name()] {
			continue
		}
		candidate := filepath.Join(usersRoot, entry.Name(), ".codex")
		if _, err := stat(filepath.Join(candidate, "config.toml")); err != nil {
			continue
		}
		info, err := stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		real, err := eval(candidate)
		if err != nil {
			continue
		}
		candidates = append(candidates, real)
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return ""
	}

	if profile := strings.TrimSpace(env["USERPROFILE"]); profile != "" {
		normalized := strings.ReplaceAll(profile, `\`, "/")
		parts := strings.Split(strings.TrimSuffix(normalized, "/"), "/")
		if len(parts) == 3 && len(parts[0]) == 2 && parts[0][1] == ':' && strings.EqualFold(parts[1], "Users") {
			drive := strings.ToLower(parts[0][:1])
			explicit := ""
			if options.UsersRoot != "" {
				explicit = filepath.Join(options.UsersRoot, parts[2], ".codex")
			} else {
				explicit = filepath.Join(wslAutomountRoot(options.WSLConf), drive, "Users", parts[2], ".codex")
			}
			if real, err := eval(explicit); err == nil {
				for _, candidate := range candidates {
					if candidate == real {
						return candidate
					}
				}
			}
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return ""
}

func wslAutomountRoot(content string) string {
	root := "/mnt"
	section := ""
	for _, raw := range strings.Split(content, "\n") {
		line := raw
		if index := strings.IndexAny(line, "#;"); index >= 0 {
			line = line[:index]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")))
			continue
		}
		if section != "automount" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "root") {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if path.IsAbs(value) {
			root = path.Clean(value)
		}
	}
	return root
}

func environmentMap() map[string]string {
	result := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}
