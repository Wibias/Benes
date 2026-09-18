package config

import (
	"encoding/json"
	"testing"
)

func TestProjectProviderSpecsAcceptsResponsesMaxUpstreamBodyBytes(t *testing.T) {
	for _, raw := range []string{"1024", "0"} {
		t.Run(raw, func(t *testing.T) {
			projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
				"target": json.RawMessage(`{"adapter":"openai-responses","baseUrl":"https://example.com/v1","authMode":"key","apiKey":"test-key","maxUpstreamBodyBytes":` + raw + `}`),
			}})
			if len(projection.Skipped) != 0 {
				t.Fatalf("skipped=%#v", projection.Skipped)
			}
			if len(projection.Specs) != 1 || projection.Specs[0].ID != "target" {
				t.Fatalf("specs=%#v", projection.Specs)
			}
			want := int64(1024)
			if raw == "0" {
				want = 0
			}
			if projection.Specs[0].MaxUpstreamBodyBytes != want {
				t.Fatalf("maxUpstreamBodyBytes=%d want=%d", projection.Specs[0].MaxUpstreamBodyBytes, want)
			}
		})
	}
}

func TestProjectProviderSpecsRejectsInvalidResponsesMaxUpstreamBodyBytes(t *testing.T) {
	for _, raw := range []string{"-1", "1.5", "1e3", `"1024"`, "null", "true", "{}", "[]"} {
		t.Run(raw, func(t *testing.T) {
			projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
				"target": json.RawMessage(`{"adapter":"openai-responses","baseUrl":"https://example.com/v1","authMode":"key","apiKey":"test-key","maxUpstreamBodyBytes":` + raw + `}`),
			}})
			if len(projection.Specs) != 0 || len(projection.Skipped) != 1 {
				t.Fatalf("projection=%#v", projection)
			}
			if projection.Skipped[0].Code != "invalid_field" || projection.Skipped[0].Field != "maxUpstreamBodyBytes" {
				t.Fatalf("skip=%#v", projection.Skipped[0])
			}
		})
	}
}

func TestProjectProviderSpecsRejectsBodyLimitForNonResponsesAdapter(t *testing.T) {
	projection := ProjectProviderSpecs(DiskConfig{Providers: map[string]json.RawMessage{
		"target": json.RawMessage(`{"adapter":"openai-chat","baseUrl":"https://example.com/v1","authMode":"key","apiKey":"test-key","maxUpstreamBodyBytes":1024}`),
	}})
	if len(projection.Specs) != 0 || len(projection.Skipped) != 1 {
		t.Fatalf("projection=%#v", projection)
	}
	if projection.Skipped[0].Code != "unsupported_field" || projection.Skipped[0].Field != "maxUpstreamBodyBytes" {
		t.Fatalf("skip=%#v", projection.Skipped[0])
	}
}
