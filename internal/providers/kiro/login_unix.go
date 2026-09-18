//go:build !windows

package kiro

import "syscall"

func pidExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}
