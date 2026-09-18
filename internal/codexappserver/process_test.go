package codexappserver

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

const appServerCmd = "/usr/local/bin/codex app-server"

func TestListFiltersInjectedSnapshots(t *testing.T) {
	matched := List(IO{ListSnapshots: func() []Snapshot {
		return []Snapshot{
			{PID: 11, CommandLine: "hermes-codex-bridge-mcp"},
			{PID: 22, CommandLine: "codex app-server --listen unix://x"},
			{PID: 22, CommandLine: "codex app-server --listen unix://x"},
			{PID: 33, CommandLine: "codex-code-mode-host"},
			{PID: 44, CommandLine: "codex exec hi"},
			{PID: 55, CommandLine: `codex exec "debug app-server behavior"`},
			{PID: 66, CommandLine: "node worker.js codex-code-mode-host"},
		}
	}})
	if len(matched) != 2 || matched[0].PID != 22 || matched[1].PID != 33 {
		t.Fatalf("matched=%v", matched)
	}
}

func TestCollectCatalogState(t *testing.T) {
	t.Run("not_running", func(t *testing.T) {
		status := Collect(IO{
			ListSnapshots:  func() []Snapshot { return nil },
			CatalogMtimeMs: func() *int64 { return ptrInt64(1000) },
		})
		if status.State != StateNotRunning || len(status.Processes) != 0 {
			t.Fatalf("%+v", status)
		}
	})
	t.Run("fresh", func(t *testing.T) {
		status := Collect(IO{
			ListSnapshots:  func() []Snapshot { return []Snapshot{{PID: 42, CommandLine: appServerCmd}} },
			ReadStartMs:    func(int) *int64 { return ptrInt64(2000) },
			CatalogMtimeMs: func() *int64 { return ptrInt64(1000) },
		})
		if status.State != StateFresh {
			t.Fatalf("state=%s", status.State)
		}
	})
	t.Run("stale", func(t *testing.T) {
		status := Collect(IO{
			ListSnapshots: func() []Snapshot {
				return []Snapshot{
					{PID: 42, CommandLine: appServerCmd},
					{PID: 43, CommandLine: appServerCmd},
				}
			},
			ReadStartMs: func(pid int) *int64 {
				if pid == 42 {
					return ptrInt64(500)
				}
				return ptrInt64(3000)
			},
			CatalogMtimeMs: func() *int64 { return ptrInt64(1000) },
		})
		if status.State != StateStale || len(status.Processes) != 2 {
			t.Fatalf("%+v", status)
		}
	})
	t.Run("unknown start or catalog", func(t *testing.T) {
		noStart := Collect(IO{
			ListSnapshots:  func() []Snapshot { return []Snapshot{{PID: 42, CommandLine: appServerCmd}} },
			ReadStartMs:    func(int) *int64 { return nil },
			CatalogMtimeMs: func() *int64 { return ptrInt64(1000) },
		})
		if noStart.State != StateUnknown {
			t.Fatalf("noStart=%s", noStart.State)
		}
		noCatalog := Collect(IO{
			ListSnapshots:  func() []Snapshot { return []Snapshot{{PID: 42, CommandLine: appServerCmd}} },
			ReadStartMs:    func(int) *int64 { return ptrInt64(500) },
			CatalogMtimeMs: func() *int64 { return nil },
		})
		if noCatalog.State != StateUnknown {
			t.Fatalf("noCatalog=%s", noCatalog.State)
		}
	})
	t.Run("unrelated ignored", func(t *testing.T) {
		status := Collect(IO{
			ListSnapshots: func() []Snapshot {
				return []Snapshot{
					{PID: 7, CommandLine: "node worker.js codex app-server"},
					{PID: 8, CommandLine: "/usr/bin/hermes-codex-bridge-mcp"},
				}
			},
			CatalogMtimeMs: func() *int64 { return ptrInt64(1000) },
		})
		if status.State != StateNotRunning {
			t.Fatalf("state=%s", status.State)
		}
	})
	t.Run("equal times are stale", func(t *testing.T) {
		status := Collect(IO{
			ListSnapshots:  func() []Snapshot { return []Snapshot{{PID: 42, CommandLine: appServerCmd}} },
			ReadStartMs:    func(int) *int64 { return ptrInt64(1000) },
			CatalogMtimeMs: func() *int64 { return ptrInt64(1000) },
		})
		if status.State != StateStale {
			t.Fatalf("state=%s", status.State)
		}
	})
	t.Run("enumeration failure is unknown", func(t *testing.T) {
		platform := "win32"
		if runtime.GOOS == "windows" {
			platform = "linux"
		}
		status := Collect(IO{
			Platform:       platform,
			CatalogMtimeMs: func() *int64 { return ptrInt64(1000) },
		})
		if status.State != StateUnknown {
			t.Fatalf("state=%s", status.State)
		}
	})
}

