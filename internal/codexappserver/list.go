package codexappserver

import (
	"fmt"
	"strconv"
	"strings"
)

// List returns matched Codex app-server processes.
//
// Enumeration failure means no targets — never signal a process we could not
// verify. The staleness collector uses Collect, which treats the same failure
// as unknown rather than not_running.
func List(io IO) []Process {
	snapshots, _ := snapshotsFor(io)
	return matchSnapshots(snapshots)
}

func snapshotsFor(io IO) ([]Snapshot, bool) {
	if io.ListSnapshots != nil {
		return io.ListSnapshots(), false
	}
	snapshots, err := defaultListSnapshots(platformOf(io), uidOf(io))
	if err != nil {
		return nil, true
	}
	return snapshots, false
}

func uidOf(io IO) *int {
	if io.Getuid != nil {
		return io.Getuid()
	}
	return hostUID()
}

func matchSnapshots(snapshots []Snapshot) []Process {
	seen := map[int]struct{}{}
	matched := make([]Process, 0)
	for _, snapshot := range snapshots {
		if _, dup := seen[snapshot.PID]; dup {
			continue
		}
		if !IsCodexAppServerCommandLine(snapshot.CommandLine, snapshot.Executable) {
			continue
		}
		seen[snapshot.PID] = struct{}{}
		matched = append(matched, Process{PID: snapshot.PID, CommandLine: snapshot.CommandLine})
	}
	return matched
}

// FormatWarning is the CLI/startup stderr text for stale matching processes.
func FormatWarning(processes []Process) string {
	pids := make([]string, 0, len(processes))
	for _, proc := range processes {
		pids = append(pids, strconv.Itoa(proc.PID))
	}
	plural := ""
	pidLabel := "PID"
	if len(processes) != 1 {
		plural = "es"
		pidLabel = "PIDs"
	}
	return fmt.Sprintf(
		"WARNING: %d Codex app-server process%s still running (%s: %s). "+
			"Disk catalog/cache were updated, but Codex may keep showing the old model list until those processes restart. "+
			"Re-run with `benes sync --restart-codex` (or `benes sync-cache --restart-codex`) to send SIGTERM only to matching app-server processes. "+
			"Active turns may be interrupted.",
		len(processes), plural, pidLabel, strings.Join(pids, ", "),
	)
}

func formatCatalogWarning(processes []CatalogProcess) string {
	converted := make([]Process, 0, len(processes))
	for _, proc := range processes {
		converted = append(converted, Process{PID: proc.PID})
	}
	return FormatWarning(converted)
}
