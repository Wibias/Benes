package catalog

import "testing"

func TestRetainedUndiscoveredDoesNotInventReasoningEfforts(t *testing.T) {
	got, err := Project(ProjectionInput{
		ProviderID: "p",
		Policy: Policy{RetainModels: map[string]map[string]bool{"p": {"missing": true}}},
		Configured: []ConfiguredModel{{ID: "missing", ContextWindow: 32000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %#v", got)
	}
	if len(got[0].ReasoningEfforts) != 0 {
		t.Fatalf("invented reasoning efforts for retained-undiscovered row: %#v", got[0].ReasoningEfforts)
	}
}

func TestProjectionCapsNestedDiscoveredWindow(t *testing.T) {
	got, err := Project(ProjectionInput{
		ProviderID: "github-copilot",
		Policy: Policy{
			ProviderContextCaps: map[string]int{"github-copilot": 128000},
		},
		Discovered: []DiscoveredModel{{ID: "gpt-4.1", ContextWindow: 1048576, MaxInput: 128000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows=%#v", got)
	}
	if got[0].Context.Tokens != 128000 || got[0].Context.Source != ContextCap {
		t.Fatalf("cap did not win over Copilot advertised window: %#v", got[0].Context)
	}
	if got[0].AdvertisedTokens != 1048576 || got[0].StandardTokens != 128000 {
		t.Fatalf("advertised/standard lost: advertised=%d standard=%d", got[0].AdvertisedTokens, got[0].StandardTokens)
	}
	if got[0].MaxInput != 128000 {
		t.Fatalf("max input=%d", got[0].MaxInput)
	}
}

func TestProjectionCanDisableProviderContextCap(t *testing.T) {
	got, err := Project(ProjectionInput{
		ProviderID: "p",
		Policy: Policy{
			ProviderContextCaps:        map[string]int{"p": 64000},
			ProviderContextCapDisabled: map[string]bool{"p": true},
		},
		Discovered: []DiscoveredModel{{ID: "m", ContextWindow: 200000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Context.Tokens != 200000 || got[0].Context.Source != ContextDiscovered {
		t.Fatalf("disabled provider cap still narrowed row: %#v", got)
	}
}
