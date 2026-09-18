//go:build unix

package codexappserver

import (
	"os"
	"os/exec"
	"syscall"
)

func isAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func defaultProcessKill(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}

func hideWindow(cmd *exec.Cmd) {}

func windowsSystemDir() string { return fallbackSystemDir() }
