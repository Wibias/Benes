package codexauth

import (
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultQuotaCooldown         = time.Minute
	MaxQuotaCooldown             = 24 * time.Hour
	MaxResetDerivedQuotaCooldown = 15 * time.Minute
	TransientSoftAvoidInitial    = 30 * time.Second
)

var retryAfterSecondsPattern = regexp.MustCompile(`^\d+(?:\.\d+)?$`)

type OutcomeKind string

const (
	OutcomeHTTP           OutcomeKind = "http"
	OutcomeConnectError   OutcomeKind = "connect_error"
	OutcomeTimeout        OutcomeKind = "timeout"
	OutcomeConnectNeutral OutcomeKind = "connect_neutral"
)

type UpstreamOutcome struct {
	Kind       OutcomeKind
	StatusCode int
}

func HTTPOutcome(statusCode int) UpstreamOutcome {
	return UpstreamOutcome{Kind: OutcomeHTTP, StatusCode: statusCode}
}

type OutcomeDenial string

const (
	DenialWorkspace   OutcomeDenial = "workspace"
	DenialEntitlement OutcomeDenial = "entitlement"
)

type OutcomeClass string

const (
	OutcomeSuccess    OutcomeClass = "success"
	OutcomeCredential OutcomeClass = "credential"
	OutcomeWorkspace  OutcomeClass = "workspace"
	OutcomeQuota      OutcomeClass = "quota"
	OutcomeTransient  OutcomeClass = "transient"
	OutcomeCaller     OutcomeClass = "caller"
	OutcomeNeutral    OutcomeClass = "neutral"
	OutcomeUnknown    OutcomeClass = "unknown"
)

type OutcomeMeta struct {
	Now               time.Time
	Denial            OutcomeDenial
	RetryAfter        string
	ResetAt           []any
	Scope             QuotaScope
	ProbeLease        *ProbeLease
	FixedAccount      bool
	WriterGeneration  uint64
	FailoverThreshold *int
	MainIdentity      string
}

type OutcomeRecorderDependencies struct {
	Health    *HealthState
	Failures  *FailureState
	Reauth    *ReauthState
	Affinity  *ThreadAffinityState
	Rotation  *RotationState
	MainFence *MainPhysicalFence
}

type OutcomeRecorder struct {
	Health    *HealthState
	Failures  *FailureState
	Reauth    *ReauthState
	Affinity  *ThreadAffinityState
	Rotation  *RotationState
	MainFence *MainPhysicalFence

	mu                       sync.RWMutex
	lastReconciledGeneration uint64
	liveAccountIDs           map[string]struct{}
}

func NewOutcomeRecorder(deps OutcomeRecorderDependencies) *OutcomeRecorder {
	if deps.Health == nil {
		deps.Health = NewHealthState()
	}
	if deps.Failures == nil {
		deps.Failures = NewFailureState()
	}
	if deps.Reauth == nil {
		deps.Reauth = NewReauthState()
	}
	if deps.Affinity == nil {
		deps.Affinity = NewThreadAffinityState()
	}
	if deps.Rotation == nil {
		deps.Rotation = NewRotationState()
	}
	if deps.MainFence == nil {
		deps.MainFence = NewMainPhysicalFence(deps.Health, deps.Failures, deps.Reauth, deps.Affinity)
	}
	return &OutcomeRecorder{
		Health: deps.Health, Failures: deps.Failures, Reauth: deps.Reauth, Affinity: deps.Affinity, Rotation: deps.Rotation,
		MainFence: deps.MainFence, liveAccountIDs: make(map[string]struct{}),
	}
}

func ClassifyUpstreamOutcome(outcome UpstreamOutcome, denial OutcomeDenial) OutcomeClass {
	switch outcome.Kind {
	case OutcomeConnectNeutral:
		return OutcomeNeutral
	case OutcomeConnectError, OutcomeTimeout:
		return OutcomeTransient
	case OutcomeHTTP, "":
		status := outcome.StatusCode
		switch {
		case status >= 200 && status < 300:
			return OutcomeSuccess
		case status >= 300 && status < 400:
			return OutcomeNeutral
		case status == 403 && (denial == DenialWorkspace || denial == DenialEntitlement):
			return OutcomeWorkspace
		case status == 401 || status == 403:
			return OutcomeCredential
		case status == 402 || status == 429:
			return OutcomeQuota
		case status >= 400 && status < 500:
			return OutcomeCaller
		case status >= 500 && status < 600:
			return OutcomeTransient
		default:
			return OutcomeUnknown
		}
	default:
		return OutcomeUnknown
	}
}

func ExtractOutcomeDenial(body []byte) OutcomeDenial {
	var root map[string]any
	if json.Unmarshal(body, &root) != nil || root == nil {
		return ""
	}
	code := structuredDenialCode(root)
	switch code {
	case "codex_workspace_access_denied", "workspace_access_denied", "invalid_workspace_selected":
		return DenialWorkspace
	case "codex_entitlement_missing", "entitlement_missing":
		return DenialEntitlement
	default:
		return ""
	}
}

