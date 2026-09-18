//go:build windows

package runtimestate

import (
	"golang.org/x/sys/windows"
)

func Inspect(pid int) ProcessInfo {
	if pid <= 0 {
		return ProcessInfo{}
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ProcessInfo{}
	}
	defer windows.CloseHandle(handle)
	var buf [windows.MAX_PATH]uint16
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &n); err != nil {
		return ProcessInfo{Alive: true}
	}
	return ProcessInfo{Alive: true, Exe: windows.UTF16ToString(buf[:n])}
}
