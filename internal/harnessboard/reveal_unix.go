//go:build !windows && !darwin

package harnessboard

import (
	"os/exec"
	"path/filepath"
)

func openPathOS(path string, isDir bool) error {
	if isDir {
		return exec.Command("xdg-open", path).Start()
	}
	return exec.Command("xdg-open", filepath.Dir(path)).Start()
}