func structuredDenialCode(root map[string]any) string {
	if value, ok := root["detail"]; ok {
		if code := stringCode(value); code != "" {
			return code
		}
		if jsonObjectLike(value) {
			return ""
		}
	}
	if value, ok := root["error"]; ok {
		if code := stringCode(value); code != "" {
			return code
		}
		if jsonObjectLike(value) {
			return ""
		}
	}
	text, _ := root["code"].(string)
	return text
}

func stringCode(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	text, _ := object["code"].(string)
	return text
}

func ComputeOutcomeQuotaCooldown(meta OutcomeMeta) Cooldown {
	now := meta.Now
	if now.IsZero() {
		now = time.Now()
	}
	if duration, ok := parseRetryAfterDuration(meta.RetryAfter, now); ok {
		return Cooldown{Until: now.Add(duration), Since: now, Source: CooldownSourceRetryAfter}
	}
	if duration, ok := parseResetCooldownDuration(meta.ResetAt, now); ok {
		return Cooldown{Until: now.Add(duration), Since: now, Source: CooldownSourceResetDerived}
	}
	return Cooldown{Until: now.Add(DefaultQuotaCooldown), Since: now, Source: CooldownSourceDefault}
}

func (r *OutcomeRecorder) Reconcile(generation uint64, liveAccountIDs map[string]struct{}) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation <= r.lastReconciledGeneration {
		return 0
	}
	r.lastReconciledGeneration = generation
	r.liveAccountIDs = cloneAccountIDSet(liveAccountIDs)
	removed := 0
	removed += r.Health.Reconcile(generation, liveAccountIDs)
	removed += r.Failures.Reconcile(generation, liveAccountIDs)
	removed += r.Reauth.Reconcile(generation, liveAccountIDs)
	removed += r.Rotation.Reconcile(generation, liveAccountIDs)
	return removed
}

func (r *OutcomeRecorder) Record(accountID string, outcome UpstreamOutcome, meta OutcomeMeta) OutcomeClass {
	if accountID == "" {
		return ClassifyUpstreamOutcome(outcome, meta.Denial)
	}
	if meta.Now.IsZero() {
		meta.Now = time.Now()
	}
	class := ClassifyUpstreamOutcome(outcome, meta.Denial)
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.writerMayRecordLocked(accountID, meta.WriterGeneration) {
		return class
	}
	if accountID == MainAccountID && !r.MainFence.Accept(meta.MainIdentity) {
		return class
	}
	threshold := outcomeFailoverThreshold(meta.FailoverThreshold)
	lease := ownedMetaLease(accountID, meta.ProbeLease)
	switch class {
	case OutcomeSuccess:
		if lease != nil {
			cleared := r.Health.CompleteProbeSuccess(*lease, meta.Now)
			if cleared && lease.Scope == "" {
				// An account-wide recovery probe proves the entire account-wide
				// health object recovered, including its transient streak.
				r.Health.ClearSoftAvoid(accountID)
				r.Failures.Clear(accountID)
				return class
			}
		}
		r.Health.ClearSoftAvoid(accountID)
		r.Failures.RecordSuccess(accountID, meta.Now, threshold)
		return class
	case OutcomeCaller, OutcomeNeutral:
		if lease != nil {
			r.Health.ReleaseProbe(*lease, meta.Now)
		}
		return class
	case OutcomeWorkspace:
		if lease != nil {
			r.Health.ReleaseProbe(*lease, meta.Now)
		}
		r.Failures.RecordFailure(accountID, meta.Now, outcomeStatus(outcome), meta.WriterGeneration)
		return class
	case OutcomeCredential:
		r.Health.ClearAccount(accountID)
		r.Reauth.Mark(accountID, meta.WriterGeneration)
		r.Failures.Clear(accountID)
		r.Failures.RecordFailure(accountID, meta.Now, outcomeStatus(outcome), meta.WriterGeneration)
		r.Affinity.ClearAccount(accountID)
		return class
	case OutcomeQuota:
		cooldown := ComputeOutcomeQuotaCooldown(meta)
		if lease != nil {
			r.Health.ReleaseProbe(*lease, meta.Now)
		}
		if cooldown.Source == CooldownSourceResetDerived && meta.Scope != "" {
			r.Health.SetScopedHardCooldownAt(accountID, meta.Scope, meta.Now, cooldown.Until, cooldown.Source)
			if !meta.FixedAccount && meta.Scope == QuotaScopeShared {
				r.Affinity.ClearAccount(accountID)
				r.Rotation.NoteFailure("codex", accountID)
			}
			return class
		}
		r.Health.SetHardCooldownAt(accountID, meta.Now, cooldown.Until, cooldown.Source)
		r.Health.ClearSoftAvoid(accountID)
		r.Failures.Clear(accountID)
		if !meta.FixedAccount {
			r.Affinity.ClearAccount(accountID)
			if meta.Scope != QuotaScopeSpark {
				r.Rotation.NoteFailure("codex", accountID)
			}
		}
		return class
	case OutcomeTransient, OutcomeUnknown:
		if lease != nil {
			r.Health.ReleaseProbe(*lease, meta.Now)
		}
		r.Failures.RecordFailure(accountID, meta.Now, outcomeStatus(outcome), meta.WriterGeneration)
		failure, ok := r.Failures.Snapshot(accountID)
		if !ok || threshold <= 0 || failure.ConsecutiveFailures < threshold {
			return class
		}
		escalation := transientSoftAvoidDuration(failure.ConsecutiveFailures, threshold)
		until := meta.Now.Add(escalation)
		if existing, exists := r.Health.SoftAvoidUntil(accountID, meta.Now); exists && existing.After(until) {
			until = existing
		}
		r.Health.SetSoftAvoid(accountID, until)
		if !meta.FixedAccount {
			r.Affinity.ClearAccount(accountID)
		}
		return class
	}
	return class
}

