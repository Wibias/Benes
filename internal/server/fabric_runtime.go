package server

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/Wibias/Benes/internal/resourcebudget"
	"github.com/Wibias/Benes/internal/sessions"
	"github.com/Wibias/Benes/internal/sidecar/fabric"
)

const (
	fabricMaxConcurrentExecutes = 8
	fabricShutdownWait          = 15 * time.Second
)

// Lock order for fabric execution authority (never invert):
//   rt.mu -> live.authorityMu -> live.intentMu / live.claimMu -> fabric.Repo.mu
// Cancel and durable handoff CAS + live claim publish share live.authorityMu so
// a cancel cannot observe a stale parent/child claim across a committed lease move.

var (
	fabricTestBeforeAuthorityHandoff           func()
	fabricTestAfterDurableHandoffBeforePublish func()
	fabricTestBeforeAuthorityReturn            func()
	fabricTestAfterDurableReturnBeforePublish  func()
	fabricTestAfterCancelSelected              func()
	fabricTestBeforeTerminalPersist            func()
)

// SetFabricAuthorityHandoffTestHooks installs race hooks around the parent->child
// authority boundary. Pass nil to clear.
func SetFabricAuthorityHandoffTestHooks(beforeAuthority, afterDurableBeforePublish func()) {
	fabricTestBeforeAuthorityHandoff = beforeAuthority
	fabricTestAfterDurableHandoffBeforePublish = afterDurableBeforePublish
}

// SetFabricAuthorityReturnTestHooks installs race hooks around the child->parent
// return authority boundary. Pass nil to clear.
func SetFabricAuthorityReturnTestHooks(beforeAuthority, afterDurableBeforePublish func()) {
	fabricTestBeforeAuthorityReturn = beforeAuthority
	fabricTestAfterDurableReturnBeforePublish = afterDurableBeforePublish
}

// SetFabricAfterCancelSelectedTestHook runs after cancel wins selectTerminal under
// the authority boundary (before waiting on the worker done channel).
func SetFabricAfterCancelSelectedTestHook(fn func()) {
	fabricTestAfterCancelSelected = fn
}

// SetFabricBeforeTerminalPersistTestHook runs inside finishPrimaryDrive immediately
// before the terminal state is persisted, so a test can assert what is still held at
// that boundary. Pass nil to clear.
func SetFabricBeforeTerminalPersistTestHook(fn func()) {
	fabricTestBeforeTerminalPersist = fn
}

// Linearizable terminal intents for one live run. First successful select wins;
// later competing intents cannot overwrite the fenced choice.
type fabricTerminalIntent int

const (
	fabricIntentNone fabricTerminalIntent = iota
	fabricIntentComplete
	fabricIntentCancel
	fabricIntentShutdown
	fabricIntentFail
)

// fabricRuntime owns live execute workers under a server-owned root context.
// Request contexts are used only for parse/admission before HTTP 202.
type fabricRuntime struct {
	h *handler

	rootCtx    context.Context
	rootCancel context.CancelFunc

	mu           sync.Mutex
	active       map[string]*fabricLiveRun // taskID -> run
	inflight     int
	shuttingDown bool
	closed       bool
	recoverOnce  sync.Once
	results      *fabricResultStore
}

// fabricCancelOutcome is the live-cancel result for HTTP mapping.
// Miss falls through to durable CancelTask; PersistFailed must not report success
// and must not be mapped to fencing 409.
type fabricCancelOutcome int

const (
	fabricCancelMiss fabricCancelOutcome = iota
	fabricCancelOK
	fabricCancelPersistFailed
	fabricCancelFencing
)

type fabricLiveRun struct {
	requestID string // canonical req_* minted once at admission; distinct from RunID
	cancel    context.CancelFunc
	done      chan struct{}
	turn      *resourcebudget.Turn

	// authorityMu linearizes durable handoff CAS + live claim publish against cancel.
	authorityMu sync.Mutex

	claimMu sync.Mutex
	id      fabric.RunIdentity

	intentMu sync.Mutex
	intent   fabricTerminalIntent

	// durableConverged is set only when a durable terminal (or interrupt reconcile)
	// actually persisted. Live cancelCurrent/cancelExpected report HTTP success
	// only when this is true after worker exit — dual storage failure must not.
	durableMu        sync.Mutex
	durableConverged bool
}

func (live *fabricLiveRun) claimSnapshot() fabric.RunIdentity {
	if live == nil {
		return fabric.RunIdentity{}
	}
	live.claimMu.Lock()
	defer live.claimMu.Unlock()
	return live.id
}

func (live *fabricLiveRun) replaceClaim(id fabric.RunIdentity) {
	if live == nil {
		return
	}
	live.claimMu.Lock()
	defer live.claimMu.Unlock()
	live.id = id
}

