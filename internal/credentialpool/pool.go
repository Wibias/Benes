package credentialpool

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNoEligibleCandidate = errors.New("no eligible credential candidate")
	ErrAffinityUnavailable = errors.New("affinity-bound credential is unavailable")
	ErrFailureNotEligible  = errors.New("failure is not eligible for automatic credential failover")
	ErrOutputCommitted     = errors.New("credential failover is forbidden after downstream output commit")
)

type AuthState string

const (
	AuthUsable         AuthState = "usable"
	AuthReauthRequired AuthState = "reauth_required"
)

type QuotaState string

const (
	QuotaUnknown QuotaState = "unknown"
	QuotaKnown   QuotaState = "known"
)

type LimitState string

const (
	LimitAvailable        LimitState = "available"
	LimitRateLimited      LimitState = "rate_limited"
	LimitExhausted        LimitState = "exhausted"
	LimitGeoBlocked       LimitState = "geoblocked"
	LimitPermissionDenied LimitState = "permission_denied"
	LimitUnusable         LimitState = "unusable"
)

type FailureClass string

const (
	FailureAuthUnusable     FailureClass = "auth_unusable"
	FailureQuotaExhausted   FailureClass = "quota_exhausted"
	FailureRateLimited      FailureClass = "rate_limited"
	FailureGeoBlocked       FailureClass = "geoblocked"
	FailurePermissionDenied FailureClass = "permission_denied"
	FailureTerminalUnusable FailureClass = "terminal_unusable"
	FailureGenericForbidden FailureClass = "generic_forbidden"
)

type Reason string

const (
	ReasonSelected         Reason = "selected"
	ReasonAffinity         Reason = "affinity"
	ReasonQuotaUnknown     Reason = "quota_unknown"
	ReasonFailedOver       Reason = "failed_over"
	ReasonReauthRequired   Reason = "reauth_required"
	ReasonCooldown         Reason = "cooldown"
	ReasonExhausted        Reason = "exhausted"
	ReasonGeoBlocked       Reason = "geoblocked"
	ReasonPermissionDenied Reason = "permission_denied"
	ReasonUnusable         Reason = "unusable"
	ReasonDestination      Reason = "destination_mismatch"
	ReasonAuthClass        Reason = "auth_class_mismatch"
)

type Availability string

const (
	AvailabilityAvailable      Availability = "available"
	AvailabilityUnknown        Availability = "unknown"
	AvailabilityExhausted      Availability = "exhausted"
	AvailabilityReauthRequired Availability = "reauth_required"
	AvailabilityLimited        Availability = "limited"
	AvailabilityUnavailable    Availability = "unavailable"
)

type Evidence struct {
	Auth           AuthState
	Quota          QuotaState
	Utilization    float64
	ObservedAt     time.Time
	ValidUntil     time.Time
	Limit          LimitState
	ResetAt        time.Time
	OverageEnabled bool
}

type Candidate struct {
	Ref                 string
	ProviderLabel       string
	Destination         string
	AuthClass           string
	Priority            int
	AuthorityGeneration uint64
	RoutingMetadata     map[string]string
	Evidence            Evidence
}

type Request struct {
	AffinityKey string
	Destination string
	AuthClass   string
	Portable    bool
	Committed   bool
	Now         time.Time
}

type Failure struct {
	Class   FailureClass
	RetryAt time.Time
}

type Observation struct {
	CandidateRef string
	Reason       Reason
}

type Decision struct {
	Candidate Candidate
	Reason    Reason
	Trace     []Observation
}

type Pool struct {
	mu         sync.Mutex
	candidates map[string]Candidate
	affinities map[string]string
}

func New(candidates []Candidate) *Pool {
	pool := &Pool{
		candidates: make(map[string]Candidate, len(candidates)),
		affinities: make(map[string]string),
	}
	for _, candidate := range candidates {
		pool.upsertLocked(candidate)
	}
	return pool
}

func (p *Pool) Upsert(candidate Candidate) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureMapsLocked()
	p.upsertLocked(candidate)
}

func (p *Pool) ReportEvidence(ref string, evidence Evidence) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	candidate, exists := p.candidates[ref]
	if !exists {
		return
	}
	candidate.Evidence = evidence
	p.candidates[ref] = candidate
}

func (p *Pool) ReportFailure(ref string, failure Failure) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reportFailureLocked(ref, failure)
}

