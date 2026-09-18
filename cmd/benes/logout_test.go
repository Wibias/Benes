package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/config"
	"github.com/Wibias/Benes/internal/credentials"
)

func TestRunLogoutDeletesSecureStoreItem(t *testing.T) {
	home := t.TempDir()
	store, err := credentials.NewFileStore(filepath.Join(home, "credentials"), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put("openai-apikey", []byte("sk-live")); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runLogout([]string{"openai-apikey"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "openai-apikey") {
		t.Fatalf("stdout=%q", stdout.String())
	}
	if _, err := store.Get(credentials.Ref{ID: "openai-apikey", Source: credentials.SourceSecureStore}); err == nil {
		t.Fatal("secret remained after logout")
	}
}

func TestRunLogoutRequiresCredentialID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runLogout(nil, &stdout, &stderr, defaultCommandDependencies()); code != 2 {
		t.Fatalf("code=%d", code)
	}
}

func TestRunLogoutResolvesProviderCredentialRef(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"local":{"credentialRef":{"id":"local-key","source":"secure-store"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := credentials.NewFileStore(filepath.Join(home, "credentials"), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put("local-key", []byte("sk-live")); err != nil {
		t.Fatal(err)
	}
	deps := defaultCommandDependencies()
	deps.resolvePaths = func(config.PathOptions) (config.Paths, error) {
		return config.Paths{Home: home, Config: configPath}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runLogout([]string{"local"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if _, err := store.Get(credentials.Ref{ID: "local-key", Source: credentials.SourceSecureStore}); err == nil {
		t.Fatal("provider credential remained after logout")
	}
}
