package codexappserver

import "sync"

var (
	restartMu sync.Mutex
	inFlight  *restartWaiter
)

type restartWaiter struct {
	done   chan struct{}
	result RestartResponse
}

// ResetRestartInFlightForTests drops the dashboard single-flight latch.
func ResetRestartInFlightForTests() {
	restartMu.Lock()
	inFlight = nil
	restartMu.Unlock()
}

// ReadState is GET /api/system/codex-app-server. It never signals.
func ReadState(io ServiceIO) StateResponse {
	collect := io.CollectState
	if collect == nil {
		collect = Collect
	}
	status := collect(io.Process)
	return StateResponse{State: status.State, RunningCount: len(status.Processes)}
}

// PerformRestart is the dashboard POST: sync catalog first, then signal only
// when the classifier is not unknown and start-time identity still matches.
func PerformRestart(io ServiceIO) RestartResponse {
	restartMu.Lock()
	if inFlight != nil {
		w := inFlight
		restartMu.Unlock()
		<-w.done
		return w.result
	}
	w := &restartWaiter{done: make(chan struct{})}
	inFlight = w
	restartMu.Unlock()

	result := runRestart(io)

	restartMu.Lock()
	w.result = result
	close(w.done)
	inFlight = nil
	restartMu.Unlock()
	return result
}

func runRestart(io ServiceIO) RestartResponse {
	synced := false
	if io.SyncCatalog != nil {
		port := 0
		if io.ListenPort != nil {
			port = io.ListenPort()
		}
		if wrote, err := io.SyncCatalog(port); err == nil {
			synced = wrote
		}
	}

	reset := io.ResetCache
	if reset == nil {
		reset = ResetCatalogStateCache
	}
	reset()

	collect := io.CollectState
	if collect == nil {
		collect = Collect
	}
	before := collect(io.Process)

	nothing := func(code RestartCode) RestartResponse {
		return RestartResponse{
			Success:     true,
			StateBefore: before.State,
			Synced:      synced,
			Requested:   emptyInts(),
			Stopped:     emptyInts(),
			Surviving:   emptyInts(),
			Failed:      emptyInts(),
			Code:        code,
		}
	}
	if before.State == StateUnknown {
		return nothing(CodeEnumerationUnavailable)
	}
	if len(before.Processes) == 0 {
		return nothing(CodeNothingRunning)
	}

	classifiedStarts := map[int]*int64{}
	for _, proc := range before.Processes {
		classifiedStarts[proc.PID] = proc.StartedAtMs
	}

	list := io.List
	if list == nil {
		list = List
	}
	live := list(io.Process)
	candidates := make([]Process, 0, len(live))
	for _, proc := range live {
		if _, ok := classifiedStarts[proc.PID]; ok {
			candidates = append(candidates, proc)
		}
	}

	readStarts := io.ReadStartMs
	if readStarts == nil {
		readStarts = func(pids []int) map[int]*int64 {
			if io.Process.ReadStartMs != nil {
				out := map[int]*int64{}
				for _, pid := range pids {
					out[pid] = io.Process.ReadStartMs(pid)
				}
				return out
			}
			return readProcessStartMsBatch(pids, platformOf(io.Process))
		}
	}

	startsNow := map[int]*int64{}
	if len(candidates) > 0 {
		pids := make([]int, 0, len(candidates))
		for _, proc := range candidates {
			pids = append(pids, proc.PID)
		}
		startsNow = readStarts(pids)
	}

	targets := make([]Process, 0, len(candidates))
	for _, proc := range candidates {
		classified := classifiedStarts[proc.PID]
		current := startsNow[proc.PID]
		if classified == nil || current == nil || *classified != *current {
			continue
		}
		targets = append(targets, proc)
	}
	if len(targets) == 0 {
		return nothing(CodeNothingRunning)
	}

	guarded := io.Process
	innerKill := killOf(io.Process)
	guarded.Kill = func(pid int) error {
		classified := classifiedStarts[pid]
		current := readStarts([]int{pid})[pid]
		if classified == nil || current == nil || *classified != *current {
			return identityChangedError{PID: pid}
		}
		return innerKill(pid)
	}

	restart := io.Restart
	if restart == nil {
		restart = Restart
	}
	result := restart(targets, guarded)
	failed := emptyInts()
	for _, entry := range result.Failed {
		failed = append(failed, entry.PID)
	}
	clean := len(result.Surviving) == 0 && len(failed) == 0
	code := CodeStopped
	if !clean {
		code = CodePartiallyStopped
	}
	requested := result.Requested
	if requested == nil {
		requested = emptyInts()
	}
	stopped := result.Stopped
	if stopped == nil {
		stopped = emptyInts()
	}
	surviving := result.Surviving
	if surviving == nil {
		surviving = emptyInts()
	}
	return RestartResponse{
		Success:     clean,
		StateBefore: before.State,
		Synced:      synced,
		Requested:   requested,
		Stopped:     stopped,
		Surviving:   surviving,
		Failed:      failed,
		Code:        code,
	}
}
