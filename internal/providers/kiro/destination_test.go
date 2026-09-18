package kiro

import (
	"errors"
	"testing"
)

func TestResolveDestinationRequiresHTTPSRuntimeHost(t *testing.T) {
	got, err := ResolveDestination("", "US-EAST-1")
	if err != nil || got != "https://runtime.us-east-1.kiro.dev" {
		t.Fatalf("default=%q err=%v", got, err)
	}
	if _, err := ResolveDestination("http://runtime.us-east-1.kiro.dev", ""); !errors.Is(err, ErrInvalidDestination) {
		t.Fatalf("cleartext=%v", err)
	}
	if _, err := ResolveDestination("https://evil.example", ""); !errors.Is(err, ErrInvalidDestination) {
		t.Fatalf("foreign=%v", err)
	}
}

func TestResolveDestinationAllowlistsRegionBeforeHostname(t *testing.T) {
	for _, region := range []string{"evil", "us-east-1/admin", "not_a_region"} {
		got, err := ResolveDestination("", region)
		if err == nil {
			t.Fatalf("region %q accepted as %q", region, got)
		}
	}
	if got, err := ResolveDestination("https://runtime.evil.kiro.dev", ""); err == nil {
		t.Fatalf("explicit evil host accepted as %q", got)
	}
	if got, err := ResolveDestination("", ""); err != nil || got != "https://runtime.us-east-1.kiro.dev" {
		t.Fatalf("empty fallback=%q err=%v", got, err)
	}
	for _, region := range []string{"us-east-1", "eu-west-1"} {
		want := "https://runtime." + region + ".kiro.dev"
		got, err := ResolveDestination("", region)
		if err != nil || got != want {
			t.Fatalf("region %q got=%q err=%v", region, got, err)
		}
	}
}
