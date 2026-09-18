package server

import (
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/router"
)

func TestResolveProviderDoesNotCrossOpenAIAccessLanes(t *testing.T) {
	tests := []struct {
		name          string
		defaultAccess string
		providers     map[string]Provider
		route         router.Route
		wantMissing   bool
		wantID        string
	}{
		{
			name:          "api default does not fall back to oauth",
			defaultAccess: config.DefaultAccessAPI,
			providers:     map[string]Provider{config.LogicalOpenAIID: nil},
			route:         router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5.6"},
			wantMissing:   true,
		},
		{
			name:          "oauth default does not fall back to api",
			defaultAccess: config.DefaultAccessOAuth,
			providers:     map[string]Provider{config.OpenAIAPIConnection: nil},
			route:         router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5.6"},
			wantMissing:   true,
		},
		{
			name:          "exact account stays on oauth when api is default",
			defaultAccess: config.DefaultAccessAPI,
			providers:     map[string]Provider{config.LogicalOpenAIID: nil},
			route:         router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5.6", CodexAccountID: "acct-1"},
			wantID:        config.LogicalOpenAIID,
		},
		{
			name:          "explicit api stays on api when oauth is default",
			defaultAccess: config.DefaultAccessOAuth,
			providers:     map[string]Provider{config.OpenAIAPIConnection: nil},
			route:         router.Route{Provider: config.OpenAIAPIConnection, Model: "gpt-5.6"},
			wantID:        config.OpenAIAPIConnection,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cacheKey := "test://" + t.Name()
			providerDefaultAccessCache.Store(cacheKey, tt.defaultAccess)
			t.Cleanup(func() { providerDefaultAccessCache.Delete(cacheKey) })
			h := &handler{configPath: cacheKey, providers: tt.providers}
			got := h.resolveProvider(tt.route)
			if got.MissingProvider != tt.wantMissing {
				t.Fatalf("MissingProvider=%v want=%v", got.MissingProvider, tt.wantMissing)
			}
			if !tt.wantMissing && got.ProviderID != tt.wantID {
				t.Fatalf("ProviderID=%q want=%q", got.ProviderID, tt.wantID)
			}
		})
	}
}
