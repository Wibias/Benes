package harnessboard

import (
	"os/exec"
	"path/filepath"
	"strings"
)

type Process struct {
	Name string
	Path string
}

var lookPathOS = exec.LookPath
var LookPath = lookPathOS
var ListProcesses = listProcessesOS

func normalizeProc(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimSuffix(name, ".exe")
	name = strings.TrimSuffix(name, ".app")
	return filepath.Base(name)
}

func nameMatches(id, procName string) bool {
	got := normalizeProc(procName)
	for _, want := range processNames(id) {
		if got == normalizeProc(want) {
			return true
		}
	}
	return false
}

func desktopPath(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	return strings.Contains(lower, "anthropicclaude") ||
		strings.Contains(lower, "claude.app") ||
		strings.Contains(lower, "/claude.app/") ||
		strings.Contains(lower, "programs/claude")
}

func runningFor(id, detectPath string, procs []Process) *bool {
	candidates := make([]Process, 0)
	for _, proc := range procs {
		if nameMatches(id, proc.Name) || nameMatches(id, filepath.Base(proc.Path)) {
			candidates = append(candidates, proc)
		}
	}
	if len(candidates) == 0 {
		v := false
		return &v
	}
	if id == "claude" || id == "claude-desktop" {
		return runningClaude(id, detectPath, candidates)
	}
	if detectPath != "" {
		if matched := runningAgainstDetect(detectPath, candidates); matched != nil {
			return matched
		}
	}
	v := true
	return &v
}

func runningClaude(id, detectPath string, candidates []Process) *bool {
	sawPath := false
	for _, proc := range candidates {
		if strings.TrimSpace(proc.Path) == "" {
			continue
		}
		sawPath = true
		isDesktop := desktopPath(proc.Path)
		if id == "claude-desktop" && isDesktop {
			v := true
			return &v
		}
		if id == "claude" && !isDesktop {
			v := true
			return &v
		}
	}
	if detectPath != "" {
		if matched := runningAgainstDetect(detectPath, candidates); matched != nil {
			return matched
		}
	}
	if !sawPath {
		// Same image name on Windows; without a path we cannot tell CLI from Desktop.
		return nil
	}
	v := false
	return &v
}

func runningAgainstDetect(detectPath string, candidates []Process) *bool {
	want := filepath.Clean(detectPath)
	info, err := osStat(want)
	isDir := err == nil && info.IsDir()
	isFile := err == nil && !info.IsDir()
	for _, proc := range candidates {
		path := strings.TrimSpace(proc.Path)
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		if isFile && strings.EqualFold(clean, want) {
			v := true
			return &v
		}
		if isDir && pathHasPrefix(clean, want) {
			v := true
			return &v
		}
		if !isFile && !isDir && strings.EqualFold(filepath.Base(clean), filepath.Base(want)) {
			v := true
			return &v
		}
	}
	return nil
}

func pathHasPrefix(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
