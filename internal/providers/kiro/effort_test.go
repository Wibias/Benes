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

func TestApplyNativeEffortForGPT56LunaAndTerra(t *testing.T) {
	for _, model := range []string{"gpt-5.6-luna", "gpt-5.6-terra"} {
		t.Run(model, func(t *testing.T) {
			payload := map[string]any{}
			if err := ApplyNativeEffort(payload, model, "high"); err != nil {
				t.Fatal(err)
			}
			fields, ok := payload["additionalModelRequestFields"].(map[string]any)
			if !ok {
				t.Fatalf("missing native request fields: %#v", payload)
			}
			reasoning, ok := fields["reasoning"].(map[string]any)
			if !ok || reasoning["effort"] != "high" {
				t.Fatalf("native reasoning=%#v payload=%#v", reasoning, payload)
			}
		})
	}
}

