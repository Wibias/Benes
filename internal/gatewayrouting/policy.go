package gatewayrouting

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	SortCost = "cost"
	SortTTFT = "ttft"
	SortTPS  = "tps"

	VercelHost = "ai-gateway.vercel.sh"
)

type Policy struct {
	Only  []string `json:"only,omitempty"`
	Order []string `json:"order,omitempty"`
	Sort  string   `json:"sort,omitempty"`
}

type Settings struct {
	Provider Policy            `json:"gatewayRouting,omitempty"`
	Models   map[string]Policy `json:"modelGatewayRouting,omitempty"`
}

func (p Policy) Empty() bool {
	return len(p.Only) == 0 && len(p.Order) == 0 && strings.TrimSpace(p.Sort) == ""
}

func (p Policy) Validate() error {
	if err := validateSlugs("only", p.Only); err != nil {
		return err
	}
	if err := validateSlugs("order", p.Order); err != nil {
		return err
	}
	sort := strings.TrimSpace(p.Sort)
	if sort != "" && sort != SortCost && sort != SortTTFT && sort != SortTPS {
		return fmt.Errorf("gateway routing sort %q is not cost, ttft, or tps", sort)
	}
	return nil
}

func (s Settings) Validate() error {
	if err := s.Provider.Validate(); err != nil {
		return err
	}
	for model, policy := range s.Models {
		if strings.TrimSpace(model) == "" {
			return fmt.Errorf("model gateway routing requires an exact model id")
		}
		if err := policy.Validate(); err != nil {
			return fmt.Errorf("model %q: %w", model, err)
		}
	}
	return nil
}

func (s Settings) Resolve(modelID string) Policy {
	modelID = strings.TrimSpace(modelID)
	if policy, ok := s.Models[modelID]; ok {
		return clonePolicy(policy)
	}
	return clonePolicy(s.Provider)
}

func CanonicalVercel(endpoint string) bool {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == VercelHost
}

func SafeView(policy Policy) map[string]any {
	out := map[string]any{}
	if len(policy.Only) > 0 {
		out["only"] = append([]string(nil), policy.Only...)
	}
	if len(policy.Order) > 0 {
		out["order"] = append([]string(nil), policy.Order...)
	}
	if sort := strings.TrimSpace(policy.Sort); sort != "" {
		out["sort"] = sort
	}
	return out
}

func validateSlugs(field string, slugs []string) error {
	seen := map[string]struct{}{}
	for _, raw := range slugs {
		slug := strings.TrimSpace(raw)
		if slug == "" || slug != raw {
			return fmt.Errorf("gateway routing %s contains an empty or padded slug", field)
		}
		for _, r := range slug {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
				continue
			}
			return fmt.Errorf("gateway routing %s slug %q is invalid", field, slug)
		}
		if _, ok := seen[slug]; ok {
			return fmt.Errorf("gateway routing %s repeats slug %q", field, slug)
		}
		seen[slug] = struct{}{}
	}
	return nil
}

func clonePolicy(policy Policy) Policy {
	out := Policy{Sort: strings.TrimSpace(policy.Sort)}
	if len(policy.Only) > 0 {
		out.Only = append([]string(nil), policy.Only...)
	}
	if len(policy.Order) > 0 {
		out.Order = append([]string(nil), policy.Order...)
	}
	return out
}
