package export

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildSupportedExportsStampHarnessIdentity(t *testing.T) {
	t.Parallel()

	ctx := Context{
		BaseURL:  "http://127.0.0.1:18080/v1",
		Hostname: "127.0.0.1",
		Models: []Model{{
			Namespaced:      "openai-apikey/gpt-5.5",
			Provider:        "openai-apikey",
			ID:              "gpt-5.5",
			ContextWindow:   128000,
			InputModalities: []string{"text", "image"},
		}},
	}

	for _, id := range []string{"opencode", "hermes", "openclaw", "dsh"} {
		id := id
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			result, err := Build(id, ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.Text, "X-Benes-Harness") || !strings.Contains(result.Text, id) {
				t.Fatalf("%s export does not stamp harness identity:\n%s", id, result.Text)
			}
		})
	}
}

func TestContributeSupportedManagedIntegrationsStampHarnessIdentity(t *testing.T) {
	t.Parallel()

	ctx := Context{
		BaseURL:  "http://127.0.0.1:18080/v1",
		Hostname: "127.0.0.1",
		Models: []Model{{
			Namespaced:      "openai-apikey/gpt-5.5",
			Provider:        "openai-apikey",
			ID:              "gpt-5.5",
			ContextWindow:   128000,
			InputModalities: []string{"text", "image"},
		}},
	}

	for _, id := range []string{"opencode", "hermes", "openclaw", "dsh"} {
		id := id
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			contribution, err := Contribute(id, ctx)
			if err != nil {
				t.Fatal(err)
			}
			got := fmt.Sprint(contribution.Fragments)
			if !strings.Contains(got, "X-Benes-Harness") || !strings.Contains(got, id) {
				t.Fatalf("%s managed contribution does not stamp harness identity: %s", id, got)
			}
		})
	}
}

func TestRemoteSupportedExportsKeepAuthHeadersWithHarnessIdentity(t *testing.T) {
	t.Parallel()

	ctx := Context{
		BaseURL:  "https://relay.example/v1",
		Hostname: "relay.example",
		Models: []Model{{
			Namespaced:    "openai-apikey/gpt-5.5",
			Provider:      "openai-apikey",
			ID:            "gpt-5.5",
			ContextWindow: 128000,
		}},
	}

	tests := []struct {
		id      string
		authRef string
	}{
		{id: "opencode", authRef: OpenCodeAPIKeyEnv},
		{id: "hermes", authRef: HermesAPIKeyEnv},
		{id: "openclaw", authRef: OpenclawAPIKeyEnv},
	}
	for _, test := range tests {
		test := test
		t.Run(test.id, func(t *testing.T) {
			t.Parallel()
			result, err := Build(test.id, ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.Text, "X-Benes-Harness") || !strings.Contains(result.Text, test.id) {
				t.Fatalf("%s remote export missing identity:\n%s", test.id, result.Text)
			}
			if !strings.Contains(result.Text, test.authRef) {
				t.Fatalf("%s remote export lost auth header/env reference:\n%s", test.id, result.Text)
			}
		})
	}
}
