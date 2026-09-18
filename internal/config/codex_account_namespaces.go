package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var codexNamespaceAccountIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

var reservedCodexNamespaceAccountIDs = map[string]struct{}{
	"__proto__":   {},
	"prototype":   {},
	"constructor": {},
}

func loadCodexAccountNamespaces(
	raw json.RawMessage,
	providers map[string]json.RawMessage,
	codexAccountsRaw json.RawMessage,
) (map[string]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 || isJSONNull(raw) {
		return nil, nil
	}
	var namespaces map[string]string
	if err := json.Unmarshal(raw, &namespaces); err != nil || namespaces == nil {
		return nil, fmt.Errorf("codexAccountNamespaces must be an object of namespace-to-account strings")
	}
	privateIDs, err := loadConfiguredCodexAccountIDs(codexAccountsRaw)
	if err != nil {
		return nil, err
	}
	providerIDs := make(map[string]struct{}, len(providers))
	for id := range providers {
		providerIDs[strings.ToLower(id)] = struct{}{}
	}
	for namespace, target := range namespaces {
		if !validCodexAccountNamespace(namespace) {
			return nil, fmt.Errorf("codexAccountNamespaces namespace %q is invalid", namespace)
		}
		if _, collision := providerIDs[strings.ToLower(namespace)]; collision {
			return nil, fmt.Errorf("codexAccountNamespaces namespace %q collides with a provider id", namespace)
		}
		if _, collision := privateIDs[namespace]; collision {
			return nil, fmt.Errorf("codexAccountNamespaces namespace %q exposes a private account id", namespace)
		}
		if target != "@main" && !validCodexNamespaceAccountID(target) {
			return nil, fmt.Errorf("codexAccountNamespaces target %q is invalid", target)
		}
	}
	return namespaces, nil
}

func validCodexAccountNamespace(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.Contains(value, "/") &&
		strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

func validCodexNamespaceAccountID(value string) bool {
	if !codexNamespaceAccountIDPattern.MatchString(value) {
		return false
	}
	_, reserved := reservedCodexNamespaceAccountIDs[strings.ToLower(value)]
	return !reserved
}

func loadConfiguredCodexAccountIDs(raw json.RawMessage) (map[string]struct{}, error) {
	ids := map[string]struct{}{}
	if len(bytes.TrimSpace(raw)) == 0 || isJSONNull(raw) {
		return ids, nil
	}
	var accounts []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &accounts); err != nil {
		return nil, fmt.Errorf("codexAccounts must be an array when codexAccountNamespaces is configured")
	}
	for _, account := range accounts {
		if account.ID != "" {
			ids[account.ID] = struct{}{}
		}
	}
	return ids, nil
}
