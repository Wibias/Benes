package integrations

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Wibias/Benes/internal/export"
)

type Paths struct {
	ConfigPath string
	DetectDir  string
}

func ResolvePaths(client, home string, env map[string]string) (Paths, error) {
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	getenv := func(key string) string {
		if env != nil {
			if v, ok := env[key]; ok {
				return strings.TrimSpace(v)
			}
		}
		return strings.TrimSpace(os.Getenv(key))
	}
	xdg := getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	switch client {
	case "opencode":
		return Paths{ConfigPath: filepath.Join(xdg, "opencode", "opencode.json"), DetectDir: filepath.Join(xdg, "opencode")}, nil
	case "pi":
		configPath, detectDir, err := export.PiPaths(home, getenv)
		if err != nil {
			return Paths{}, err
		}
		return Paths{ConfigPath: configPath, DetectDir: detectDir}, nil
	case "prime":
		configPath, detectDir, err := export.PrimePaths(home, getenv)
		if err != nil {
			return Paths{}, err
		}
		return Paths{ConfigPath: configPath, DetectDir: detectDir}, nil
	case "omp":
		return Paths{ConfigPath: filepath.Join(home, ".pi", "models.yml"), DetectDir: filepath.Join(home, ".pi")}, nil
	case "hermes":
		if runtime.GOOS == "windows" {
			local := getenv("LOCALAPPDATA")
			if local == "" {
				local = filepath.Join(home, "AppData", "Local")
			}
			return Paths{ConfigPath: filepath.Join(local, "hermes", "config.yaml"), DetectDir: filepath.Join(local, "hermes")}, nil
		}
		return Paths{ConfigPath: filepath.Join(home, ".hermes", "config.yaml"), DetectDir: filepath.Join(home, ".hermes")}, nil
	case "openclaw":
		explicit := getenv("OPENCLAW_CONFIG_PATH")
		if explicit != "" {
			if !filepath.IsAbs(explicit) {
				return Paths{}, fmt.Errorf("OPENCLAW_CONFIG_PATH must be absolute")
			}
			return Paths{ConfigPath: explicit, DetectDir: filepath.Dir(explicit)}, nil
		}
		return Paths{ConfigPath: filepath.Join(home, ".openclaw", "openclaw.json"), DetectDir: filepath.Join(home, ".openclaw")}, nil
	case "kimi":
		dir := getenv("KIMI_CODE_HOME")
		if dir == "" {
			dir = filepath.Join(home, ".kimi-code")
		}
		return Paths{ConfigPath: filepath.Join(dir, "config.toml"), DetectDir: dir}, nil
	case "gajae":
		return Paths{ConfigPath: filepath.Join(home, ".gjc", "agent", "models.yml"), DetectDir: filepath.Join(home, ".gjc")}, nil
	case "dsh":
		return Paths{ConfigPath: filepath.Join(home, ".dsh", "settings.yaml"), DetectDir: filepath.Join(home, ".dsh")}, nil
	case "mcode":
		dir := getenv("MINIMAX_DATA_DIR")
		if dir == "" {
			dir = filepath.Join(home, ".minimax")
		}
		return Paths{ConfigPath: filepath.Join(dir, "config.yaml"), DetectDir: dir}, nil
	default:
		return Paths{}, fmt.Errorf("unknown client %q", client)
	}
}

func clientFormat(client string) (export.Format, error) {
	for _, item := range export.Clients() {
		if item.ID == client {
			return item.Format, nil
		}
	}
	return "", fmt.Errorf("unknown client %q", client)
}