func (r *OutcomeRecorder) writerMayRecordLocked(accountID string, writerGeneration uint64) bool {
	if writerGeneration >= r.lastReconciledGeneration {
		return true
	}
	_, live := r.liveAccountIDs[accountID]
	return live
}

func parseRetryAfterDuration(value string, now time.Time) (time.Duration, bool) {
	text := strings.TrimSpace(value)
	if text == "" {
		return 0, false
	}
	if retryAfterSecondsPattern.MatchString(text) {
		seconds, err := strconv.ParseFloat(text, 64)
		if err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) && seconds > 0 {
			milliseconds := math.Ceil(seconds * 1000)
			if milliseconds > float64(MaxQuotaCooldown/time.Millisecond) {
				milliseconds = float64(MaxQuotaCooldown / time.Millisecond)
			}
			if milliseconds < 1 {
				milliseconds = 1
			}
			return time.Duration(milliseconds) * time.Millisecond, true
		}
	}
	if parsed, err := http.ParseTime(text); err == nil {
		delay := parsed.Sub(now)
		if delay > 0 {
			return clampQuotaDuration(delay), true
		}
	}
	return 0, false
}

func parseResetCooldownDuration(values []any, now time.Time) (time.Duration, bool) {
	var best time.Duration
	found := false
	for _, value := range values {
		milliseconds, ok := resetTimestampMilliseconds(value)
		if !ok {
			continue
		}
		delayMilliseconds := milliseconds - float64(now.UnixMilli())
		if delayMilliseconds <= 0 || math.IsNaN(delayMilliseconds) || math.IsInf(delayMilliseconds, 0) {
			continue
		}
		delay := time.Duration(math.Min(delayMilliseconds, float64(MaxQuotaCooldown/time.Millisecond))) * time.Millisecond
		if delay > MaxResetDerivedQuotaCooldown {
			delay = MaxResetDerivedQuotaCooldown
		}
		if delay < time.Millisecond {
			delay = time.Millisecond
		}
		if !found || delay < best {
			best = delay
			found = true
		}
	}
	return best, found
}

func resetTimestampMilliseconds(value any) (float64, bool) {
	var numeric float64
	switch typed := value.(type) {
	case int:
		numeric = float64(typed)
	case int64:
		numeric = float64(typed)
	case uint64:
		numeric = float64(typed)
	case float64:
		numeric = typed
	case float32:
		numeric = float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		numeric = parsed
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return 0, false
		}
		numeric = parsed
	default:
		return 0, false
	}
	if numeric <= 0 || math.IsNaN(numeric) || math.IsInf(numeric, 0) {
		return 0, false
	}
	if numeric < 1_000_000_000_000 {
		numeric *= 1000
	}
	return numeric, true
}
func clampQuotaDuration(duration time.Duration) time.Duration {
	if duration < time.Millisecond {
		return time.Millisecond
	}
	if duration > MaxQuotaCooldown {
		return MaxQuotaCooldown
	}
	return duration
}
func outcomeStatus(outcome UpstreamOutcome) int {
	if outcome.Kind == OutcomeHTTP || outcome.Kind == "" {
		return outcome.StatusCode
	}
	return 0
}
func outcomeFailoverThreshold(value *int) int {
	if value == nil {
		return 3
	}
	return *value
}
func transientSoftAvoidDuration(consecutiveFailures, threshold int) time.Duration {
	levels := [...]time.Duration{TransientSoftAvoidInitial, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute}
	index := consecutiveFailures - threshold
	if index < 0 {
		index = 0
	}
	if index >= len(levels) {
		index = len(levels) - 1
	}
	return levels[index]
}
func ownedMetaLease(accountID string, lease *ProbeLease) *ProbeLease {
	if lease == nil || lease.AccountID != accountID || lease.LeaseID == "" {
		return nil
	}
	copy := *lease
	return &copy
}
