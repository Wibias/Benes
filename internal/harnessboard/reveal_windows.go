//go:build windows

package harnessboard

import "os/exec"

func openPathOS(path string, isDir bool) error {
	if isDir {
		return exec.Command("explorer", path).Start()
	}
	// /select, must be one argument with no space after the comma.
	return exec.Command("explorer", "/select,"+path).Start()
}
