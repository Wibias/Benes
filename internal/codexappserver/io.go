package codexappserver

// StaleHint is attached to dashboard sync results after a catalog or cache write.
const StaleHint = "If Codex still shows an older model list, restart its long-lived app-server process after sync (benes sync --restart-codex)."

const catalogStateTTLMs int64 = 5_000
const restartWaitMs int64 = 2_000

// Snapshot is one OS process row before Codex matching.
type Snapshot struct {
	PID         int
	CommandLine string
	Executable  string
	UID         *int
	Owner       string
}

// Process is a matched Codex app-server (or code-mode host).
type Process struct {
	PID         int
	CommandLine string
}

// IO injects process listing, signalling, and clocks so tests never terminate
// a developer's own Codex.
type IO struct {
	Platform       string
	Getuid         func() *int
	ListSnapshots  func() []Snapshot
	IsAlive        func(pid int) bool
	Kill           func(pid int) error
	ExecFile       func(file string, args []string) error
	ProcessKill    func(pid int) error
	WaitExit       func(pid int, timeoutMs int) bool
	Now            func() int64
	ReadStartMs    func(pid int) *int64
	CatalogMtimeMs func() *int64
	CatalogHome    string
}

// CatalogState is the classifier reading used by the dashboard banner and restart.
type CatalogState string

const (
	StateFresh      CatalogState = "fresh"
	StateStale      CatalogState = "stale"
	StateNotRunning CatalogState = "not_running"
	StateUnknown    CatalogState = "unknown"
)

// CatalogStatus is the classifier result, including start times used as identity.
type CatalogStatus struct {
	State          CatalogState
	Processes      []CatalogProcess
	CatalogMtimeMs *int64
}

// CatalogProcess is a matched server as the classifier reports it (no command line).
type CatalogProcess struct {
	PID         int
	StartedAtMs *int64
}

// RestartResult is the pid-only outcome of signalling matched app-servers.
type RestartResult struct {
	Requested []int
	Stopped   []int
	Surviving []int
	Failed    []RestartFailure
}

// RestartFailure keeps the OS error internally; API responses project it to a pid.
type RestartFailure struct {
	PID   int
	Error string
}

// Logger is the CLI/startup warning sink.
type Logger interface {
	Log(string)
	Error(string)
}

// AfterWriteOptions drive the CLI --restart-codex path.
type AfterWriteOptions struct {
	Restart bool
	Log     Logger
	IO      IO
}

// AfterWriteResult is what the CLI reports after a catalog/cache write.
type AfterWriteResult struct {
	Processes []Process
	Warned    bool
	Restart   *RestartResult
	Hint      string
}

// StateResponse is GET /api/system/codex-app-server.
type StateResponse struct {
	State        CatalogState `json:"state"`
	RunningCount int          `json:"runningCount"`
}

// RestartCode is the dashboard POST outcome.
type RestartCode string

const (
	CodeStopped                RestartCode = "stopped"
	CodeNothingRunning         RestartCode = "nothing_running"
	CodeEnumerationUnavailable RestartCode = "enumeration_unavailable"
	CodePartiallyStopped       RestartCode = "partially_stopped"
)

// RestartResponse is POST /api/system/codex-restart. Arrays are pid lists only.
type RestartResponse struct {
	Success     bool         `json:"success"`
	StateBefore CatalogState `json:"stateBefore"`
	Synced      bool         `json:"synced"`
	Requested   []int        `json:"requested"`
	Stopped     []int        `json:"stopped"`
	Surviving   []int        `json:"surviving"`
	Failed      []int        `json:"failed"`
	Code        RestartCode  `json:"code"`
}

// ServiceIO injects dashboard restart seams. Production leaves every field nil.
type ServiceIO struct {
	Process      IO
	SyncCatalog  func(port int) (bool, error)
	ListenPort   func() int
	CollectState func(IO) CatalogStatus
	List         func(IO) []Process
	Restart      func([]Process, IO) RestartResult
	ResetCache   func()
	ReadStartMs  func(pids []int) map[int]*int64
}

func emptyInts() []int { return []int{} }

func platformOf(io IO) string {
	if io.Platform != "" {
		return io.Platform
	}
	return hostGOOS()
}

func isWindows(platform string) bool {
	return platform == "windows" || platform == "win32"
}
