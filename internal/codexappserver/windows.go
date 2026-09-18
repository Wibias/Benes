//go:build windows

package codexappserver

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

const stillActive = 259

func isAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}

func defaultProcessKill(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

func windowsSystemDir() string {
	dir, err := windows.GetSystemDirectory()
	if err == nil && dir != "" {
		return dir
	}
	return fallbackSystemDir()
}
