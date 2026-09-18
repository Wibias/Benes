package harnessboard

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Wibias/Benes/internal/claudedesktop"
)

// RevealTarget selects which allowlisted harness path to open.
type RevealTarget string

const (
	RevealLog    RevealTarget = "log"
	RevealConfig RevealTarget = "config"
)

var (
	ErrUnknownClient     = errors.New("unknown harness")
	ErrUnknownTarget     = errors.New("unknown reveal target")
	ErrPathUnavailable   = errors.New("path unavailable")
	ErrRevealUnsupported = errors.New("reveal unsupported")
)

// OpenPath opens path in the platform file manager. isDir selects folder vs file reveal.
// Tests replace this hook.
var OpenPath = openPathOS

type RevealInput struct {
	ClientID  string
	Target    RevealTarget
	BenesHome string
	CodexHome string
	Env       map[string]string
}

// ResolveRevealPath returns the absolute allowlisted path for clientID+target.
func ResolveRevealPath(in RevealInput) (string, error) {
	id := strings.TrimSpace(in.ClientID)
	if !Known(id) {
		return "", ErrUnknownClient
	}
	detect, logPath := resolvePaths(id, in.CodexHome, in.Env)
	var raw string
	switch in.Target {
	case RevealLog:
		raw = logPath
	case RevealConfig:
		raw = configPathFor(id, detect, in.CodexHome, in.Env)
	default:
		return "", ErrUnknownTarget
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrPathUnavailable
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", ErrPathUnavailable
	}
	info, err := osStat(abs)
	if err != nil {
		return "", ErrPathUnavailable
	}
	_ = info
	return abs, nil
}

// Reveal opens the allowlisted path for clientID+target in the OS file manager.
func Reveal(in RevealInput) (string, error) {
	path, err := ResolveRevealPath(in)
	if err != nil {
		return "", err
	}
	info, err := osStat(path)
	if err != nil {
		return "", ErrPathUnavailable
	}
	if err := OpenPath(path, info.IsDir()); err != nil {
		return path, fmt.Errorf("%w: %v", ErrRevealUnsupported, err)
	}
	return path, nil
}

func configPathFor(id, detectPath, codexHome string, env map[string]string) string {
	home, _ := UserHome()
	if fileClient(id) {
		_, config := fileDetect(id, env)
		return firstExisting(config)
	}
	switch id {
	case "claude-desktop":
		return firstExisting(claudedesktop.ConfigCandidatesForReveal(home, env)...)
	case "claude":
		return firstExisting(filepath.Join(home, ".claude.json"))
	case "codex":
		return firstExisting(
			filepath.Join(codexHome, "config.toml"),
			filepath.Join(home, ".codex", "config.toml"),
		)
	case "grok":
		local := getenv(env, "LOCALAPPDATA")
		if local == "" && home != "" {
			local = filepath.Join(home, "AppData", "Local")
		}
		return firstExisting(
			filepath.Join(local, "Grok", "config.json"),
			filepath.Join(home, ".grok", "config.json"),
		)
	default:
		if detectPath == "" {
			return ""
		}
		dir := detectPath
		if info, err := osStat(detectPath); err == nil && !info.IsDir() {
			dir = filepath.Dir(detectPath)
		}
		return firstExisting(
			filepath.Join(dir, "config.json"),
			filepath.Join(dir, "config.toml"),
			filepath.Join(dir, "config.yaml"),
		)
	}
}
