package kiro

import (
	"errors"
	"testing"
)

func TestAccountSnapshotFailsClosedOnMissingOrSplitIdentity(t *testing.T) {
	ok := AccountSnapshot{
		AccessToken: "tok",
		ProfileARN:  "arn:aws:codewhisperer:us-east-1:123456789012:profile/abc",
		APIRegion:   "us-east-1",
	}
	if err := ok.Validate(); err != nil || ok.EffectiveRegion() != "us-east-1" {
		t.Fatalf("ok=%v region=%s", err, ok.EffectiveRegion())
	}
	if err := (AccountSnapshot{AccessToken: "tok"}).Validate(); !errors.Is(err, ErrMissingProfile) {
		t.Fatalf("missing=%v", err)
	}
	if err := (AccountSnapshot{AccessToken: "tok", AuthType: "kiro_desktop", APIRegion: "us-east-1"}).Validate(); !errors.Is(err, ErrMissingProfile) {
		t.Fatalf("desktop missing=%v", err)
	}
	split := AccountSnapshot{
		AccessToken: "tok",
		ProfileARN:  "arn:aws:codewhisperer:us-west-2:123456789012:profile/abc",
		APIRegion:   "us-east-1",
	}
	if err := split.Validate(); !errors.Is(err, ErrSplitSnapshot) {
		t.Fatalf("split=%v", err)
	}
}

func TestAccountSnapshotAllowsBuilderIDWithoutProfileARN(t *testing.T) {
	for _, auth := range []string{"aws_sso_oidc", "builder-id"} {
		snap := AccountSnapshot{
			AccessToken: "tok",
			APIRegion:   "eu-west-1",
			AuthType:    auth,
		}
		if err := snap.Validate(); err != nil {
			t.Fatalf("auth=%s err=%v", auth, err)
		}
		if snap.ProfileARN != "" {
			t.Fatalf("auth=%s synthesized profile %q", auth, snap.ProfileARN)
		}
		if snap.EffectiveRegion() != "eu-west-1" {
			t.Fatalf("auth=%s region=%s", auth, snap.EffectiveRegion())
		}
	}
}
