package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/combo"
	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/provideractivity"
	"github.com/Wibias/Benes/internal/router"
)

func TestApplyLogicalOpenAIAccessHonorsDefaultAPIWhenBothLanesCanServe(t *testing.T) {
	configPath := writeLogicalOpenAIConfig(t, "api")
	h := &handler{
		providers: map[string]Provider{
			config.LogicalOpenAIID:     providerFunc(nil),
			config.OpenAIAPIConnection: providerFunc(nil),
		},
		catalogModels: []catalog.Model{
			{ID: config.LogicalOpenAIID + "/gpt-5"},
			{ID: config.OpenAIAPIConnection + "/gpt-5"},
		},
		configPath: configPath,
	}

	got := h.applyLogicalOpenAIAccess(router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5"})
	if got.Provider != config.OpenAIAPIConnection {
		t.Fatalf("provider=%q", got.Provider)
	}
}

func TestApplyLogicalOpenAIAccessHonorsDefaultOAuthWhenBothLanesCanServe(t *testing.T) {
	configPath := writeLogicalOpenAIConfig(t, "oauth")
	h := &handler{
		providers: map[string]Provider{
			config.LogicalOpenAIID:     providerFunc(nil),
			config.OpenAIAPIConnection: providerFunc(nil),
		},
		catalogModels: []catalog.Model{
			{ID: config.LogicalOpenAIID + "/gpt-5"},
			{ID: config.OpenAIAPIConnection + "/gpt-5"},
		},
		configPath: configPath,
	}

	got := h.applyLogicalOpenAIAccess(router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5"})
	if got.Provider != config.LogicalOpenAIID {
		t.Fatalf("provider=%q", got.Provider)
	}
}

func TestApplyLogicalOpenAIAccessDoesNotFallbackFromSelectedLane(t *testing.T) {
	configPath := writeLogicalOpenAIConfig(t, "api")
	h := &handler{
		providers: map[string]Provider{
			config.LogicalOpenAIID:     providerFunc(nil),
			config.OpenAIAPIConnection: providerFunc(nil),
		},
		catalogModels: []catalog.Model{{ID: config.LogicalOpenAIID + "/gpt-5"}},
		configPath:    configPath,
	}
	if got := h.applyLogicalOpenAIAccess(router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5"}); got.Provider != config.OpenAIAPIConnection {
		t.Fatalf("selected provider=%q", got.Provider)
	}

	h.catalogModels = []catalog.Model{{ID: config.OpenAIAPIConnection + "/gpt-4o"}}
	if got := h.applyLogicalOpenAIAccess(router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-4o"}); got.Provider != config.OpenAIAPIConnection {
		t.Fatalf("api provider=%q", got.Provider)
	}
}

func TestApplyLogicalOpenAIAccessKeepsExplicitAccountRouteOnOAuth(t *testing.T) {
	configPath := writeLogicalOpenAIConfig(t, "api")
	h := &handler{
		providers: map[string]Provider{
			config.LogicalOpenAIID:     providerFunc(nil),
			config.OpenAIAPIConnection: providerFunc(nil),
		},
		catalogModels: []catalog.Model{
			{ID: config.LogicalOpenAIID + "/gpt-5"},
			{ID: config.OpenAIAPIConnection + "/gpt-5"},
		},
		configPath: configPath,
	}

	got := h.applyLogicalOpenAIAccess(router.Route{
		Provider:       config.LogicalOpenAIID,
		Model:          "gpt-5",
		CodexAccountID: "account-a",
	})
	if got.Provider != config.LogicalOpenAIID {
		t.Fatalf("provider=%q", got.Provider)
	}
}

func TestComboLogicalOpenAITargetUsesSameAccessLaneResolution(t *testing.T) {
	configPath := writeLogicalOpenAIConfig(t, "api")
	h := &handler{
		providers: map[string]Provider{
			config.LogicalOpenAIID:     providerFunc(nil),
			config.OpenAIAPIConnection: providerFunc(nil),
		},
		catalogModels: []catalog.Model{
			{ID: config.LogicalOpenAIID + "/gpt-5"},
			{ID: config.OpenAIAPIConnection + "/gpt-5"},
		},
		configPath: configPath,
		combos: map[string]Combo{
			"both": {ID: "both", Targets: []ComboTarget{{ProviderID: config.LogicalOpenAIID, Model: "gpt-5", Protocol: "openai-responses"}}},
		},
	}

	resolved := h.resolveProvider(router.Route{Provider: comboNamespace, Model: "both"})
	attributed, ok := resolved.Provider.(attributedProvider)
	if !ok {
		t.Fatalf("provider=%T", resolved.Provider)
	}
	walker, ok := attributed.Provider.(*combo.Walker)
	if !ok || len(walker.Targets) != 1 {
		t.Fatalf("provider=%T targets=%#v", attributed.Provider, walker)
	}
	if got := walker.Targets[0].Member.ID; got != config.OpenAIAPIConnection+"/gpt-5" {
		t.Fatalf("member=%q", got)
	}
}

func TestDefaultAccessCacheInvalidatesAfterManagementChange(t *testing.T) {
	configPath := writeLogicalOpenAIConfig(t, "oauth")
	h := &handler{
		providers: map[string]Provider{
			config.LogicalOpenAIID:     providerFunc(nil),
			config.OpenAIAPIConnection: providerFunc(nil),
		},
		catalogModels: []catalog.Model{
			{ID: config.LogicalOpenAIID + "/gpt-5"},
			{ID: config.OpenAIAPIConnection + "/gpt-5"},
		},
		configPath: configPath,
		activity:   provideractivity.New(),
	}
	if got := h.applyLogicalOpenAIAccess(router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5"}); got.Provider != config.LogicalOpenAIID {
		t.Fatalf("initial provider=%q", got.Provider)
	}

	if err := os.WriteFile(configPath, []byte(logicalOpenAIConfigJSON("api")), 0o600); err != nil {
		t.Fatal(err)
	}
	h.recordProviderActivity(config.LogicalOpenAIID, "default_access_changed", "", "info")
	if got := h.applyLogicalOpenAIAccess(router.Route{Provider: config.LogicalOpenAIID, Model: "gpt-5"}); got.Provider != config.OpenAIAPIConnection {
		t.Fatalf("updated provider=%q", got.Provider)
	}
}

func writeLogicalOpenAIConfig(t *testing.T, defaultAccess string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(logicalOpenAIConfigJSON(defaultAccess)), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func logicalOpenAIConfigJSON(defaultAccess string) string {
	return `{
		"providers": {
			"openai": {
				"adapter": "openai-responses",
				"baseUrl": "https://chatgpt.com/backend-api/codex",
				"authMode": "forward",
				"defaultAccess": "` + defaultAccess + `"
			},
			"openai-apikey": {
				"adapter": "openai-responses",
				"baseUrl": "https://api.openai.com/v1",
				"authMode": "key"
			}
		}
	}`
}
