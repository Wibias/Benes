package config

import (
	"encoding/json"
	"testing"
)

func TestProjectCombosProjectsFailoverTargetsFromDisk(t *testing.T) {
	disk := DiskConfig{
		Providers: map[string]json.RawMessage{
			"google": json.RawMessage(`{"adapter":"google"}`),
			"local":  json.RawMessage(`{"adapter":"openai-responses"}`),
		},
		Raw: json.RawMessage(`{"combos":{"fast":{"strategy":"failover","targets":[{"provider":"google","model":"gemini-flash"},{"provider":"local","model":"gpt-5.4"}]}}}`),
	}
	got := ProjectCombos(disk)
	if len(got.Combos) != 1 || len(got.Skipped) != 0 {
		t.Fatalf("got=%#v", got)
	}
	combo := got.Combos[0]
	if combo.ID != "fast" || combo.Strategy != "failover" || len(combo.Targets) != 2 {
		t.Fatalf("combo=%#v", combo)
	}
	if combo.Targets[0].ProviderID != "google" || combo.Targets[0].Model != "gemini-flash" {
		t.Fatalf("first=%#v", combo.Targets[0])
	}
	if combo.Targets[1].ProviderID != "local" || combo.Targets[1].Model != "gpt-5.4" {
		t.Fatalf("second=%#v", combo.Targets[1])
	}
}

func TestProjectCombosDefaultsMissingStrategyToFailover(t *testing.T) {
	disk := DiskConfig{
		Providers: map[string]json.RawMessage{"local": json.RawMessage(`{}`)},
		Raw:       json.RawMessage(`{"combos":{"plain":{"targets":[{"provider":"local","model":"gpt"}]}}}`),
	}
	got := ProjectCombos(disk)
	if len(got.Combos) != 1 || got.Combos[0].Strategy != "failover" {
		t.Fatalf("got=%#v", got)
	}
}

func TestProjectCombosTreatsRoundRobinAsFailover(t *testing.T) {
	disk := DiskConfig{
		Providers: map[string]json.RawMessage{"local": json.RawMessage(`{}`)},
		Raw:      json.RawMessage(`{"combos":{"rr":{"strategy":"round-robin","targets":[{"provider":"local","model":"gpt"}]}}}`),
	}
	got := ProjectCombos(disk)
	if len(got.Combos) != 1 || len(got.Skipped) != 0 {
		t.Fatalf("got=%#v", got)
	}
	if got.Combos[0].ID != "rr" || got.Combos[0].Strategy != "failover" {
		t.Fatalf("combo=%#v", got.Combos[0])
	}
}

func TestProjectCombosSkipsUnknownStrategyAndUnknownProviders(t *testing.T) {
	disk := DiskConfig{
		Providers: map[string]json.RawMessage{"local": json.RawMessage(`{}`)},
		Raw: json.RawMessage(`{"combos":{
			"weighted":{"strategy":"weighted","targets":[{"provider":"local","model":"gpt"}]},
			"ghost":{"targets":[{"provider":"missing","model":"gpt"}]},
			"empty":{"targets":[]}
		}}`),
	}
	got := ProjectCombos(disk)
	if len(got.Combos) != 0 || len(got.Skipped) != 3 {
		t.Fatalf("got=%#v", got)
	}
}

func TestProjectCombosOmitsBlankRoot(t *testing.T) {
	got := ProjectCombos(DiskConfig{Raw: json.RawMessage(`{"providers":{}}`)})
	if len(got.Combos) != 0 || len(got.Skipped) != 0 {
		t.Fatalf("got=%#v", got)
	}
}
