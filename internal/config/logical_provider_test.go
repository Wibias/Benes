package config

import (
	"encoding/json"
	"testing"
)

func TestFoldLogicalProvidersAbsorbsOpenAIAPIKeyConnection(t *testing.T) {
	disk := DiskConfig{
		Raw: []byte(`{"accountPoolStrategy":"quota","autoSwitchThreshold":80}`),
		Providers: map[string]json.RawMessage{
			"openai":         json.RawMessage(`{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward","codexAccountMode":"pool"}`),
			"openai-apikey":  json.RawMessage(`{"adapter":"openai-chat","baseUrl":"https://api.openai.com/v1","credentialRef":{"id":"k","source":"secure-store"}}`),
			"anthropic":      json.RawMessage(`{"adapter":"anthropic","baseUrl":"https://api.anthropic.com","authMode":"oauth"}`),
			"custom-forward": json.RawMessage(`{"adapter":"openai-responses","baseUrl":"https://example.com/v1","authMode":"forward"}`),
		},
	}
	got := FoldLogicalProviders(disk)
	if len(got) != 3 {
		t.Fatalf("len=%d got=%#v", len(got), got)
	}
	openai := got[0]
	if openai.ID != LogicalOpenAIID {
		t.Fatalf("first=%s", openai.ID)
	}
	if len(openai.ConnectionIDs) != 2 || openai.ConnectionIDs[1] != OpenAIAPIConnection {
		t.Fatalf("connections=%#v", openai.ConnectionIDs)
	}
	hidden := HiddenConnectionSet(got)
	if !hidden[OpenAIAPIConnection] {
		t.Fatal("openai-apikey was not hidden")
	}
	if len(openai.Access.Methods) != 2 {
		t.Fatalf("methods=%#v", openai.Access.Methods)
	}
	if openai.Access.Methods[0].Kind != AccessMethodOAuth || openai.Access.Methods[1].Kind != AccessMethodAPIKey {
		t.Fatalf("methods=%#v", openai.Access.Methods)
	}
	if openai.Access.DefaultMethodID != DefaultAccessOAuth || !openai.Access.DefaultAccess {
		t.Fatalf("default=%#v", openai.Access)
	}
	if openai.Access.Selection == nil || !openai.Access.Selection.ShowAutoSwitch || openai.Access.Selection.ShowStickyLimit {
		t.Fatalf("selection=%#v", openai.Access.Selection)
	}
	var ids []string
	for _, lp := range got {
		ids = append(ids, lp.ID)
	}
	if ids[1] != "anthropic" || ids[2] != "custom-forward" {
		t.Fatalf("ids=%v", ids)
	}
}

func TestFoldLogicalProvidersKeepsAPIOnlyOpenAIAsOwnRow(t *testing.T) {
	disk := DiskConfig{Providers: map[string]json.RawMessage{
		"openai-apikey": json.RawMessage(`{"adapter":"openai-chat","baseUrl":"https://api.openai.com/v1"}`),
	}}
	got := FoldLogicalProviders(disk)
	if len(got) != 1 || got[0].ID != OpenAIAPIConnection {
		t.Fatalf("got=%#v", got)
	}
	if HiddenConnectionSet(got)[OpenAIAPIConnection] {
		t.Fatal("solo openai-apikey must remain visible")
	}
}

func TestFoldLogicalProvidersExposesAPILaneWithoutDiskKeyProvider(t *testing.T) {
	disk := DiskConfig{Providers: map[string]json.RawMessage{
		"openai": json.RawMessage(`{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward","defaultAccess":"api"}`),
	}}
	got := FoldLogicalProviders(disk)
	if len(got) != 1 || got[0].Access.DefaultMethodID != DefaultAccessAPI {
		t.Fatalf("got=%#v", got)
	}
	if len(got[0].Access.Methods) != 2 || got[0].Access.Methods[1].ConnectionPresent {
		t.Fatalf("api method=%#v", got[0].Access.Methods)
	}
}

func TestFoldLogicalProvidersRoundRobinShowsStickyNotThreshold(t *testing.T) {
	disk := DiskConfig{
		Raw: []byte(`{"accountPoolStrategy":"round-robin","accountPoolStickyLimit":4}`),
		Providers: map[string]json.RawMessage{
			"openai": json.RawMessage(`{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward"}`),
		},
	}
	got := FoldLogicalProviders(disk)
	sel := got[0].Access.Selection
	if sel == nil || !sel.ShowStickyLimit || sel.ShowAutoSwitch || sel.StickyLimit == nil || *sel.StickyLimit != 4 {
		t.Fatalf("selection=%#v", sel)
	}
}

func TestFoldLogicalProvidersAccessMethodIDsShareDefaultNamespace(t *testing.T) {
	disk := DiskConfig{Providers: map[string]json.RawMessage{
		"openai":        json.RawMessage(`{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward","defaultAccess":"api"}`),
		"openai-apikey": json.RawMessage(`{"adapter":"openai-responses","baseUrl":"https://api.openai.com/v1","credentialRef":{"id":"k","source":"secure-store"}}`),
	}}
	got := FoldLogicalProviders(disk)
	if len(got) != 1 {
		t.Fatalf("got=%#v", got)
	}
	openai := got[0]
	methodIDs := map[string]bool{}
	for _, method := range openai.Access.Methods {
		methodIDs[method.ID] = true
	}
	if !methodIDs[DefaultAccessOAuth] || !methodIDs[DefaultAccessAPI] {
		t.Fatalf("method ids=%#v", openai.Access.Methods)
	}
	if !methodIDs[openai.Access.DefaultMethodID] {
		t.Fatalf("default method %q is not one of %#v", openai.Access.DefaultMethodID, openai.Access.Methods)
	}
	if openai.Access.Methods[1].Kind != AccessMethodAPIKey {
		t.Fatalf("api kind=%q", openai.Access.Methods[1].Kind)
	}
}

func TestFoldLogicalProvidersDirectModeIsNotReportedAsPool(t *testing.T) {
	disk := DiskConfig{Providers: map[string]json.RawMessage{
		"openai": json.RawMessage(`{"adapter":"openai-responses","baseUrl":"https://chatgpt.com/backend-api/codex","authMode":"forward","codexAccountMode":"direct"}`),
	}}
	got := FoldLogicalProviders(disk)
	if len(got) != 1 || got[0].Access.Selection == nil {
		t.Fatalf("got=%#v", got)
	}
	if got[0].Access.Selection.Mode != "direct" {
		t.Fatalf("mode=%q", got[0].Access.Selection.Mode)
	}
}
