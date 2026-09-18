package config

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestMiniMaxInitSeedProjectsReasoningSplit(t *testing.T) {
	raw, err := os.ReadFile("../../cmd/benes/init_providers.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Providers []struct {
			ID   string          `json:"id"`
			Seed json.RawMessage `json:"seed"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, provider := range envelope.Providers {
		if provider.ID != "minimax" && provider.ID != "minimax-cn" {
			continue
		}
		found++
		var seed map[string]any
		if err := json.Unmarshal(provider.Seed, &seed); err != nil {
			t.Fatalf("%s seed: %v", provider.ID, err)
		}
		seed["apiKey"] = "sk-test"
		encoded, err := json.Marshal(seed)
		if err != nil {
			t.Fatal(err)
		}
		projection := ProjectProviderSpecs(DiskConfig{
			Providers: map[string]json.RawMessage{provider.ID: encoded},
		})
		if len(projection.Skipped) != 0 {
			t.Fatalf("%s skipped=%#v", provider.ID, projection.Skipped)
		}
		if len(projection.Specs) != 1 {
			t.Fatalf("%s specs=%#v", provider.ID, projection.Specs)
		}
		spec := projection.Specs[0]
		if !reflect.DeepEqual(spec.Chat.ReasoningSplitModels, []string{
			"MiniMax-M3",
			"MiniMax-M2.7",
			"MiniMax-M2.7-highspeed",
			"MiniMax-M2.5",
			"MiniMax-M2.5-highspeed",
			"MiniMax-M2.1",
			"MiniMax-M2.1-highspeed",
			"MiniMax-M2",
		}) {
			t.Fatalf("%s split=%#v", provider.ID, spec.Chat.ReasoningSplitModels)
		}
	}
	if found != 2 {
		t.Fatalf("expected minimax and minimax-cn seeds, found %d", found)
	}
}
