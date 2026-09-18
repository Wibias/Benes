package router

import (
	"fmt"
	"strings"
)

type Route struct {
	Provider       string
	Model          string
	CodexAccountID string
}

func ParseExplicit(selector string) (Route, error) {
	return ParseExplicitWithCodexAccounts(selector, nil)
}

func ParseExplicitWithCodexAccounts(selector string, namespaces map[string]string) (Route, error) {
	provider, model, found := strings.Cut(selector, "/")
	if !found || provider == "" || model == "" {
		return Route{}, fmt.Errorf("model selector must contain non-empty provider and model")
	}
	if strings.HasPrefix(model, "/") || strings.HasSuffix(model, "/") || strings.Contains(model, "//") {
		return Route{}, fmt.Errorf("model selector contains an empty model path segment")
	}
	if accountID, ok := namespaces[provider]; ok {
		return Route{Provider: "openai", Model: model, CodexAccountID: accountID}, nil
	}
	return Route{Provider: provider, Model: model}, nil
}
