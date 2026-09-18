package grok

import "testing"

// The registration summary is the only thing the dashboard and the CLI report about the
// projection, so every state it can be in has to be pinned here.
func TestRegistrationForDescribesTheProjection(t *testing.T) {
	base := "http://127.0.0.1:23100/v1"
	catalogue := []Model{{ID: "openai/gpt-5.6-sol"}, {ID: "xai/grok-4.6"}}
	written := Status{
		ConfigPath: "/tmp/config.toml",
		Present:    true,
		BaseURL:    base,
		Models:     []StatusModel{{Alias: "benes-openai-gpt-5-6-sol", ID: "openai/gpt-5.6-sol"}, {Alias: "benes-xai-grok-4-6", ID: "xai/grok-4.6"}},
	}

	absent := RegistrationFor(catalogue, Status{ConfigPath: "/tmp/config.toml"}, base)
	if absent.Present || absent.Current || absent.Registered != 0 || absent.Catalogue != 2 {
		t.Fatalf("absent: %#v", absent)
	}

	full := RegistrationFor(catalogue, written, base)
	if !full.Present || !full.Current || full.Registered != 2 || full.Catalogue != 2 {
		t.Fatalf("current: %#v", full)
	}

	// The catalogue gained a model: every registered id is still known, and it is still stale.
	grown := RegistrationFor(append(catalogue, Model{ID: "anthropic/claude-opus-4-6"}), written, base)
	if grown.Current || grown.Registered != 2 || grown.Catalogue != 3 {
		t.Fatalf("grown: %#v", grown)
	}

	// The block carries an id the catalogue no longer offers.
	retired := written
	retired.Models = []StatusModel{{ID: "openai/gpt-5.6-sol"}, {ID: "retired/model"}}
	if got := RegistrationFor(catalogue, retired, base); got.Current || got.Registered != 2 {
		t.Fatalf("retired: %#v", got)
	}

	// Matching models, wrong address: Grok would call somewhere else.
	elsewhere := written
	elsewhere.BaseURL = "http://127.0.0.1:9/v1"
	if got := RegistrationFor(catalogue, elsewhere, base); got.Current {
		t.Fatalf("elsewhere: %#v", got)
	}
	// With no listener to compare against, the model set is all there is to judge.
	if got := RegistrationFor(catalogue, elsewhere, ""); !got.Current {
		t.Fatalf("no expected base: %#v", got)
	}

	// Blank and repeated ids are not models: they must not inflate either side of the count.
	noisy := []Model{{ID: "openai/gpt-5.6-sol"}, {ID: "  "}, {ID: "openai/gpt-5.6-sol"}}
	duplicated := written
	duplicated.Models = []StatusModel{{ID: "openai/gpt-5.6-sol"}, {ID: "openai/gpt-5.6-sol"}}
	if got := RegistrationFor(noisy, duplicated, base); !got.Current || got.Catalogue != 1 || got.Registered != 1 {
		t.Fatalf("noisy: %#v", got)
	}
}