func (p *Pool) Select(request Request) (Decision, error) {
	if p == nil {
		return Decision{}, ErrNoEligibleCandidate
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.selectLocked(normalizeRequest(request), "", false)
}

func (p *Pool) NextAfterFailure(request Request, previousRef string, failure Failure) (Decision, error) {
	if p == nil {
		return Decision{}, ErrNoEligibleCandidate
	}
	request = normalizeRequest(request)
	if request.Committed {
		return Decision{}, ErrOutputCommitted
	}
	if !automaticFailoverEligible(failure.Class) {
		return Decision{}, ErrFailureNotEligible
	}
	if !request.Portable {
		return Decision{}, ErrFailureNotEligible
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	p.reportFailureLocked(previousRef, failure)
	decision, err := p.selectLocked(request, previousRef, true)
	if err != nil {
		return Decision{}, err
	}
	decision.Reason = ReasonFailedOver
	if request.AffinityKey != "" {
		p.affinities[request.AffinityKey] = decision.Candidate.Ref
	}
	return decision, nil
}

func (p *Pool) selectLocked(request Request, excludeRef string, failover bool) (Decision, error) {
	p.ensureMapsLocked()

	if request.AffinityKey != "" {
		if ref, bound := p.affinities[request.AffinityKey]; bound && ref != excludeRef {
			if candidate, exists := p.candidates[ref]; exists {
				candidate = refreshCandidate(candidate, request.Now)
				p.candidates[ref] = candidate
				eligible, _, _ := assess(candidate, request)
				if eligible {
					return Decision{Candidate: cloneCandidate(candidate), Reason: ReasonAffinity}, nil
				}
				if !request.Portable {
					return Decision{}, ErrAffinityUnavailable
				}
			}
		}
	}

	type ranked struct {
		candidate Candidate
		unknown   bool
	}
	var eligible []ranked
	trace := make([]Observation, 0, len(p.candidates))
	for ref, stored := range p.candidates {
		candidate := refreshCandidate(stored, request.Now)
		p.candidates[ref] = candidate
		if ref == excludeRef {
			continue
		}
		ok, reason, unknown := assess(candidate, request)
		if !ok {
			if reason != "" {
				trace = append(trace, Observation{CandidateRef: candidate.Ref, Reason: reason})
			}
			continue
		}
		eligible = append(eligible, ranked{candidate: candidate, unknown: unknown})
	}
	if len(eligible) == 0 {
		return Decision{}, ErrNoEligibleCandidate
	}

	sort.SliceStable(eligible, func(i, j int) bool {
		left, right := eligible[i], eligible[j]
		if left.unknown != right.unknown {
			return !left.unknown
		}
		if left.candidate.Priority != right.candidate.Priority {
			return left.candidate.Priority > right.candidate.Priority
		}
		if !left.unknown && left.candidate.Evidence.Utilization != right.candidate.Evidence.Utilization {
			return left.candidate.Evidence.Utilization < right.candidate.Evidence.Utilization
		}
		return left.candidate.Ref < right.candidate.Ref
	})

	chosen := eligible[0]
	reason := ReasonSelected
	if chosen.unknown {
		reason = ReasonQuotaUnknown
	}
	if failover {
		reason = ReasonFailedOver
	}
	trace = append(trace, Observation{CandidateRef: chosen.candidate.Ref, Reason: reason})
	if request.AffinityKey != "" {
		p.affinities[request.AffinityKey] = chosen.candidate.Ref
	}
	return Decision{Candidate: cloneCandidate(chosen.candidate), Reason: reason, Trace: trace}, nil
}

func EvaluateEvidence(evidence Evidence, now time.Time) Availability {
	evidence = normalizeEvidence(evidence, now)
	if evidence.Auth != AuthUsable {
		return AvailabilityReauthRequired
	}
	switch evidence.Limit {
	case LimitRateLimited:
		return AvailabilityLimited
	case LimitExhausted:
		return AvailabilityExhausted
	case LimitGeoBlocked, LimitPermissionDenied, LimitUnusable:
		return AvailabilityUnavailable
	}
	quotaKnown := evidence.Quota == QuotaKnown &&
		(evidence.ValidUntil.IsZero() || now.Before(evidence.ValidUntil) || now.Equal(evidence.ValidUntil))
	if !quotaKnown {
		return AvailabilityUnknown
	}
	if evidence.Utilization >= 1 && !evidence.OverageEnabled {
		return AvailabilityExhausted
	}
	return AvailabilityAvailable
}

func assess(candidate Candidate, request Request) (eligible bool, reason Reason, quotaUnknown bool) {
	if candidate.Destination != request.Destination {
		return false, ReasonDestination, false
	}
	if candidate.AuthClass != request.AuthClass {
		return false, ReasonAuthClass, false
	}
	switch EvaluateEvidence(candidate.Evidence, request.Now) {
	case AvailabilityAvailable:
		return true, "", false
	case AvailabilityUnknown:
		return true, ReasonQuotaUnknown, true
	case AvailabilityExhausted:
		return false, ReasonExhausted, false
	case AvailabilityReauthRequired:
		return false, ReasonReauthRequired, false
	case AvailabilityLimited:
		return false, ReasonCooldown, false
	case AvailabilityUnavailable:
		switch candidate.Evidence.Limit {
		case LimitGeoBlocked:
			return false, ReasonGeoBlocked, false
		case LimitPermissionDenied:
			return false, ReasonPermissionDenied, false
		default:
			return false, ReasonUnusable, false
		}
	default:
		return false, ReasonUnusable, false
	}
}

func normalizeEvidence(evidence Evidence, now time.Time) Evidence {
	if evidence.ResetAt.IsZero() || now.Before(evidence.ResetAt) {
		return evidence
	}
	switch evidence.Limit {
	case LimitRateLimited:
		evidence.Limit = LimitAvailable
		evidence.ResetAt = time.Time{}
	case LimitExhausted:
		evidence.Limit = LimitAvailable
		evidence.ResetAt = time.Time{}
		evidence.Quota = QuotaUnknown
		evidence.Utilization = 0
		evidence.ValidUntil = time.Time{}
	}
	return evidence
}

func refreshCandidate(candidate Candidate, now time.Time) Candidate {
	candidate.Evidence = normalizeEvidence(candidate.Evidence, now)
	return candidate
}

func (p *Pool) reportFailureLocked(ref string, failure Failure) {
	candidate, exists := p.candidates[ref]
	if !exists {
		return
	}
	switch failure.Class {
	case FailureAuthUnusable:
		candidate.Evidence.Auth = AuthReauthRequired
	case FailureQuotaExhausted:
		candidate.Evidence.Quota = QuotaKnown
		candidate.Evidence.Utilization = 1
		candidate.Evidence.Limit = LimitExhausted
		candidate.Evidence.ResetAt = failure.RetryAt
	case FailureRateLimited:
		candidate.Evidence.Limit = LimitRateLimited
		candidate.Evidence.ResetAt = failure.RetryAt
	case FailureGeoBlocked:
		candidate.Evidence.Limit = LimitGeoBlocked
	case FailurePermissionDenied:
		candidate.Evidence.Limit = LimitPermissionDenied
	case FailureTerminalUnusable:
		candidate.Evidence.Limit = LimitUnusable
	case FailureGenericForbidden:
		return
	default:
		return
	}
	p.candidates[ref] = candidate
}

func automaticFailoverEligible(class FailureClass) bool {
	switch class {
	case FailureAuthUnusable, FailureQuotaExhausted, FailureRateLimited, FailureGeoBlocked, FailurePermissionDenied, FailureTerminalUnusable:
		return true
	default:
		return false
	}
}

func normalizeRequest(request Request) Request {
	request.Destination = strings.TrimSpace(request.Destination)
	request.AuthClass = strings.TrimSpace(request.AuthClass)
	if request.Now.IsZero() {
		request.Now = time.Now()
	}
	return request
}

func (p *Pool) ensureMapsLocked() {
	if p.candidates == nil {
		p.candidates = make(map[string]Candidate)
	}
	if p.affinities == nil {
		p.affinities = make(map[string]string)
	}
}

func (p *Pool) upsertLocked(candidate Candidate) {
	p.ensureMapsLocked()
	candidate.Ref = strings.TrimSpace(candidate.Ref)
	candidate.Destination = strings.TrimSpace(candidate.Destination)
	candidate.AuthClass = strings.TrimSpace(candidate.AuthClass)
	if candidate.Ref == "" || candidate.Destination == "" || candidate.AuthClass == "" {
		return
	}
	p.candidates[candidate.Ref] = cloneCandidate(candidate)
}

func cloneCandidate(candidate Candidate) Candidate {
	clone := candidate
	if candidate.RoutingMetadata != nil {
		clone.RoutingMetadata = make(map[string]string, len(candidate.RoutingMetadata))
		for key, value := range candidate.RoutingMetadata {
			clone.RoutingMetadata[key] = value
		}
	}
	return clone
}
