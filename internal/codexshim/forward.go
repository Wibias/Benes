package codexshim

import (
	"os"
	"os/exec"
	"strings"
)

var lookupExecutable = os.Executable

func ForwardIfShim(args []string) (bool, int) {
	exe, err := lookupExecutable()
	if err != nil || strings.TrimSpace(exe) == "" {
		return false, 0
	}
	spec, err := ReadSidecar(SidecarPath(exe))
	if err != nil || spec == nil {
		return false, 0
	}
	if spec.Benes != "" && !samePath(spec.Benes, exe) {
		cmd := exec.Command(spec.Benes, "ensure")
		cmd.Stdout = nil
		cmd.Stderr = nil
		_ = cmd.Run()
	}
	cmd := exec.Command(spec.Backup, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if code, ok := exitCode(err); ok {
			return true, code
		}
		return true, 1
	}
	return true, 0
}

func exitCode(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), true
	}
	return 0, false
}
