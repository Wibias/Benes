package config

import "testing"

func TestResolveEnvironmentDataPlaneTokenTrims(t *testing.T) {
	got := ResolveEnvironmentDataPlaneToken(map[string]string{
		"BENES_API_AUTH_TOKEN": "  new-secret  ",
	})
	if got != "new-secret" {
		t.Fatalf("got=%q", got)
	}
}

func TestResolveEnvironmentDataPlaneTokenIgnoresBlank(t *testing.T) {
	if got := ResolveEnvironmentDataPlaneToken(map[string]string{
		"BENES_API_AUTH_TOKEN": "   ",
	}); got != "" {
		t.Fatalf("got=%q", got)
	}
}

func TestResolveEnvironmentDataPlaneTokenReturnsEmptyWhenUnset(t *testing.T) {
	if got := ResolveEnvironmentDataPlaneToken(map[string]string{}); got != "" {
		t.Fatalf("got=%q", got)
	}
}

func TestResolveEnvironmentDataPlaneTokenExplicitMapDoesNotReadProcessEnvironment(t *testing.T) {
	t.Setenv("BENES_API_AUTH_TOKEN", "process-secret")
	if got := ResolveEnvironmentDataPlaneToken(map[string]string{}); got != "" {
		t.Fatalf("got=%q", got)
	}
}

func TestResolveEnvironmentDataPlaneTokenReadsProcessEnvironmentWhenNil(t *testing.T) {
	t.Setenv("BENES_API_AUTH_TOKEN", " process-new ")
	if got := ResolveEnvironmentDataPlaneToken(nil); got != "process-new" {
		t.Fatalf("got=%q", got)
	}
}
