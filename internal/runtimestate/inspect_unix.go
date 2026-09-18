//go:build !windows

package runtimestate

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func Inspect(pid int) ProcessInfo {
	if pid <= 0 {
		return ProcessInfo{}
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return ProcessInfo{}
	}
	info := ProcessInfo{Alive: true}
	if runtime.GOOS == "linux" {
		if target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
			info.Exe = target
		}
	}
	if runtime.GOOS == "darwin" && info.Exe == "" {
		out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
		if err == nil {
			info.Exe = strings.TrimSpace(string(out))
		}
	}
	return info
}
