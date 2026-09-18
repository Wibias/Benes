package server

import (
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/router"
)

func TestWebSearchProviderIDUsesResolvedPhysicalConnection(t *testing.T) {
	tests := []struct {
		name     string
		route    router.Route
		resolved resolvedProvider
		want     string
	}{
		{
			name:     "logical openai api default uses api connection",
			route:    router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5.6"},
			resolved: resolvedProvider{ProviderID: config.OpenAIAPIConnection},
			want:     config.OpenAIAPIConnection,
		},
		{
			name:     "exact account remains oauth",
			route:    router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5.6", CodexAccountID: "acct-1"},
			resolved: resolvedProvider{ProviderID: config.LogicalOpenAIID},
			want:     config.LogicalOpenAIID,
		},
		{
			name:     "explicit api remains api",
			route:    router.Route{Provider: config.OpenAIAPIConnection, Model: "gpt-5.6"},
			resolved: resolvedProvider{ProviderID: config.OpenAIAPIConnection},
			want:     config.OpenAIAPIConnection,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := webSearchProviderID(tt.route, tt.resolved); got != tt.want {
				t.Fatalf("provider=%q want=%q", got, tt.want)
			}
		})
	}
}
