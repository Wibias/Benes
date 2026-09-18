package codexappserver

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func proc(pid int, commandLine ...string) Process {
	line := "codex app-server"
	if len(commandLine) > 0 {
		line = commandLine[0]
	}
	return Process{PID: pid, CommandLine: line}
}

func baseService(overrides ServiceIO) ServiceIO {
	io := ServiceIO{
		SyncCatalog: func(int) (bool, error) { return true, nil },
		ListenPort:  func() int { return 41999 },
		ResetCache:  func() {},
		CollectState: func(IO) CatalogStatus {
			return CatalogStatus{State: StateNotRunning, Processes: []CatalogProcess{}}
		},
		List: func(IO) []Process { return nil },
		ReadStartMs: func(pids []int) map[int]*int64 {
			out := map[int]*int64{}
			for _, pid := range pids {
				out[pid] = ptrInt64(1)
			}
			return out
		},
		Restart: func([]Process, IO) RestartResult {
			return RestartResult{Requested: emptyInts(), Stopped: emptyInts(), Surviving: emptyInts()}
		},
	}
	if overrides.SyncCatalog != nil {
		io.SyncCatalog = overrides.SyncCatalog
	}
	if overrides.ListenPort != nil {
		io.ListenPort = overrides.ListenPort
	}
	if overrides.CollectState != nil {
		io.CollectState = overrides.CollectState
	}
	if overrides.List != nil {
		io.List = overrides.List
	}
	if overrides.Restart != nil {
		io.Restart = overrides.Restart
	}
	if overrides.ReadStartMs != nil {
		io.ReadStartMs = overrides.ReadStartMs
	}
	if overrides.ResetCache != nil {
		io.ResetCache = overrides.ResetCache
	}
	return io
}

func TestPerformRestartStopsStaleServers(t *testing.T) {
	t.Cleanup(ResetRestartInFlightForTests)
	var signalled []int
	result := PerformRestart(baseService(ServiceIO{
		CollectState: func(IO) CatalogStatus {
			return CatalogStatus{
				State: StateStale,
				Processes: []CatalogProcess{
					{PID: 100, StartedAtMs: ptrInt64(1)},
					{PID: 200, StartedAtMs: ptrInt64(2)},
				},
				CatalogMtimeMs: ptrInt64(10),
			}
		},
		List: func(IO) []Process { return []Process{proc(100), proc(200)} },
		ReadStartMs: func([]int) map[int]*int64 {
			return map[int]*int64{100: ptrInt64(1), 200: ptrInt64(2)}
		},
		Restart: func(targets []Process, _ IO) RestartResult {
			for _, target := range targets {
				signalled = append(signalled, target.PID)
			}
			return RestartResult{Requested: []int{100, 200}, Stopped: []int{100, 200}, Surviving: emptyInts()}
		},
	}))
	if !result.Success || result.Code != CodeStopped || len(signalled) != 2 {
		t.Fatalf("%+v signalled=%v", result, signalled)
	}
}

func TestPerformRestartUnknownSignalsNothing(t *testing.T) {
	t.Cleanup(ResetRestartInFlightForTests)
	restarted := false
	result := PerformRestart(baseService(ServiceIO{
		CollectState: func(IO) CatalogStatus {
			return CatalogStatus{State: StateUnknown, Processes: []CatalogProcess{}}
		},
		Restart: func([]Process, IO) RestartResult {
			restarted = true
			return RestartResult{}
		},
	}))
	if result.Code != CodeEnumerationUnavailable || result.StateBefore != StateUnknown || restarted {
		t.Fatalf("%+v restarted=%v", result, restarted)
	}
}

func TestPerformRestartNothingRunning(t *testing.T) {
	t.Cleanup(ResetRestartInFlightForTests)
	restarted := false
	result := PerformRestart(baseService(ServiceIO{
		Restart: func([]Process, IO) RestartResult {
			restarted = true
			return RestartResult{}
		},
	}))
	if result.Code != CodeNothingRunning || restarted || !result.Success {
		t.Fatalf("%+v restarted=%v", result, restarted)
	}
}

func TestPerformRestartPartialAndExitedBetweenClassifyAndSignal(t *testing.T) {
	t.Cleanup(ResetRestartInFlightForTests)
	partial := PerformRestart(baseService(ServiceIO{
		CollectState: func(IO) CatalogStatus {
			return CatalogStatus{
				State: StateStale,
				Processes: []CatalogProcess{
					{PID: 100, StartedAtMs: ptrInt64(1)},
					{PID: 200, StartedAtMs: ptrInt64(2)},
				},
			}
		},
		List: func(IO) []Process { return []Process{proc(100), proc(200)} },
		ReadStartMs: func([]int) map[int]*int64 {
			return map[int]*int64{100: ptrInt64(1), 200: ptrInt64(2)}
		},
		Restart: func([]Process, IO) RestartResult {
			return RestartResult{Requested: []int{100, 200}, Stopped: []int{100}, Surviving: []int{200}}
		},
	}))
	if partial.Success || partial.Code != CodePartiallyStopped || len(partial.Surviving) != 1 {
		t.Fatalf("%+v", partial)
	}

	ResetRestartInFlightForTests()
	restarted := false
	exited := PerformRestart(baseService(ServiceIO{
		CollectState: func(IO) CatalogStatus {
			return CatalogStatus{State: StateStale, Processes: []CatalogProcess{{PID: 100, StartedAtMs: ptrInt64(1)}}}
		},
		List: func(IO) []Process { return nil },
		Restart: func([]Process, IO) RestartResult {
			restarted = true
			return RestartResult{}
		},
	}))
	if exited.Code != CodeNothingRunning || restarted {
		t.Fatalf("%+v restarted=%v", exited, restarted)
	}
}

