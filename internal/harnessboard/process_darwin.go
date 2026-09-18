//go:build darwin

package harnessboard

import (
	"os/exec"
	"strings"
)

func listProcessesOS() ([]Process, error) {
	output, err := exec.Command("ps", "-A", "-o", "comm=").Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(output), "\n")
	out := make([]Process, 0, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		out = append(out, Process{Name: name})
	}
	return out, nil
}
