package zcode

import (
	"fmt"
	"os"
	"path/filepath"
)

func ConfigPath() (string, error) {
	if raw := os.Getenv("ZCODE_DATA_DIR"); raw != "" {
		if !filepath.IsAbs(raw) {
			return "", fmt.Errorf("ZCODE_DATA_DIR must be an absolute path")
		}
		return filepath.Join(filepath.Clean(raw), "v2", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zcode", "v2", "config.json"), nil
}
