package kiro

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	AuthAWSSsoOIDC  = "aws_sso_oidc"
	AuthKiroDesktop = "kiro_desktop"
	AuthBuilderID   = "builder-id"
)

var profileARNPattern = regexp.MustCompile(`^arn:[a-z0-9-]+:codewhisperer:[a-z0-9-]+:\d{12}:profile/[A-Za-z0-9-]+$`)

var (
	ErrMissingProfile = fmt.Errorf("Kiro requires a CodeWhisperer profileArn for this account; re-login so the profile is captured")
	ErrSplitSnapshot  = fmt.Errorf("Kiro token and profile/region metadata are not from the same account snapshot")
)

type AccountSnapshot struct {
	AccessToken string
	Refresh     string
	ProfileARN  string
	APIRegion   string
	SSORegion   string
	Account     string
	AuthType    string
}

func (s AccountSnapshot) Validate() error {
	if strings.TrimSpace(s.AccessToken) == "" {
		return fmt.Errorf("Kiro access token is required")
	}
	profile := strings.TrimSpace(s.ProfileARN)
	if profile == "" {
		if s.requiresOwnedProfile() {
			return ErrMissingProfile
		}
		return nil
	}
	if !profileARNPattern.MatchString(profile) {
		return fmt.Errorf("Kiro profileArn is malformed")
	}
	if inferred := InferRegionFromProfileARN(profile); inferred != "" && s.APIRegion != "" && NormalizeRegion(s.APIRegion) != inferred {
		return ErrSplitSnapshot
	}
	return nil
}

func (s AccountSnapshot) requiresOwnedProfile() bool {
	switch strings.TrimSpace(s.AuthType) {
	case AuthAWSSsoOIDC, AuthBuilderID:
		return false
	default:
		return true
	}
}

func InferRegionFromProfileARN(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) < 4 {
		return ""
	}
	return NormalizeRegion(parts[3])
}

func (s AccountSnapshot) Ref() string {
	if profile := strings.TrimSpace(s.ProfileARN); profile != "" {
		return profile
	}
	return strings.TrimSpace(s.Account)
}

func (s AccountSnapshot) EffectiveRegion() string {
	if region := NormalizeRegion(s.APIRegion); region != "" {
		return region
	}
	if region := InferRegionFromProfileARN(s.ProfileARN); region != "" {
		return region
	}
	if region := NormalizeRegion(s.SSORegion); region != "" {
		return region
	}
	return DefaultRegion
}
