package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDiskConfigCodexAccountNamespaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
		"providers":{"openai":{"adapter":"openai-responses","authMode":"forward","codexAccountMode":"direct"}},
		"codexAccounts":[{"id":"acct-a"},{"id":"acct-b"}],
		"codexAccountNamespaces":{"side":"acct-b","main":"@main"}
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := LoadDiskConfig(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.CodexAccountNamespaces) != 2 || config.CodexAccountNamespaces["side"] != "acct-b" || config.CodexAccountNamespaces["main"] != "@main" {
		t.Fatalf("namespaces=%#v", config.CodexAccountNamespaces)
	}
}

func TestLoadDiskConfigRejectsUnsafeCodexAccountNamespaces(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "not object", body: `{"providers":{"openai":{"adapter":"openai-responses"}},"codexAccountNamespaces":[]}`},
		{name: "provider collision case insensitive", body: `{"providers":{"openai":{"adapter":"openai-responses"}},"codexAccountNamespaces":{"OPENAI":"acct-a"}}`},
		{name: "private account id collision", body: `{"providers":{"openai":{"adapter":"openai-responses"}},"codexAccounts":[{"id":"acct-a"}],"codexAccountNamespaces":{"acct-a":"acct-a"}}`},
		{name: "slash namespace", body: `{"providers":{"openai":{"adapter":"openai-responses"}},"codexAccountNamespaces":{"bad/name":"acct-a"}}`},
		{name: "whitespace namespace", body: `{"providers":{"openai":{"adapter":"openai-responses"}},"codexAccountNamespaces":{" side":"acct-a"}}`},
		{name: "invalid target", body: `{"providers":{"openai":{"adapter":"openai-responses"}},"codexAccountNamespaces":{"side":"@other"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadDiskConfig(path, 1<<20); err == nil {
				t.Fatal("expected namespace validation error")
			}
		})
	}
}
