package sidecar

import "testing"

func TestResolveDedicatedSearchAndRejectLookalike(t *testing.T) {
	proven := Candidate{
		Modality: ModalityWebSearch, Class: ClassDedicatedSearch, ProviderID: "exa",
		Destination: "https://api.exa.ai", ModelID: "exa-search", AuthClass: "key",
		Wire: "search-json", Citations: true, Proven: true,
	}
	got, err := Resolve(Selection{Modality: ModalityWebSearch, Backend: "dedicated_search", Candidates: []Candidate{proven}})
	if err != nil || got.ProviderID != "exa" {
		t.Fatalf("dedicated=%v %#v", err, got)
	}
	lookalike := proven
	lookalike.Proven = false
	lookalike.Destination = "https://exa.example.invalid"
	if _, err := Resolve(Selection{Modality: ModalityWebSearch, Backend: "openai", Candidates: []Candidate{lookalike}}); err != ErrUnprovenCapability && err != ErrUnsupportedBackend {
		t.Fatalf("lookalike=%v", err)
	}
}

func TestResolveProviderNativeSearchWithCitations(t *testing.T) {
	native := Candidate{
		Modality: ModalityWebSearch, Class: ClassProviderNativeSearch, ProviderID: "anthropic",
		Destination: "https://api.anthropic.com", ModelID: "claude-sonnet-4-5", AuthClass: "key",
		Wire: "anthropic-messages", Citations: true, Proven: true,
	}
	got, err := Resolve(Selection{Modality: ModalityWebSearch, Backend: "anthropic", ProviderID: "anthropic", ModelID: "claude-sonnet-4-5", Candidates: []Candidate{native}})
	if err != nil || !got.Citations || got.Class != ClassProviderNativeSearch {
		t.Fatalf("native=%v %#v", err, got)
	}
}

func TestResolveAmbiguousBareModelFailsClosed(t *testing.T) {
	a := Candidate{Modality: ModalityVisionDescribe, Class: ClassVisionDescribe, ProviderID: "openai-apikey", ModelID: "gpt-4o", ImageInput: true, Proven: true}
	b := a
	b.ProviderID = "azure-openai"
	_, err := Resolve(Selection{Modality: ModalityVisionDescribe, ModelID: "gpt-4o", Candidates: []Candidate{a, b}})
	if err != ErrAmbiguousModel {
		t.Fatalf("ambiguous=%v", err)
	}
	got, err := Resolve(Selection{Modality: ModalityVisionDescribe, ModelID: "openai-apikey/gpt-4o", Candidates: []Candidate{a, b}})
	if err != nil || got.ProviderID != "openai-apikey" {
		t.Fatalf("qualified=%v %#v", err, got)
	}
}

func TestCatalogOmitsUnknownCapability(t *testing.T) {
	unknown := Candidate{Modality: ModalityVisionDescribe, Class: ClassVisionDescribe, ProviderID: "custom", ModelID: "looks-like-vision", Proven: false}
	proven := Candidate{Modality: ModalityVisionDescribe, Class: ClassVisionDescribe, ProviderID: "openai-apikey", ModelID: "gpt-4o", ImageInput: true, Proven: true}
	got := Catalog([]Candidate{unknown, proven}, ModalityVisionDescribe)
	if len(got) != 1 || got[0].ModelID != "gpt-4o" {
		t.Fatalf("%#v", got)
	}
}

func TestLegacyBrandDoesNotGrantUnrelatedClass(t *testing.T) {
	got, err := NormalizeBackend("openai", ModalityVisionDescribe)
	if err != nil || got != ClassVisionDescribe {
		t.Fatalf("openai vision alias=%v %q", err, got)
	}
	if _, err := NormalizeBackend("gemini", ModalityWebSearch); err != ErrUnsupportedBackend {
		t.Fatalf("gemini brand=%v", err)
	}
	if _, err := NormalizeBackend("dedicated_search", ModalityVisionDescribe); err != ErrUnsupportedBackend {
		t.Fatalf("search class on vision=%v", err)
	}
}

func TestBoundSourcesAndText(t *testing.T) {
	sources := []map[string]string{{"url": "a"}, {"url": "b"}, {"url": "c"}}
	if got := BoundSources(sources, 2); len(got) != 2 || got[1]["url"] != "b" {
		t.Fatalf("%#v", got)
	}
	if got := BoundText("abcdef", 3); got != "abc" {
		t.Fatalf("%q", got)
	}
}
