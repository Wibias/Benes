//go:build windows

package harnessboard

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func listProcessesOS() ([]Process, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}
	out := make([]Process, 0, 128)
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		out = append(out, Process{Name: name, Path: imagePath(entry.ProcessID)})
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return out, nil
}

func imagePath(pid uint32) string {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)
	var buf [windows.MAX_PATH]uint16
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