func TestRestartSignalsAllFirstSharedDeadlineNoSIGKILL(t *testing.T) {
	var signals []int
	var waits []int
	alive := map[int]bool{100: true, 200: true}
	snapshots := []Snapshot{
		{PID: 100, CommandLine: "codex app-server"},
		{PID: 200, CommandLine: "codex-code-mode-host"},
	}
	now := int64(1000)
	result := Restart([]Process{
		{PID: 100, CommandLine: "codex app-server"},
		{PID: 200, CommandLine: "codex-code-mode-host"},
	}, IO{
		ListSnapshots: func() []Snapshot { return snapshots },
		Kill: func(pid int) error {
			signals = append(signals, pid)
			if pid == 100 {
				alive[100] = false
			}
			return nil
		},
		IsAlive: func(pid int) bool { return alive[pid] },
		WaitExit: func(pid int, timeoutMs int) bool {
			waits = append(waits, timeoutMs)
			now += 500
			return !alive[pid]
		},
		Now: func() int64 { return now },
	})
	if len(signals) != 2 || signals[0] != 100 || signals[1] != 200 {
		t.Fatalf("signals=%v", signals)
	}
	if len(waits) != 2 || waits[0] != 2000 || waits[1] != 1500 {
		t.Fatalf("waits=%v", waits)
	}
	if len(result.Stopped) != 1 || result.Stopped[0] != 100 || len(result.Surviving) != 1 || result.Surviving[0] != 200 {
		t.Fatalf("%+v", result)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("failed=%v", result.Failed)
	}
}

func TestRestartTreatsKillThrowOnDeadPidAsStopped(t *testing.T) {
	result := Restart([]Process{{PID: 9, CommandLine: "codex app-server"}}, IO{
		ListSnapshots: func() []Snapshot { return []Snapshot{{PID: 9, CommandLine: "codex app-server"}} },
		Kill:          func(int) error { return errors.New("ESRCH") },
		IsAlive:       func(int) bool { return false },
		WaitExit:      func(int, int) bool { return true },
	})
	if len(result.Stopped) != 1 || result.Stopped[0] != 9 || len(result.Failed) != 0 || len(result.Surviving) != 0 {
		t.Fatalf("%+v", result)
	}
}

func TestRestartSkipsChangedAndRecycledIdentities(t *testing.T) {
	var signals []int
	changed := Restart([]Process{{PID: 42, CommandLine: "codex app-server"}}, IO{
		ListSnapshots: func() []Snapshot { return []Snapshot{{PID: 42, CommandLine: "vim README.md"}} },
		Kill: func(pid int) error {
			signals = append(signals, pid)
			return nil
		},
		IsAlive:  func(int) bool { return true },
		WaitExit: func(int, int) bool { return false },
	})
	if len(signals) != 0 || len(changed.Stopped) != 0 || len(changed.Surviving) != 0 {
		t.Fatalf("changed %+v signals=%v", changed, signals)
	}
	recycled := Restart([]Process{{PID: 42, CommandLine: "codex app-server --listen unix://old"}}, IO{
		ListSnapshots: func() []Snapshot {
			return []Snapshot{{PID: 42, CommandLine: "codex-code-mode-host --session 9"}}
		},
		Kill: func(pid int) error {
			signals = append(signals, pid)
			return nil
		},
		IsAlive:  func(int) bool { return true },
		WaitExit: func(int, int) bool { return false },
	})
	if len(signals) != 0 || len(recycled.Stopped) != 0 {
		t.Fatalf("recycled %+v signals=%v", recycled, signals)
	}
}

func TestAfterCatalogWriteWarnsOrRestarts(t *testing.T) {
	var errors []string
	var logs []string
	snapshots := []Snapshot{{PID: 7, CommandLine: "codex app-server --listen unix://x"}}
	io := IO{
		ListSnapshots: func() []Snapshot { return snapshots },
		Kill:          func(int) error { return nil },
		IsAlive:       func(int) bool { return false },
		WaitExit:      func(int, int) bool { return true },
	}
	log := testLog{log: &logs, err: &errors}
	warned := AfterCatalogWrite(AfterWriteOptions{Restart: false, Log: log, IO: io})
	if !warned.Warned || !strings.Contains(errors[0], "benes sync --restart-codex") {
		t.Fatalf("warned=%+v errors=%v", warned, errors)
	}
	restarted := AfterCatalogWrite(AfterWriteOptions{Restart: true, Log: log, IO: io})
	if restarted.Warned || restarted.Restart == nil || len(restarted.Restart.Stopped) != 1 {
		t.Fatalf("%+v", restarted)
	}
	found := false
	for _, line := range logs {
		if strings.Contains(line, "Stopping Codex app-server") {
			found = true
		}
	}
	if !found {
		t.Fatalf("logs=%v", logs)
	}
}

