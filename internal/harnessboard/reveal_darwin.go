//go:build darwin

package harnessboard

import "os/exec"

func openPathOS(path string, isDir bool) error {
	if isDir {
		return exec.Command("open", path).Start()
	}
	return exec.Command("open", "-R", path).Start()
}
