package codexauth

import (
	"time"

	"github.com/Wibias/Benes/internal/credentialpool"
)

const (
	codexPoolDestination = "https://chatgpt.com/backend-api/codex"
	codexPoolAuthClass   = "oauth"
)

func (s *PoolSelector) pickLowest(input PoolSelectionInput, exclude, pinned string, threshold float64) string {
	ids := s.tierCandidates(input, exclude, pinned, threshold)
	return selectFromCredentialPool(ids, input, s)
}

func selectFromCredentialPool(ids []string, input PoolSelectionInput, selector *PoolSelector) string {
	if len(ids) == 0 {
		return ""
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	candidates := make([]credentialpool.Candidate, 0, len(ids))
	for _, id := range ids {
		usage := selector.usage(input, id)
		evidence := credentialpool.Evidence{Auth: credentialpool.AuthUsable, Limit: credentialpool.LimitAvailable, ObservedAt: now}
		if usage >= UnknownUsageScore {
			evidence.Quota = credentialpool.QuotaUnknown
		} else {
			evidence.Quota = credentialpool.QuotaKnown
			evidence.Utilization = usage / 100
			evidence.ValidUntil = now.Add(time.Minute)
		}
		candidates = append(candidates, credentialpool.Candidate{
			Ref:         id,
			Destination: codexPoolDestination,
			AuthClass:   codexPoolAuthClass,
			Priority:    AccountPriority(input.Accounts, id),
			Evidence:    evidence,
		})
	}
	decision, err := credentialpool.New(candidates).Select(credentialpool.Request{
		Destination: codexPoolDestination,
		AuthClass:   codexPoolAuthClass,
		Now:         now,
	})
	if err != nil {
		return ""
	}
	return decision.Candidate.Ref
}
