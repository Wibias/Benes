package server

import (
	"encoding/json"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/config"
)

func TestFoldLogicalOpenAICatalogUsesOnlyConfiguredDefaultLane(t *testing.T) {
	canonical := func(defaultAccess string, includeAPI bool) config.DiskConfig {
		providers := map[string]json.RawMessage{
			config.LogicalOpenAIID: json.RawMessage(`{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward","defaultAccess":"` + defaultAccess + `"}`),
		}
		if includeAPI {
			providers[config.OpenAIAPIConnection] = json.RawMessage(`{"adapter":"openai-responses","baseUrl":"https://api.openai.com/v1"}`)
		}
		return config.DiskConfig{Providers: providers}
	}
	models := []catalog.Model{
		{ID: "openai/oauth-only"},
		{ID: "openai/shared"},
		{ID: "openai-apikey/api-only"},
		{ID: "openai-apikey/shared"},
		{ID: "anthropic/claude"},
		{ID: "combo/mixed"},
	}

	t.Run("oauth default exposes only oauth models under logical identity", func(t *testing.T) {
		got := foldLogicalOpenAICatalog(canonical(config.DefaultAccessOAuth, true), models)
		assertOpenAICatalogIDs(t, got, []string{"openai/oauth-only", "openai/shared", "anthropic/claude", "combo/mixed"})
	})

	t.Run("api default exposes only api models under logical identity", func(t *testing.T) {
		got := foldLogicalOpenAICatalog(canonical(config.DefaultAccessAPI, true), models)
		assertOpenAICatalogIDs(t, got, []string{"openai/api-only", "openai/shared", "anthropic/claude", "combo/mixed"})
	})

	t.Run("api default with missing api connection exposes no logical openai models", func(t *testing.T) {
		got := foldLogicalOpenAICatalog(canonical(config.DefaultAccessAPI, false), []catalog.Model{{ID: "openai/oauth-only"}, {ID: "anthropic/claude"}})
		assertOpenAICatalogIDs(t, got, []string{"anthropic/claude"})
	})

	t.Run("standalone api provider remains physical", func(t *testing.T) {
		disk := config.DiskConfig{Providers: map[string]json.RawMessage{
			config.OpenAIAPIConnection: json.RawMessage(`{"adapter":"openai-responses","baseUrl":"https://api.openai.com/v1"}`),
		}}
		got := foldLogicalOpenAICatalog(disk, []catalog.Model{{ID: "openai-apikey/gpt-5.6"}})
		assertOpenAICatalogIDs(t, got, []string{"openai-apikey/gpt-5.6"})
	})
}

func assertOpenAICatalogIDs(t *testing.T, models []catalog.Model, want []string) {
	t.Helper()
	got := make([]string, 0, len(models))
	for _, model := range models {
		got = append(got, model.ID)
	}
	if len(got) != len(want) {
		t.Fatalf("ids=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids=%v want=%v", got, want)
		}
	}
}
