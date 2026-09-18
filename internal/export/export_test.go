package export

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildOpencodeOmitsSecretsAndIncludesEnvRef(t *testing.T) {
	result, err := Build("opencode", Context{
		BaseURL:  "http://127.0.0.1:18080/v1",
		Hostname: "127.0.0.1",
		Models:   []Model{{Namespaced: "openai-apikey/gpt-5.5", Provider: "openai-apikey", ID: "gpt-5.5", ContextWindow: 128000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != FormatJSON || !strings.Contains(result.Text, OpenCodeAPIKeyEnv) || strings.Contains(result.Text, "sk-") {
		t.Fatalf("text=%s", result.Text)
	}
	if result.ModelCount != 1 || result.ModelsWithoutLimits != 0 {
		t.Fatalf("count=%d missing=%d", result.ModelCount, result.ModelsWithoutLimits)
	}
}

func TestBuildPiHonorsCodingAgentDir(t *testing.T) {
	elsewhere := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", elsewhere)
	result, err := Build("pi", Context{
		BaseURL:  "http://127.0.0.1:18080/v1",
		Hostname: "127.0.0.1",
		Models:   []Model{{Namespaced: "xai/grok", Provider: "xai", ID: "grok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(elsewhere, "models.json")
	if result.Destination != want {
		t.Fatalf("destination=%q want=%q", result.Destination, want)
	}
}

func TestBuildPiRejectsRelativeCodingAgentDir(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", "pi-elsewhere")
	_, err := Build("pi", Context{
		BaseURL:  "http://127.0.0.1:18080/v1",
		Hostname: "127.0.0.1",
		Models:   []Model{{Namespaced: "xai/grok", Provider: "xai", ID: "grok"}},
	})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildPiIsLoopbackOnly(t *testing.T) {
	_, err := Build("pi", Context{BaseURL: "http://example.com:18080/v1", Hostname: "example.com"})
	if err == nil || !strings.Contains(err.Error(), "loopback-only") {
		t.Fatalf("err=%v", err)
	}
	result, err := Build("pi", Context{
		BaseURL:  "http://127.0.0.1:18080/v1",
		Hostname: "127.0.0.1",
		Models:   []Model{{Namespaced: "xai/grok", Provider: "xai", ID: "grok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, loopbackKey) || strings.Contains(result.Text, "sk-") {
		t.Fatalf("text=%s", result.Text)
	}
}

func TestBuildUnknownClient(t *testing.T) {
	if _, err := Build("nope", Context{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildPrimeHonorsCodingAgentDir(t *testing.T) {
	elsewhere := t.TempDir()
	t.Setenv("PRIME_AGENT_CODING_AGENT_DIR", elsewhere)
	result, err := Build("prime", Context{
		BaseURL:  "http://127.0.0.1:18080/v1",
		Hostname: "127.0.0.1",
		Models:   []Model{{Namespaced: "xai/grok", Provider: "xai", ID: "grok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(elsewhere, "models.json")
	if result.Destination != want {
		t.Fatalf("destination=%q want=%q", result.Destination, want)
	}
}

func TestBuildPrimeRejectsRelativeCodingAgentDir(t *testing.T) {
	t.Setenv("PRIME_AGENT_CODING_AGENT_DIR", "prime-elsewhere")
	_, err := Build("prime", Context{
		BaseURL:  "http://127.0.0.1:18080/v1",
		Hostname: "127.0.0.1",
		Models:   []Model{{Namespaced: "xai/grok", Provider: "xai", ID: "grok"}},
	})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildPrimeReusesPiSerializerWithoutSharingPath(t *testing.T) {
	ctx := Context{
		BaseURL:  "http://127.0.0.1:18080/v1",
		Hostname: "127.0.0.1",
		Models:   []Model{{Namespaced: "xai/grok", Provider: "xai", ID: "grok"}},
	}
	pi, err := Build("pi", ctx)
	if err != nil {
		t.Fatal(err)
	}
	prime, err := Build("prime", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if prime.Client != "prime" || prime.Format != pi.Format || prime.Text != pi.Text {
		t.Fatalf("prime=%+v pi format=%s text=%s", prime, pi.Format, pi.Text)
	}
	if prime.Destination == pi.Destination {
		t.Fatalf("shared destination %q", prime.Destination)
	}
}

func TestContributePrimeOwnsProvidersBenes(t *testing.T) {
	contrib, err := Contribute("prime", Context{
		BaseURL:  "http://127.0.0.1:18080/v1",
		Hostname: "127.0.0.1",
		Models:   []Model{{Namespaced: "xai/grok", Provider: "xai", ID: "grok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if contrib.Client != "prime" {
		t.Fatalf("client=%q", contrib.Client)
	}
	if len(contrib.Fragments) != 1 {
		t.Fatalf("fragments=%d", len(contrib.Fragments))
	}
	path := strings.Join(contrib.Fragments[0].Path, ".")
	if path != "providers."+ProviderID {
		t.Fatalf("path=%q", path)
	}
}

func TestBuildAllClients(t *testing.T) {
	ctx := Context{
		BaseURL:  "http://127.0.0.1:18080/v1",
		Hostname: "127.0.0.1",
		Models: []Model{{
			Namespaced: "openai-apikey/gpt-5.5", Provider: "openai-apikey", ID: "gpt-5.5",
			ContextWindow: 64000, InputModalities: []string{"text"}, ReasoningEfforts: []string{"low", "high"},
		}},
	}
	for _, id := range ClientIDs {
		result, err := Build(id, ctx)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if result.Text == "" || strings.Contains(result.Text, "sk-") || strings.Contains(strings.ToLower(result.Text), "secret") {
			t.Fatalf("%s text=%s", id, result.Text)
		}
	}
}
