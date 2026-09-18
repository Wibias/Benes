package codexappserver

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	catalogCacheMu sync.Mutex
	catalogCache   *catalogCacheEntry
)

type catalogCacheEntry struct {
	atMs   int64
	status CatalogStatus
}

func nowMs(io IO) int64 {
	if io.Now != nil {
		return io.Now()
	}
	return time.Now().UnixMilli()
}

func fullyDefault(io IO) bool {
	return io.ListSnapshots == nil && io.ReadStartMs == nil && io.CatalogMtimeMs == nil &&
		io.Platform == "" && io.Getuid == nil && io.Now == nil && io.CatalogHome == ""
}

// ResetCatalogStateCache drops the 5s memoized classifier reading.
func ResetCatalogStateCache() {
	catalogCacheMu.Lock()
	catalogCache = nil
	catalogCacheMu.Unlock()
}

// Collect compares the on-disk catalog mtime against running Codex app-servers.
//
//   - not_running: no app-server process
//   - unknown: enumeration failed, catalog unreadable, or any start time unreadable
//   - stale: at least one server started at or before the catalog mtime (`<=` is
//     deliberate: coarse clocks can report equal values)
func Collect(io IO) CatalogStatus {
	now := nowMs(io)
	if fullyDefault(io) {
		catalogCacheMu.Lock()
		if catalogCache != nil && now-catalogCache.atMs < catalogStateTTLMs {
			status := catalogCache.status
			catalogCacheMu.Unlock()
			return status
		}
		catalogCacheMu.Unlock()
	}
	status := computeCatalogState(io)
	if fullyDefault(io) {
		catalogCacheMu.Lock()
		catalogCache = &catalogCacheEntry{atMs: now, status: status}
		catalogCacheMu.Unlock()
	}
	return status
}

func computeCatalogState(io IO) CatalogStatus {
	snapshots, enumerationFailed := snapshotsFor(io)
	processes := matchSnapshots(snapshots)
	if len(processes) == 0 {
		if enumerationFailed {
			return CatalogStatus{State: StateUnknown, Processes: []CatalogProcess{}}
		}
		return CatalogStatus{State: StateNotRunning, Processes: []CatalogProcess{}}
	}
	catalogMtime := catalogMtimeMs(io)
	withStarts := attachStartTimes(io, processes)
	if catalogMtime == nil {
		return CatalogStatus{State: StateUnknown, Processes: withStarts, CatalogMtimeMs: nil}
	}
	for _, proc := range withStarts {
		if proc.StartedAtMs == nil {
			return CatalogStatus{State: StateUnknown, Processes: withStarts, CatalogMtimeMs: catalogMtime}
		}
	}
	stale := false
	for _, proc := range withStarts {
		if *proc.StartedAtMs <= *catalogMtime {
			stale = true
			break
		}
	}
	state := StateFresh
	if stale {
		state = StateStale
	}
	return CatalogStatus{State: state, Processes: withStarts, CatalogMtimeMs: catalogMtime}
}

func attachStartTimes(io IO, processes []Process) []CatalogProcess {
	out := make([]CatalogProcess, 0, len(processes))
	if io.ReadStartMs != nil {
		for _, proc := range processes {
			out = append(out, CatalogProcess{PID: proc.PID, StartedAtMs: io.ReadStartMs(proc.PID)})
		}
		return out
	}
	pids := make([]int, 0, len(processes))
	for _, proc := range processes {
		pids = append(pids, proc.PID)
	}
	batch := readProcessStartMsBatch(pids, platformOf(io))
	for _, proc := range processes {
		out = append(out, CatalogProcess{PID: proc.PID, StartedAtMs: batch[proc.PID]})
	}
	return out
}

func catalogMtimeMs(io IO) *int64 {
	if io.CatalogMtimeMs != nil {
		return io.CatalogMtimeMs()
	}
	home := io.CatalogHome
	if home == "" {
		home = os.Getenv("CODEX_HOME")
	}
	if home == "" {
		return nil
	}
	info, err := os.Stat(filepath.Join(home, "benes-catalog.json"))
	if err != nil {
		return nil
	}
	ms := info.ModTime().UnixMilli()
	return &ms
}

// AfterCatalogWrite warns about stale app-servers, or restarts them when requested.
func AfterCatalogWrite(options AfterWriteOptions) AfterWriteResult {
	processes := List(options.IO)
	result := AfterWriteResult{Processes: processes, Hint: StaleHint}
	if len(processes) == 0 {
		return result
	}
	if !options.Restart {
		if options.Log != nil {
			options.Log.Error(FormatWarning(processes))
		}
		result.Warned = true
		return result
	}
	if options.Log != nil {
		pids := make([]string, 0, len(processes))
		for _, proc := range processes {
			pids = append(pids, strconv.Itoa(proc.PID))
		}
		options.Log.Log("Stopping Codex app-server process(es): " + strings.Join(pids, ", ") + " (active turns may be interrupted).")
	}
	restart := Restart(processes, options.IO)
	result.Restart = &restart
	if options.Log != nil {
		if len(restart.Stopped) > 0 {
			options.Log.Log("Stopped Codex app-server PID(s): " + joinIntComma(restart.Stopped))
		}
		for _, failure := range restart.Failed {
			options.Log.Error("Failed to stop Codex app-server PID " + strconv.Itoa(failure.PID) + ": " + failure.Error)
		}
		if len(restart.Surviving) > 0 {
			options.Log.Error(
				"Codex app-server PID(s) still running after SIGTERM: " + joinIntComma(restart.Surviving) +
					". Stop them manually if the model list stays stale.",
			)
		}
	}
	return result
}

// WarnIfStaleAfterStartupWrite is the unattended-boot counterpart: it never signals.
func WarnIfStaleAfterStartupWrite(log Logger, io IO) (warned bool) {
	defer func() {
		if recover() != nil {
			warned = false
		}
	}()
	ResetCatalogStateCache()
	status := Collect(io)
	if status.State != StateStale {
		return false
	}
	if log != nil {
		log.Error(formatCatalogWarning(status.Processes))
	}
	return true
}

func joinIntComma(values []int) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.Itoa(value))
	}
	return strings.Join(parts, ", ")
}
