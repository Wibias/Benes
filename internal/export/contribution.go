package export

import "fmt"

type Fragment struct {
	Path  []string
	Value any
}

type Contribution struct {
	Client    string
	Fragments []Fragment
}

func lookupMap(doc any, keys ...string) (map[string]any, bool) {
	cur, ok := doc.(map[string]any)
	if !ok {
		return nil, false
	}
	for i, key := range keys {
		next, exists := cur[key]
		if !exists {
			return nil, false
		}
		if i == len(keys)-1 {
			m, ok := next.(map[string]any)
			return m, ok
		}
		cur, ok = next.(map[string]any)
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func Contribute(client string, ctx Context) (Contribution, error) {
	spec, ok := specs[client]
	if !ok {
		return Contribution{}, fmt.Errorf("unknown client %q", client)
	}
	doc, err := spec.build(ctx)
	if err != nil {
		return Contribution{}, err
	}
	stampHarnessIdentity(client, doc)
	contrib := Contribution{Client: client}
	switch client {
	case "opencode":
		block, ok := lookupMap(doc, "provider", ProviderID)
		if !ok {
			return Contribution{}, fmt.Errorf("opencode contribution missing")
		}
		contrib.Fragments = []Fragment{{Path: []string{"provider", ProviderID}, Value: block}}
	case "pi", "prime", "omp", "hermes", "gajae":
		block, ok := lookupMap(doc, "providers", ProviderID)
		if !ok {
			return Contribution{}, fmt.Errorf("%s contribution missing", client)
		}
		contrib.Fragments = []Fragment{{Path: []string{"providers", ProviderID}, Value: block}}
	case "openclaw":
		block, ok := lookupMap(doc, "models", "providers", ProviderID)
		if !ok {
			return Contribution{}, fmt.Errorf("openclaw contribution missing")
		}
		contrib.Fragments = []Fragment{{Path: []string{"models", "providers", ProviderID}, Value: block}}
	case "kimi":
		block, ok := lookupMap(doc, "providers", ProviderID)
		if !ok {
			return Contribution{}, fmt.Errorf("kimi contribution missing")
		}
		contrib.Fragments = []Fragment{{Path: []string{"providers", ProviderID}, Value: block}}
		if models, ok := lookupMap(doc, "models"); ok {
			for alias, value := range models {
				contrib.Fragments = append(contrib.Fragments, Fragment{Path: []string{"models", alias}, Value: value})
			}
		}
	case "dsh":
		block, ok := lookupMap(doc, "llm-pi-ai", "providers", ProviderID)
		if !ok {
			return Contribution{}, fmt.Errorf("dsh contribution missing")
		}
		contrib.Fragments = []Fragment{{Path: []string{"llm-pi-ai", "providers", ProviderID}, Value: block}}
	case "mcode":
		block, ok := lookupMap(doc, "custom_provider", ProviderID)
		if !ok {
			return Contribution{}, fmt.Errorf("mcode contribution missing")
		}
		contrib.Fragments = []Fragment{{Path: []string{"custom_provider", ProviderID}, Value: block}}
	default:
		return Contribution{}, fmt.Errorf("unknown client %q", client)
	}
	return contrib, nil
}