func TestPerformRestartOnlyClassifiedPidsAndLivePort(t *testing.T) {
	t.Cleanup(ResetRestartInFlightForTests)
	var received []Process
	PerformRestart(baseService(ServiceIO{
		CollectState: func(IO) CatalogStatus {
			return CatalogStatus{
				State: StateStale,
				Processes: []CatalogProcess{
					{PID: 100, StartedAtMs: ptrInt64(1)},
					{PID: 200, StartedAtMs: ptrInt64(2)},
				},
			}
		},
		List: func(IO) []Process { return []Process{proc(200), proc(900)} },
		ReadStartMs: func([]int) map[int]*int64 {
			return map[int]*int64{200: ptrInt64(2), 900: ptrInt64(7)}
		},
		Restart: func(targets []Process, _ IO) RestartResult {
			received = append([]Process{}, targets...)
			return RestartResult{Requested: []int{200}, Stopped: []int{200}, Surviving: emptyInts()}
		},
	}))
	if len(received) != 1 || received[0].PID != 200 || !strings.Contains(received[0].CommandLine, "app-server") {
		t.Fatalf("received=%v", received)
	}

	ResetRestartInFlightForTests()
	var syncedPort int
	PerformRestart(baseService(ServiceIO{
		ListenPort: func() int { return 45123 },
		SyncCatalog: func(port int) (bool, error) {
			syncedPort = port
			return true, nil
		},
	}))
	if syncedPort != 45123 {
		t.Fatalf("port=%d", syncedPort)
	}
}

func TestPerformRestartSyncFailureStillRestartsAndStripsSecrets(t *testing.T) {
	t.Cleanup(ResetRestartInFlightForTests)
	result := PerformRestart(baseService(ServiceIO{
		SyncCatalog: func(int) (bool, error) { return false, errors.New("catalog write failed") },
		CollectState: func(IO) CatalogStatus {
			return CatalogStatus{State: StateStale, Processes: []CatalogProcess{{PID: 100, StartedAtMs: ptrInt64(1)}}}
		},
		List: func(IO) []Process { return []Process{proc(100, "/opt/private-marker/codex app-server")} },
		ReadStartMs: func([]int) map[int]*int64 {
			return map[int]*int64{100: ptrInt64(1)}
		},
		Restart: func([]Process, IO) RestartResult {
			return RestartResult{
				Requested: []int{100},
				Stopped:   emptyInts(),
				Surviving: []int{100},
				Failed:    []RestartFailure{{PID: 100, Error: "EPERM: /opt/private-marker/Library/private"}},
			}
		},
	}))
	if result.Synced || result.Code != CodePartiallyStopped {
		t.Fatalf("%+v", result)
	}
	serialized, _ := json.Marshal(result)
	text := string(serialized)
	for _, needle := range []string{"private-marker", "EPERM", "app-server"} {
		if strings.Contains(text, needle) {
			t.Fatalf("leaked %q in %s", needle, text)
		}
	}
	if len(result.Failed) != 1 || result.Failed[0] != 100 {
		t.Fatalf("failed=%v", result.Failed)
	}
}

func TestPerformRestartRefusesRecycledPidAndUnreadableStart(t *testing.T) {
	t.Cleanup(ResetRestartInFlightForTests)
	recycled := PerformRestart(baseService(ServiceIO{
		CollectState: func(IO) CatalogStatus {
			return CatalogStatus{State: StateStale, Processes: []CatalogProcess{{PID: 100, StartedAtMs: ptrInt64(1)}}}
		},
		List: func(IO) []Process { return []Process{proc(100)} },
		ReadStartMs: func([]int) map[int]*int64 {
			return map[int]*int64{100: ptrInt64(99)}
		},
		Restart: func([]Process, IO) RestartResult {
			t.Fatal("restart must not run for recycled pid")
			return RestartResult{}
		},
	}))
	if recycled.Code != CodeNothingRunning {
		t.Fatalf("%+v", recycled)
	}

	ResetRestartInFlightForTests()
	unreadable := PerformRestart(baseService(ServiceIO{
		CollectState: func(IO) CatalogStatus {
			return CatalogStatus{State: StateStale, Processes: []CatalogProcess{{PID: 100, StartedAtMs: ptrInt64(1)}}}
		},
		List:        func(IO) []Process { return []Process{proc(100)} },
		ReadStartMs: func([]int) map[int]*int64 { return map[int]*int64{100: nil} },
		Restart: func([]Process, IO) RestartResult {
			t.Fatal("restart must not run for unreadable start")
			return RestartResult{}
		},
	}))
	if unreadable.Code != CodeNothingRunning {
		t.Fatalf("%+v", unreadable)
	}
}