func TestPlatformTerminationLadder(t *testing.T) {
	target := Process{PID: 4242, CommandLine: "/opt/codex app-server"}
	snapshots := func() []Snapshot {
		return []Snapshot{{PID: 4242, CommandLine: "/opt/codex app-server"}}
	}

	t.Run("windows taskkill", func(t *testing.T) {
		var execCalls [][]string
		var signals []int
		Restart([]Process{target}, IO{
			Platform:      "win32",
			ListSnapshots: snapshots,
			ExecFile: func(file string, args []string) error {
				if !strings.Contains(strings.ToLower(file), "taskkill") {
					t.Fatalf("file=%s", file)
				}
				execCalls = append(execCalls, append([]string{file}, args...))
				return nil
			},
			ProcessKill: func(pid int) error {
				signals = append(signals, pid)
				return nil
			},
			IsAlive:  func(int) bool { return false },
			WaitExit: func(int, int) bool { return true },
		})
		if len(execCalls) != 1 || strings.Join(execCalls[0][1:], " ") != "/PID 4242 /T /F" {
			t.Fatalf("exec=%v", execCalls)
		}
		if len(signals) != 0 {
			t.Fatalf("signals=%v", signals)
		}
	})
	t.Run("taskkill fallback", func(t *testing.T) {
		var signals []int
		Restart([]Process{target}, IO{
			Platform:      "win32",
			ListSnapshots: snapshots,
			ExecFile:      func(string, []string) error { return errors.New("taskkill unavailable") },
			ProcessKill: func(pid int) error {
				signals = append(signals, pid)
				return nil
			},
			IsAlive:  func(int) bool { return false },
			WaitExit: func(int, int) bool { return true },
		})
		if len(signals) != 1 || signals[0] != 4242 {
			t.Fatalf("signals=%v", signals)
		}
	})
	t.Run("linux sigterm only", func(t *testing.T) {
		var execCalls int
		var signals []int
		Restart([]Process{target}, IO{
			Platform:      "linux",
			ListSnapshots: snapshots,
			ExecFile: func(string, []string) error {
				execCalls++
				return nil
			},
			ProcessKill: func(pid int) error {
				signals = append(signals, pid)
				return nil
			},
			IsAlive:  func(int) bool { return false },
			WaitExit: func(int, int) bool { return true },
		})
		if execCalls != 0 || len(signals) != 1 || signals[0] != 4242 {
			t.Fatalf("exec=%d signals=%v", execCalls, signals)
		}
	})
	t.Run("darwin like linux", func(t *testing.T) {
		var execCalls int
		var signals []int
		Restart([]Process{target}, IO{
			Platform:      "darwin",
			ListSnapshots: snapshots,
			ExecFile: func(string, []string) error {
				execCalls++
				return nil
			},
			ProcessKill: func(pid int) error {
				signals = append(signals, pid)
				return nil
			},
			IsAlive:  func(int) bool { return false },
			WaitExit: func(int, int) bool { return true },
		})
		if execCalls != 0 || len(signals) != 1 {
			t.Fatalf("exec=%d signals=%v", execCalls, signals)
		}
	})
}

func TestWindowsCIMScriptPinsOwnerFailClosed(t *testing.T) {
	script := WindowsCIMScript()
	for _, needle := range []string{
		"Invoke-CimMethod",
		"GetOwner",
		"__BENES_ENUM_INCOMPLETE__",
		"ReturnValue",
		"codex-code-mode-host",
		"(?i)",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("script missing %q", needle)
		}
	}
}

func TestWarnIfStaleAfterStartupWriteNeverSignals(t *testing.T) {
	signalled := false
	warned := WarnIfStaleAfterStartupWrite(nil, IO{
		ListSnapshots: func() []Snapshot { return []Snapshot{{PID: 9, CommandLine: appServerCmd}} },
		ReadStartMs:   func(int) *int64 { return ptrInt64(1) },
		CatalogMtimeMs: func() *int64 {
			return ptrInt64(10)
		},
		Kill: func(int) error {
			signalled = true
			return nil
		},
	})
	if !warned || signalled {
		t.Fatalf("warned=%v signalled=%v", warned, signalled)
	}
	quiet := WarnIfStaleAfterStartupWrite(nil, IO{
		ListSnapshots:  func() []Snapshot { return nil },
		CatalogMtimeMs: func() *int64 { return ptrInt64(10) },
		Kill: func(int) error {
			signalled = true
			return nil
		},
	})
	if quiet {
		t.Fatal("not_running should stay quiet")
	}
}

type testLog struct {
	log *[]string
	err *[]string
}

func (l testLog) Log(msg string)   { *l.log = append(*l.log, msg) }
func (l testLog) Error(msg string) { *l.err = append(*l.err, msg) }

func ptrInt64(v int64) *int64 { return &v }
