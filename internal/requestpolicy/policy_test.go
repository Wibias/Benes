package requestpolicy

import "testing"

func TestServiceTierPolicyValidation(t *testing.T) {
	valid := []string{"", "auto", "default", "flex", "priority"}
	for _, tier := range valid {
		t.Run("valid_"+tier, func(t *testing.T) {
			if err := (Policy{ServiceTier: tier}).Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
	invalid := []string{"fast", " Priority ", "PRIORITY", "premium"}
	for _, tier := range invalid {
		t.Run("invalid_"+tier, func(t *testing.T) {
			if err := (Policy{ServiceTier: tier}).Validate(); err == nil {
				t.Fatalf("Validate() accepted %q", tier)
			}
		})
	}
}

func TestResolveServiceTierPrecedence(t *testing.T) {
	configured := Policy{ServiceTier: "priority"}

	explicit := "flex"
	if got := configured.ResolveServiceTier(&explicit); got == nil || *got != "flex" {
		t.Fatalf("explicit client tier must win, got %#v", got)
	}

	blank := "  "
	if got := configured.ResolveServiceTier(&blank); got == nil || *got != "priority" {
		t.Fatalf("blank client tier must fall back to configured tier, got %#v", got)
	}

	if got := configured.ResolveServiceTier(nil); got == nil || *got != "priority" {
		t.Fatalf("configured tier must fill an absent client tier, got %#v", got)
	}

	if got := (Policy{}).ResolveServiceTier(nil); got != nil {
		t.Fatalf("unset policy must inject nothing, got %#v", got)
	}
}

func TestResolveServiceTierDoesNotAliasInput(t *testing.T) {
	explicit := "default"
	got := (Policy{ServiceTier: "priority"}).ResolveServiceTier(&explicit)
	if got == nil || *got != "default" {
		t.Fatalf("unexpected result %#v", got)
	}
	*got = "flex"
	if explicit != "default" {
		t.Fatalf("resolved tier aliased caller input: %q", explicit)
	}
}
