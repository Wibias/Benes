//go:build linux

package harnessboard

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func listProcessesOS() ([]Process, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	out := make([]Process, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		base := filepath.Join("/proc", entry.Name())
		name := ""
		if raw, err := os.ReadFile(filepath.Join(base, "comm")); err == nil {
			name = strings.TrimSpace(string(raw))
		}
		path, err := os.Readlink(filepath.Join(base, "exe"))
		if err != nil {
			path = ""
		}
		if name == "" && path == "" {
			continue
		}
		out = append(out, Process{Name: name, Path: path})
	}
	return out, nil
}
