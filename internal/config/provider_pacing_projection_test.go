package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestProjectProviderSpecsRequestPacingUsesStricterRPMAndInterval(t *testing.T) {
	disk := DiskConfig{Providers: map[string]json.RawMessage{
		"openai-apikey": json.RawMessage(`{
			"adapter":"openai-responses",
			"baseUrl":"https://api.openai.com/v1",
			"authMode":"key",
			"apiKey":"sk-test",
			"requestPacing":{
				"enabled":true,
				"requestsPerMinute":60,
				"minIntervalMs":500,
				"models":{
					"gpt-5":{"requestsPerMinute":30},
					"gpt-5-mini":{"minIntervalMs":2000}
				}
			}
		}`),
	}}
	projection := ProjectProviderSpecs(disk)
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	spec := projection.Specs[0]
	if spec.RequestPacing != time.Second {
		t.Fatalf("base pacing=%s", spec.RequestPacing)
	}
	if got := spec.ModelRequestPacing["gpt-5"]; got != 2*time.Second {
		t.Fatalf("gpt-5 pacing=%s", got)
	}
	if got := spec.ModelRequestPacing["gpt-5-mini"]; got != 2*time.Second {
		t.Fatalf("gpt-5-mini pacing=%s", got)
	}
}

func TestProjectProviderSpecsRequestPacingObjectOverridesLegacyMirror(t *testing.T) {
	disk := DiskConfig{Providers: map[string]json.RawMessage{
		"openai-apikey": json.RawMessage(`{
			"adapter":"openai-responses",
			"baseUrl":"https://api.openai.com/v1",
			"authMode":"key",
			"apiKey":"sk-test",
			"requestPacingMs":250,
			"requestPacing":{"enabled":true,"requestsPerMinute":30}
		}`),
	}}
	projection := ProjectProviderSpecs(disk)
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if got := projection.Specs[0].RequestPacing; got != 2*time.Second {
		t.Fatalf("pacing=%s", got)
	}
}

func TestProjectProviderSpecsDisabledRequestPacingDisablesModelOverrides(t *testing.T) {
	disk := DiskConfig{Providers: map[string]json.RawMessage{
		"openai-apikey": json.RawMessage(`{
			"adapter":"openai-responses",
			"baseUrl":"https://api.openai.com/v1",
			"authMode":"key",
			"apiKey":"sk-test",
			"requestPacing":{"enabled":false,"minIntervalMs":1000,"models":{"gpt-5":{"minIntervalMs":5000}}}
		}`),
	}}
	projection := ProjectProviderSpecs(disk)
	if len(projection.Skipped) != 0 || len(projection.Specs) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	spec := projection.Specs[0]
	if spec.RequestPacing != 0 || len(spec.ModelRequestPacing) != 0 {
		t.Fatalf("base=%s models=%#v", spec.RequestPacing, spec.ModelRequestPacing)
	}
}
