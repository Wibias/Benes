package openairesponses

import (
	"strings"

	"github.com/Wibias/Benes/internal/credentialpool"
	"github.com/Wibias/Benes/internal/providers"
)

type APIKeySlot struct {
	ID  string
	Key string
}

type apiKeyPool struct {
	pool *credentialpool.Pool
	keys map[string]string
	dest string
}

func newAPIKeyPool(destination string, slots []APIKeySlot) *apiKeyPool {
	if len(slots) == 0 {
		return nil
	}
	candidates := make([]credentialpool.Candidate, 0, len(slots))
	keys := make(map[string]string, len(slots))
	for index, slot := range slots {
		id := strings.TrimSpace(slot.ID)
		key := strings.TrimSpace(slot.Key)
		if key == "" {
			continue
		}
		if id == "" {
			id = key
		}
		keys[id] = key
		candidates = append(candidates, credentialpool.Candidate{
			Ref:         id,
			Destination: destination,
			AuthClass:   "api-key",
			Priority:    len(slots) - index,
			Evidence:    credentialpool.Evidence{Auth: credentialpool.AuthUsable, Quota: credentialpool.QuotaUnknown, Limit: credentialpool.LimitAvailable},
		})
	}
	if len(candidates) == 0 {
		return nil
	}
	return &apiKeyPool{pool: credentialpool.New(candidates), keys: keys, dest: destination}
}

func (p *apiKeyPool) selectKey() (string, string) {
	if p == nil || p.pool == nil {
		return "", ""
	}
	decision, err := p.pool.Select(credentialpool.Request{Destination: p.dest, AuthClass: "api-key", Portable: true})
	if err != nil {
		return "", ""
	}
	return decision.Candidate.Ref, p.keys[decision.Candidate.Ref]
}

func (p *apiKeyPool) keyByRef(ref string) (string, string, bool) {
	if p == nil {
		return "", "", false
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", false
	}
	key, ok := p.keys[ref]
	if !ok || strings.TrimSpace(key) == "" {
		return "", "", false
	}
	return ref, key, true
}

func (p *apiKeyPool) eachRef() []string {
	if p == nil || len(p.keys) == 0 {
		return nil
	}
	out := make([]string, 0, len(p.keys))
	for ref := range p.keys {
		out = append(out, ref)
	}
	return out
}

func (p *apiKeyPool) failover(previous string, committed bool) (string, string) {
	if p == nil || p.pool == nil {
		return "", ""
	}
	if committed {
		return "", ""
	}
	if previous == "" {
		return p.selectKey()
	}
	decision, err := p.pool.NextAfterFailure(
		credentialpool.Request{Destination: p.dest, AuthClass: "api-key", Portable: true, Committed: committed},
		previous,
		credentialpool.Failure{Class: credentialpool.FailureRateLimited},
	)
	if err != nil {
		return "", ""
	}
	return decision.Candidate.Ref, p.keys[decision.Candidate.Ref]
}

func physicalPinFromKey(destination, ref, key string) *providers.PhysicalPin {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = "primary"
	}
	return &providers.PhysicalPin{
		Destination:   strings.TrimSpace(destination),
		CredentialRef: ref,
		AuthClass:     "api-key",
	}
}
