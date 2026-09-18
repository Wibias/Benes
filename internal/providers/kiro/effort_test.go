package kiro

import "testing"

func TestApplyNativeEffortOnlyForKnownFamilies(t *testing.T) {
	payload := map[string]any{}
	if err := ApplyNativeEffort(payload, "gpt-5.6-sol", "high"); err != nil {
		t.Fatal(err)
	}
	fields := payload["additionalModelRequestFields"].(map[string]any)
	if fields["reasoning"].(map[string]any)["effort"] != "high" {
		t.Fatalf("%#v", payload)
	}
	if err := ApplyNativeEffort(map[string]any{}, "mystery", "high"); err != nil {
		t.Fatal(err)
	}
	if err := ApplyNativeEffort(map[string]any{}, "gpt-5.6-sol", "nope"); err == nil {
		t.Fatal("unsupported effort")
	}
}
