package export

import "github.com/Wibias/Benes/internal/harnessidentity"

func stampHarnessIdentity(client string, doc any) {
	root, ok := doc.(map[string]any)
	if !ok {
		return
	}

	switch client {
	case "opencode":
		if options := nestedMap(root, "provider", ProviderID, "options"); options != nil {
			setHarnessHeader(options, "headers", client)
		}
	case "hermes":
		if provider := nestedMap(root, "providers", ProviderID); provider != nil {
			setHarnessHeader(provider, "extra_headers", client)
		}
	case "openclaw":
		if provider := nestedMap(root, "models", "providers", ProviderID); provider != nil {
			setHarnessHeader(provider, "headers", client)
		}
	case "dsh":
		if provider := nestedMap(root, "llm-pi-ai", "providers", ProviderID); provider != nil {
			setHarnessHeader(provider, "headers", client)
		}
	}
}

func nestedMap(root map[string]any, path ...string) map[string]any {
	current := root
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil
		}
		current = next
	}
	return current
}

func setHarnessHeader(owner map[string]any, key, id string) {
	headers, _ := owner[key].(map[string]any)
	if headers == nil {
		headers = map[string]any{}
		owner[key] = headers
	}
	headers[harnessidentity.Header] = id
}
