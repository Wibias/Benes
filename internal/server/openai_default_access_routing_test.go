package server

import (
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/router"
)

func TestResolveLogicalOpenAIRouteUsesConfiguredDefaultOnly(t *testing.T) {
	tests := []struct {
		name          string
		route         router.Route
		defaultAccess string
		wantProvider  string
	}{
		{
			name:          "logical oauth default",
			route:         router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5.6"},
			defaultAccess: config.DefaultAccessOAuth,
			wantProvider:  config.LogicalOpenAIID,
		},
		{
			name:          "logical api default",
			route:         router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5.6"},
			defaultAccess: config.DefaultAccessAPI,
			wantProvider:  config.OpenAIAPIConnection,
		},
		{
			name:          "exact account remains oauth",
			route:         router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5.6", CodexAccountID: "acct-1"},
			defaultAccess: config.DefaultAccessAPI,
			wantProvider:  config.LogicalOpenAIID,
		},
		{
			name:          "explicit api remains api",
			route:         router.Route{Provider: config.OpenAIAPIConnection, Model: "gpt-5.6"},
			defaultAccess: config.DefaultAccessOAuth,
			wantProvider:  config.OpenAIAPIConnection,
		},
		{
			name:          "other provider remains unchanged",
			route:         router.Route{Provider: "anthropic", Model: "claude-sonnet"},
			defaultAccess: config.DefaultAccessAPI,
			wantProvider:  "anthropic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveLogicalOpenAIRoute(tt.route, tt.defaultAccess)
			if got.Provider != tt.wantProvider {
				t.Fatalf("provider=%q want=%q", got.Provider, tt.wantProvider)
			}
			if got.Model != tt.route.Model {
				t.Fatalf("model changed from %q to %q", tt.route.Model, got.Model)
			}
			if got.CodexAccountID != tt.route.CodexAccountID {
				t.Fatalf("account changed from %q to %q", tt.route.CodexAccountID, got.CodexAccountID)
			}
		})
	}
}