func (live *fabricLiveRun) setClaimOwnerFence(owner string, fence int) {
	if live == nil {
		return
	}
	live.claimMu.Lock()
	defer live.claimMu.Unlock()
	live.id.Owner = owner
	live.id.Fence = fence
}

func (live *fabricLiveRun) markDurableConverged() {
	if live == nil {
		return
	}
	live.durableMu.Lock()
	live.durableConverged = true
	live.durableMu.Unlock()
}

func (live *fabricLiveRun) durableTerminalConverged() bool {
	if live == nil {
		return false
	}
	live.durableMu.Lock()
	defer live.durableMu.Unlock()
	return live.durableConverged
}

func newFabricRuntime(h *handler) *fabricRuntime {
	ctx, cancel := context.WithCancel(context.Background())
	return &fabricRuntime{
		h:          h,
		rootCtx:    ctx,
		rootCancel: cancel,
		active:     map[string]*fabricLiveRun{},
		results:    newFabricResultStore(),
	}
}

func (rt *fabricRuntime) ensureRecovered(repo *fabric.Repo) {
	if rt == nil || repo == nil {
		return
	}
	rt.recoverOnce.Do(func() {
		_, _ = repo.RecoverOrphans()
	})
}

// selectTerminal fences a single terminal intent. Returns true only when this
// caller wins (or re-asserts the already-selected matching intent).
func (live *fabricLiveRun) selectTerminal(want fabricTerminalIntent) bool {
	if live == nil {
		return false
	}
	live.intentMu.Lock()
	defer live.intentMu.Unlock()
	if live.intent == fabricIntentNone {
		live.intent = want
		return true
	}
	return live.intent == want
}

func (live *fabricLiveRun) selectedTerminal() fabricTerminalIntent {
	if live == nil {
		return fabricIntentNone
	}
	live.intentMu.Lock()
	defer live.intentMu.Unlock()
	return live.intent
}

// execute admits capacity, begins the fenced run, then starts a worker under
// the server-owned root context. The HTTP request context must NOT be passed.
func (rt *fabricRuntime) execute(repo *fabric.Repo, taskID string, owner, model, input, delegationModel string) (fabric.ExecuteResult, string, error) {
	if rt == nil || rt.h == nil {
		return fabric.ExecuteResult{}, "", fabric.InvalidTransition("fabric runtime unavailable")
	}
	rt.mu.Lock()
	if rt.shuttingDown || rt.closed {
		rt.mu.Unlock()
		return fabric.ExecuteResult{}, "", fabric.InvalidTransition("fabric is shutting down")
	}
	if rt.inflight >= fabricMaxConcurrentExecutes {
		rt.mu.Unlock()
		return fabric.ExecuteResult{}, "", fabric.CapacityExceeded("fabric execute capacity exceeded")
	}
	if _, exists := rt.active[taskID]; exists {
		rt.mu.Unlock()
		return fabric.ExecuteResult{}, "", fabric.InvalidTransition("task already has an active primary run")
	}
	// Reserve execution slot BEFORE BeginExecute / 202.
	rt.inflight++
	rt.mu.Unlock()

	releaseSlot := func() {
		rt.mu.Lock()
		rt.inflight--
		rt.mu.Unlock()
	}

	// Acquire resource turn BEFORE BeginExecute so capacity fail never emits RunStarted.
	// Use non-blocking try-acquire: waiting would hold the HTTP goroutine and still risk 202-then-fail.
	var turn *resourcebudget.Turn
	if rt.h.resourceBudget != nil {
		var ok bool
		turn, ok = rt.h.resourceBudget.TryAcquireTurn("")
		if !ok {
			releaseSlot()
			return fabric.ExecuteResult{}, "", fabric.CapacityExceeded("request resource budget is exhausted")
		}
		if len(input) > 0 {
			if _, err := turn.Reserve(resourcebudget.ClassRequestBody, int64(len(input))); err != nil {
				_ = turn.Close()
				releaseSlot()
				return fabric.ExecuteResult{}, "", fabric.CapacityExceeded("request resource budget is exhausted")
			}
		}
		turn.SetPhase(resourcebudget.PhaseAdmission)
	}

	// Mint canonical requestId once at admission (distinct from Fabric runId).
	requestID := sessions.NewRequestID()

	id, err := repo.BeginExecute(taskID, fabric.ExecuteInput{Owner: owner, Model: model})
	if err != nil {
		if turn != nil {
			_ = turn.Close()
		}
		releaseSlot()
		return fabric.ExecuteResult{}, "", err
	}

	runCtx, cancel := context.WithCancel(rt.rootCtx)
	live := &fabricLiveRun{id: id, requestID: requestID, cancel: cancel, done: make(chan struct{}), turn: turn}
	rt.mu.Lock()
	if rt.shuttingDown || rt.closed {
		rt.mu.Unlock()
		cancel()
		if turn != nil {
			_ = turn.Close()
		}
		_ = repo.InterruptExecute(id, "shutdown")
		releaseSlot()
		return fabric.ExecuteResult{}, "", fabric.InvalidTransition("fabric is shutting down")
	}
	if _, exists := rt.active[taskID]; exists {
		rt.mu.Unlock()
		cancel()
		if turn != nil {
			_ = turn.Close()
		}
		_ = repo.FailExecute(id, "registration_conflict")
		releaseSlot()
		return fabric.ExecuteResult{}, "", fabric.InvalidTransition("task already has an active primary run")
	}
	rt.active[taskID] = live
	rt.mu.Unlock()

	go rt.driveWithOptionalHandoff(runCtx, repo, live, model, input, delegationModel)

	handle := "fr_" + id.RunID
	return fabric.ExecuteResult{
		RunID:  id.RunID,
		TaskID: id.TaskID,
		Owner:  id.Owner,
		Fence:  id.Fence,
		Status: "running",
		Model:  strings.TrimSpace(model),
	}, handle, nil
}