func TestPerformRestartUnknownWithProcessesSignalsNothing(t *testing.T) {
	t.Cleanup(ResetRestartInFlightForTests)
	restarted := false
	result := PerformRestart(baseService(ServiceIO{
		CollectState: func(IO) CatalogStatus {
			return CatalogStatus{State: StateUnknown, Processes: []CatalogProcess{{PID: 100, StartedAtMs: ptrInt64(1)}}}
		},
		List: func(IO) []Process { return []Process{proc(100)} },
		Restart: func([]Process, IO) RestartResult {
			restarted = true
			return RestartResult{}
		},
	}))
	if result.Code != CodeEnumerationUnavailable || restarted {
		t.Fatalf("%+v restarted=%v", result, restarted)
	}
}

func TestPerformRestartSingleFlight(t *testing.T) {
	t.Cleanup(ResetRestartInFlightForTests)
	var started sync.WaitGroup
	started.Add(1)
	release := make(chan struct{})
	calls := 0
	io := baseService(ServiceIO{
		CollectState: func(IO) CatalogStatus {
			return CatalogStatus{State: StateStale, Processes: []CatalogProcess{{PID: 100, StartedAtMs: ptrInt64(1)}}}
		},
		List: func(IO) []Process { return []Process{proc(100)} },
		ReadStartMs: func([]int) map[int]*int64 {
			return map[int]*int64{100: ptrInt64(1)}
		},
		Restart: func([]Process, IO) RestartResult {
			calls++
			started.Done()
			<-release
			return RestartResult{Requested: []int{100}, Stopped: []int{100}, Surviving: emptyInts()}
		},
	})
	firstDone := make(chan RestartResponse, 1)
	go func() { firstDone <- PerformRestart(io) }()
	started.Wait()
	secondDone := make(chan RestartResponse, 1)
	go func() { secondDone <- PerformRestart(io) }()
	time.Sleep(20 * time.Millisecond)
	close(release)
	first := <-firstDone
	second := <-secondDone
	if calls != 1 || first.Code != CodeStopped || second.Code != CodeStopped {
		t.Fatalf("calls=%d first=%+v second=%+v", calls, first, second)
	}
}

func TestReadStateReportsClassifier(t *testing.T) {
	got := ReadState(ServiceIO{
		CollectState: func(IO) CatalogStatus {
			return CatalogStatus{State: StateStale, Processes: []CatalogProcess{{PID: 1}, {PID: 2}}}
		},
	})
	if got.State != StateStale || got.RunningCount != 2 {
		t.Fatalf("%+v", got)
	}
	unknown := ReadState(ServiceIO{
		CollectState: func(IO) CatalogStatus { return CatalogStatus{State: StateUnknown} },
	})
	if unknown.State != StateUnknown {
		t.Fatalf("%+v", unknown)
	}
}

func TestLastMomentIdentityGateRefusesRecycledPid(t *testing.T) {
	t.Cleanup(ResetRestartInFlightForTests)
	starts := []int64{10, 99}
	read := 0
	snapshots := []Snapshot{{PID: 42, CommandLine: "codex app-server"}}
	result := PerformRestart(ServiceIO{
		SyncCatalog: func(int) (bool, error) { return true, nil },
		ListenPort:  func() int { return 1 },
		ResetCache:  func() {},
		CollectState: func(IO) CatalogStatus {
			return CatalogStatus{State: StateStale, Processes: []CatalogProcess{{PID: 42, StartedAtMs: ptrInt64(10)}}}
		},
		List: func(IO) []Process { return []Process{{PID: 42, CommandLine: "codex app-server"}} },
		ReadStartMs: func(pids []int) map[int]*int64 {
			out := map[int]*int64{}
			for _, pid := range pids {
				idx := read
				if idx >= len(starts) {
					idx = len(starts) - 1
				}
				read++
				out[pid] = ptrInt64(starts[idx])
			}
			return out
		},
		Process: IO{
			ListSnapshots: func() []Snapshot { return snapshots },
			IsAlive:       func(int) bool { return true },
			WaitExit:      func(int, int) bool { return false },
		},
	})
	if result.Code != CodePartiallyStopped || len(result.Failed) != 1 || result.Failed[0] != 42 {
		t.Fatalf("%+v read=%d", result, read)
	}
	serialized, _ := json.Marshal(result)
	if strings.Contains(string(serialized), "identity changed") {
		t.Fatalf("identity error leaked: %s", serialized)
	}
}
