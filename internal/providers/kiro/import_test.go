package kiro

import (
	"errors"
	"testing"
)

func TestImportSnapshotRejectsSplitAccountMetadata(t *testing.T) {
	ok, err := ImportSnapshot(ImportedCredential{
		AccessToken: "tok",
		ProfileARN:  "arn:aws:codewhisperer:us-east-1:123456789012:profile/abc",
		APIRegion:   "us-east-1",
		Account:     "acct-a",
	})
	if err != nil || ok.EffectiveRegion() != "us-east-1" {
		t.Fatalf("ok=%#v err=%v", ok, err)
	}
	if _, err := ImportSnapshot(ImportedCredential{
		AccessToken: "tok",
		ProfileARN:  "arn:aws:codewhisperer:us-west-2:123456789012:profile/abc",
		APIRegion:   "us-east-1",
	}); !errors.Is(err, ErrSplitSnapshot) {
		t.Fatalf("split=%v", err)
	}
	builder, err := ImportSnapshot(ImportedCredential{
		AccessToken: "tok",
		APIRegion:   "us-east-1",
		AuthType:    "aws_sso_oidc",
	})
	if err != nil || builder.AuthType != "aws_sso_oidc" || builder.ProfileARN != "" {
		t.Fatalf("builder=%#v err=%v", builder, err)
	}
	latest := ok
	latest.AccessToken = "other"
	if err := RevalidateSnapshot(ok, latest); !errors.Is(err, ErrSplitSnapshot) {
		t.Fatalf("switch=%v", err)
	}
}
