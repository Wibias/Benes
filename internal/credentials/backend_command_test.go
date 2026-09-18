package credentials

import (
	"errors"
	"strings"
	"testing"
)

func TestDarwinBackendUsesSecurityAndFailsClosedWhenMissing(t *testing.T) {
	backend := darwinBackend()
	backend.look = func(string) (string, error) { return "", errors.New("missing") }
	if !errors.Is(backend.Available(), ErrSecureStoreUnavailable) {
		t.Fatal("missing security")
	}
	var got commandSpec
	backend.look = func(string) (string, error) { return "/usr/bin/security", nil }
	backend.run = func(name string, args []string, stdin string) (string, error) {
		got = commandSpec{Name: name, Args: args, Stdin: stdin}
		return "", nil
	}
	if err := backend.Put("slot-a", []byte("sk-mac")); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got.Args, " ")
	if got.Name != "security" || !strings.Contains(joined, "add-generic-password") || !strings.Contains(joined, "Benes/slot-a") {
		t.Fatalf("put=%#v", got)
	}
	if strings.Contains(joined, "sk-mac") == false {
		t.Fatal("password must be passed as -w argument, not omitted")
	}
}

func TestLinuxBackendUsesSecretToolAndDoesNotLeakOnLookupFailure(t *testing.T) {
	backend := linuxBackend()
	backend.look = func(string) (string, error) { return "/usr/bin/secret-tool", nil }
	backend.run = func(name string, args []string, stdin string) (string, error) {
		if args[0] == "lookup" {
			return "", errors.New("secret-tool: no such item")
		}
		return "", nil
	}
	_, err := backend.Get("slot-a")
	if !errors.Is(err, ErrCredentialUnavailable) || strings.Contains(err.Error(), "secret-tool") {
		t.Fatalf("err=%v", err)
	}
	backend.run = func(name string, args []string, stdin string) (string, error) {
		if args[0] != "store" || stdin != "sk-linux" {
			t.Fatalf("store name=%s args=%v stdin=%q", name, args, stdin)
		}
		return "", nil
	}
	if err := backend.Put("slot-a", []byte("sk-linux")); err != nil {
		t.Fatal(err)
	}
}