func (rt *fabricRuntime) drive(ctx context.Context, repo *fabric.Repo, live *fabricLiveRun, model, input string) {
	rt.driveWithOptionalHandoff(ctx, repo, live, model, input, "")
}

func (rt *fabricRuntime) shuttingDownLocked() bool {
	if rt == nil {
		return false
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.shuttingDown || rt.closed
}

func (rt *fabricRuntime) reconcileTerminalPersist(repo *fabric.Repo, live *fabricLiveRun, cause error) bool {
	reason := "terminal_persist_failed"
	if cause != nil {
		reason = clipFabricReason(cause.Error())
	}
	id := live.claimSnapshot()
	if err := repo.FailExecute(id, reason); err == nil {
		live.markDurableConverged()
		return true
	}
	if err := repo.InterruptExecute(id, reason); err == nil {
		live.markDurableConverged()
		return true
	}
	return false
}

func (rt *fabricRuntime) persistCancel(repo *fabric.Repo, live *fabricLiveRun, reason string) {
	if err := repo.CancelExecute(live.claimSnapshot(), reason); err != nil {
		_ = rt.reconcileTerminalPersist(repo, live, err)
		return
	}
	live.markDurableConverged()
}

func (rt *fabricRuntime) persistInterrupt(repo *fabric.Repo, live *fabricLiveRun, reason string) {
	if err := repo.InterruptExecute(live.claimSnapshot(), reason); err != nil {
		_ = rt.reconcileTerminalPersist(repo, live, err)
		return
	}
	live.markDurableConverged()
}

func (rt *fabricRuntime) persistFail(repo *fabric.Repo, live *fabricLiveRun, reason string) {
	id := live.claimSnapshot()
	if err := repo.FailExecute(id, reason); err != nil {
		if ierr := repo.InterruptExecute(id, firstNonEmptyReason(reason, "terminal_persist_failed")); ierr == nil {
			live.markDurableConverged()
		}
		return
	}
	live.markDurableConverged()
}

func firstNonEmptyReason(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return "cancelled"
}

// cancelExpected cancels only when the live claim exactly matches expected
// owner+fence+runID under authorityMu. Fencing mismatch returns fabricCancelFencing
// (caller maps to 409 while the run is still live). Explicit fencing is never weakened.
// HTTP success requires durable terminal convergence (not merely worker exit).
func (rt *fabricRuntime) cancelExpected(taskID string, expected fabric.RunIdentity) fabricCancelOutcome {
	return rt.cancelUnderAuthority(taskID, &expected)
}

// cancelCurrent cancels against the CURRENT live claim under authorityMu with no
// caller precondition. Used for unconditioned cancel so handoff races cannot
// produce a spurious 409 from a stale Repo.Get snapshot.
// HTTP success requires durable terminal convergence (not merely worker exit).
func (rt *fabricRuntime) cancelCurrent(taskID string) fabricCancelOutcome {
	return rt.cancelUnderAuthority(taskID, nil)
}

// cancel is preserved as cancelExpected for existing call sites/tests.
func (rt *fabricRuntime) cancel(taskID string, id fabric.RunIdentity) bool {
	return rt.cancelExpected(taskID, id) == fabricCancelOK
}

func (rt *fabricRuntime) cancelUnderAuthority(taskID string, expected *fabric.RunIdentity) fabricCancelOutcome {
	if rt == nil {
		return fabricCancelMiss
	}
	rt.mu.Lock()
	live, ok := rt.active[taskID]
	if !ok {
		rt.mu.Unlock()
		return fabricCancelMiss
	}
	// Same authority boundary as durable handoff CAS + claim publish.
	live.authorityMu.Lock()
	live.claimMu.Lock()
	claim := live.id
	live.claimMu.Unlock()
	if claim.RunID == "" {
		live.authorityMu.Unlock()
		rt.mu.Unlock()
		return fabricCancelMiss
	}
	if expected != nil {
		match := claim.RunID == expected.RunID && claim.Owner == expected.Owner && claim.Fence == expected.Fence
		if !match {
			live.authorityMu.Unlock()
			rt.mu.Unlock()
			return fabricCancelFencing
		}
	}
	won := live.selectTerminal(fabricIntentCancel)
	cancel := live.cancel
	done := live.done
	live.authorityMu.Unlock()
	rt.mu.Unlock()
	if won {
		if hook := fabricTestAfterCancelSelected; hook != nil {
			hook()
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(fabricShutdownWait):
	}
	if live.durableTerminalConverged() && live.selectedTerminal() == fabricIntentCancel {
		return fabricCancelOK
	}
	if live.selectedTerminal() == fabricIntentCancel {
		// Cancel intent won (or was already cancel) but durable terminal did not
		// persist — dual storage failure / half-commit without reconcile.
		return fabricCancelPersistFailed
	}
	return fabricCancelMiss
}

// reconcileHandoffStorageFailure converges recoverable post-CAS handoff storage
// failures in the same process via ReconcileExecuteInterrupted. Callers must invoke
// this even when selectTerminal(Fail) lost to another terminal intent (shutdown):
// lease movement already happened and must not depend on Fail winning. Returns true
// when the run was interrupted (or already inactive). On failure to persist, returns
// false without claiming success.
func (rt *fabricRuntime) reconcileHandoffStorageFailure(repo *fabric.Repo, live *fabricLiveRun, runID, reason string) bool {
	if rt == nil || repo == nil || live == nil {
		return false
	}
	claim := live.claimSnapshot()
	taskID := claim.TaskID
	if taskID == "" {
		return false
	}
	if runID == "" {
		runID = claim.RunID
	}
	reason = firstNonEmptyReason(reason, "handoff_storage_failed")
	if err := repo.ReconcileExecuteInterrupted(taskID, runID, reason); err != nil {
		return false
	}
	live.markDurableConverged()
	return true
}

func (rt *fabricRuntime) shutdown(repo *fabric.Repo) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	if rt.closed {
		rt.mu.Unlock()
		return
	}
	rt.shuttingDown = true
	rt.closed = true
	lives := make([]*fabricLiveRun, 0, len(rt.active))
	for _, live := range rt.active {
		_ = live.selectTerminal(fabricIntentShutdown)
		lives = append(lives, live)
	}
	rootCancel := rt.rootCancel
	rt.mu.Unlock()
	if rootCancel != nil {
		rootCancel()
	}
	for _, live := range lives {
		live.cancel()
	}
	deadline := time.Now().Add(fabricShutdownWait)
	for _, live := range lives {
		wait := time.Until(deadline)
		if wait < 0 {
			wait = 0
		}
		select {
		case <-live.done:
		case <-time.After(wait):
			if repo != nil && live.selectedTerminal() == fabricIntentShutdown {
				_ = repo.InterruptExecute(live.claimSnapshot(), "shutdown")
			}
		}
	}
	if rt.results != nil {
		rt.results.clear()
	}
}

// selectTerminalForTest lets tests inject a terminal intent without taking
// authorityMu (mirrors shutdown's select path racing a handoff half-commit).
func (rt *fabricRuntime) selectTerminalForTest(taskID string, want fabricTerminalIntent) bool {
	if rt == nil {
		return false
	}
	rt.mu.Lock()
	live := rt.active[taskID]
	rt.mu.Unlock()
	if live == nil {
		return false
	}
	return live.selectTerminal(want)
}

// inflightForTest returns the current execute inflight count for leak checks.
func (rt *fabricRuntime) inflightForTest() int {
	if rt == nil {
		return 0
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.inflight
}

// activeCountForTest returns how many live runs remain registered.
func (rt *fabricRuntime) activeCountForTest() int {
	if rt == nil {
		return 0
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.active)
}

func (rt *fabricRuntime) hasActive(taskID, runID string) bool {
	if rt == nil {
		return false
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	live, ok := rt.active[taskID]
	if !ok {
		return false
	}
	return live.claimSnapshot().RunID == runID
}

func (rt *fabricRuntime) resultByHandle(handle string) (*fabricStoredResult, bool) {
	if rt == nil || rt.results == nil {
		return nil, false
	}
	return rt.results.get(handle)
}

func (rt *fabricRuntime) resultByRun(taskID, runID string) (*fabricStoredResult, bool) {
	if rt == nil || rt.results == nil {
		return nil, false
	}
	return rt.results.getByRun(taskID, runID)
}
