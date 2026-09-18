package storage

import (
	"encoding/json"
	"strings"
	"time"
)

type PolicyTrigger struct {
	ArchivedBytesOver int64 `json:"archivedBytesOver"`
}

type PolicyTarget struct {
	ReduceToBytes       *int64 `json:"reduceToBytes,omitempty"`
	RemoveOldestPercent *int   `json:"removeOldestPercent,omitempty"`
}

type PolicyLastRun struct {
	At         int64 `json:"at"`
	FreedBytes int64 `json:"freedBytes"`
	Removed    int   `json:"removed"`
}

type JobOutcome struct {
	OK         bool   `json:"ok"`
	Skipped    string `json:"skipped,omitempty"`
	Deferred   string `json:"deferred,omitempty"`
	Error      string `json:"error,omitempty"`
	Mode       string `json:"mode,omitempty"`
	FreedBytes int64  `json:"freedBytes,omitempty"`
	Removed    int    `json:"removed,omitempty"`
}

type PolicyJob struct {
	Status      string      `json:"status"`
	Reason      string      `json:"reason,omitempty"`
	StartedAt   int64       `json:"startedAt,omitempty"`
	FinishedAt  int64       `json:"finishedAt,omitempty"`
	LastError   string      `json:"lastError,omitempty"`
	LastOutcome *JobOutcome `json:"lastOutcome,omitempty"`
	Generation  int64       `json:"generation,omitempty"`
}

type Policy struct {
	Enabled  bool           `json:"enabled"`
	Trigger  PolicyTrigger  `json:"trigger"`
	Target   PolicyTarget   `json:"target"`
	Schedule string         `json:"schedule"`
	Mode     string         `json:"mode"`
	LastRun  *PolicyLastRun `json:"lastRun,omitempty"`
	NextRun  *int64         `json:"nextRun,omitempty"`
	Job      *PolicyJob     `json:"job,omitempty"`
}

func DefaultPolicy() Policy {
	pct := 25
	return Policy{
		Enabled:  false,
		Trigger:  PolicyTrigger{ArchivedBytesOver: 5 * 1024 * 1024 * 1024},
		Target:   PolicyTarget{RemoveOldestPercent: &pct},
		Schedule: ScheduleManual,
		Mode:     ModeQuarantine,
		Job:      &PolicyJob{Status: JobIdle},
	}
}

func ParsePolicy(raw json.RawMessage) (Policy, error) {
	policy := DefaultPolicy()
	if len(raw) == 0 {
		return policy, nil
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		return Policy{}, coded(CodeInvalidPolicy, "invalid policy JSON")
	}
	return NormalizePolicy(policy)
}

func NormalizePolicy(policy Policy) (Policy, error) {
	policy.Schedule = strings.TrimSpace(policy.Schedule)
	if policy.Schedule == "" {
		policy.Schedule = ScheduleManual
	}
	switch policy.Schedule {
	case ScheduleStartup, ScheduleDaily, ScheduleWeekly, ScheduleManual:
	default:
		return Policy{}, coded(CodeInvalidPolicy, "schedule must be startup, daily, weekly, or manual")
	}
	policy.Mode = strings.TrimSpace(policy.Mode)
	if policy.Mode == "" {
		policy.Mode = ModeQuarantine
	}
	if policy.Mode != ModeQuarantine && policy.Mode != ModePermanent {
		return Policy{}, coded(CodeInvalidPolicy, "mode must be quarantine or permanent")
	}
	if policy.Trigger.ArchivedBytesOver < 0 {
		return Policy{}, coded(CodeInvalidPolicy, "archivedBytesOver must be >= 0")
	}
	hasReduce := policy.Target.ReduceToBytes != nil
	hasPct := policy.Target.RemoveOldestPercent != nil
	if hasReduce && hasPct {
		return Policy{}, coded(CodeInvalidPolicy, "target modes are mutually exclusive")
	}
	if !hasReduce && !hasPct {
		pct := 25
		policy.Target.RemoveOldestPercent = &pct
		hasPct = true
	}
	if hasReduce && *policy.Target.ReduceToBytes < 0 {
		return Policy{}, coded(CodeInvalidPolicy, "reduceToBytes must be >= 0")
	}
	if hasPct {
		pct := *policy.Target.RemoveOldestPercent
		if pct < 1 || pct > 100 {
			return Policy{}, coded(CodeInvalidPolicy, "removeOldestPercent must be 1-100")
		}
	}
	if policy.Job == nil {
		policy.Job = &PolicyJob{Status: JobIdle}
	} else if policy.Job.Status != JobRunning {
		policy.Job.Status = JobIdle
	}
	return policy, nil
}

func NextRunAt(policy Policy, now time.Time, loc *time.Location) *int64 {
	if !policy.Enabled || policy.Schedule == ScheduleManual {
		return nil
	}
	if loc == nil {
		loc = time.Local
	}
	now = now.In(loc)
	base := now
	if policy.Job != nil && policy.Job.FinishedAt > 0 {
		base = time.UnixMilli(policy.Job.FinishedAt).In(loc)
	}
	var next time.Time
	switch policy.Schedule {
	case ScheduleStartup:
		return nil
	case ScheduleDaily:
		next = nextLocalMidnight(base, loc)
		if !next.After(now) {
			next = nextLocalMidnight(now, loc)
		}
	case ScheduleWeekly:
		next = nextLocalWeek(base, loc)
		if !next.After(now) {
			next = nextLocalWeek(now, loc)
		}
	default:
		return nil
	}
	ms := next.UnixMilli()
	return &ms
}

func nextLocalMidnight(from time.Time, loc *time.Location) time.Time {
	local := from.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	next := day.AddDate(0, 0, 1)
	return time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, loc)
}

func nextLocalWeek(from time.Time, loc *time.Location) time.Time {
	mid := nextLocalMidnight(from, loc)
	week := mid.AddDate(0, 0, 6)
	return time.Date(week.Year(), week.Month(), week.Day(), 0, 0, 0, 0, loc)
}

func ShouldRunScheduled(policy Policy, now time.Time, loc *time.Location, startup bool) bool {
	if !policy.Enabled || policy.Schedule == ScheduleManual {
		return false
	}
	if policy.Schedule == ScheduleStartup {
		return startup
	}
	if policy.NextRun != nil {
		return now.UnixMilli() >= *policy.NextRun
	}
	next := NextRunAt(policy, now, loc)
	if next == nil {
		return false
	}
	return now.UnixMilli() >= *next
}

func PreserveNextRun(current, incoming Policy, now time.Time, loc *time.Location) *int64 {
	if incoming.Enabled && (incoming.Schedule == ScheduleDaily || incoming.Schedule == ScheduleWeekly) &&
		current.Enabled && current.Schedule == incoming.Schedule && current.NextRun != nil {
		return current.NextRun
	}
	return NextRunAt(incoming, now, loc)
}

func PolicyPublic(policy Policy) Policy {
	if policy.Job != nil && policy.Job.Status == JobRunning {
		return policy
	}
	if policy.Job == nil {
		policy.Job = &PolicyJob{Status: JobIdle}
	}
	return policy
}
