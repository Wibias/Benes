package codexappserver

import (
	"fmt"
	"time"
)

type identityChangedError struct {
	PID int
}

func (e identityChangedError) Error() string {
	return fmt.Sprintf("codex app-server identity changed before signal (pid %d)", e.PID)
}

func isAliveOf(io IO) func(int) bool {
	if io.IsAlive != nil {
		return io.IsAlive
	}
	return isAlive
}

func waitExitOf(io IO) func(int, int) bool {
	if io.WaitExit != nil {
		return io.WaitExit
	}
	alive := isAliveOf(io)
	return func(pid int, timeoutMs int) bool {
		deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
		for time.Now().Before(deadline) {
			if !alive(pid) {
				return true
			}
			time.Sleep(50 * time.Millisecond)
		}
		return !alive(pid)
	}
}

func defaultKill(pid int, io IO) error {
	if isWindows(platformOf(io)) {
		execFile := io.ExecFile
		if execFile == nil {
			execFile = execTrusted
		}
		if err := execFile(trustedTaskkillPath(), []string{"/PID", fmt.Sprintf("%d", pid), "/T", "/F"}); err != nil {
			return processKillOf(io)(pid)
		}
		return nil
	}
	return processKillOf(io)(pid)
}

func processKillOf(io IO) func(int) error {
	if io.ProcessKill != nil {
		return io.ProcessKill
	}
	return defaultProcessKill
}

func killOf(io IO) func(int) error {
	if io.Kill != nil {
		return io.Kill
	}
	return func(pid int) error {
		return defaultKill(pid, io)
	}
}

// Restart sends SIGTERM (or Windows taskkill /T /F) to matched processes and
// waits briefly. It never escalates to SIGKILL on Unix.
func Restart(processes []Process, io IO) RestartResult {
	alive := isAliveOf(io)
	kill := killOf(io)
	wait := waitExitOf(io)
	now := nowMs
	requested := make([]int, 0, len(processes))
	for _, proc := range processes {
		requested = append(requested, proc.PID)
	}
	stopped := emptyInts()
	surviving := emptyInts()
	failed := make([]RestartFailure, 0)

	liveByPID := map[int]Process{}
	for _, proc := range List(io) {
		liveByPID[proc.PID] = proc
	}
	signaled := make([]Process, 0, len(processes))
	for _, proc := range processes {
		live, ok := liveByPID[proc.PID]
		if !ok || ProcessIdentity(live.PID, live.CommandLine) != ProcessIdentity(proc.PID, proc.CommandLine) {
			if !alive(proc.PID) {
				stopped = append(stopped, proc.PID)
			}
			continue
		}
		if err := kill(proc.PID); err != nil {
			if alive(proc.PID) {
				failed = append(failed, RestartFailure{PID: proc.PID, Error: err.Error()})
				surviving = append(surviving, proc.PID)
			} else {
				stopped = append(stopped, proc.PID)
			}
			continue
		}
		signaled = append(signaled, proc)
	}

	deadline := now(io) + restartWaitMs
	for _, proc := range signaled {
		remaining := deadline - now(io)
		if remaining < 0 {
			remaining = 0
		}
		if wait(proc.PID, int(remaining)) || !alive(proc.PID) {
			stopped = append(stopped, proc.PID)
		} else {
			surviving = append(surviving, proc.PID)
		}
	}
	return RestartResult{
		Requested: requested,
		Stopped:   stopped,
		Surviving: surviving,
		Failed:    failed,
	}
}
