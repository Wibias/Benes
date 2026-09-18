package kiro

import "fmt"

type ImportedCredential struct {
	AccessToken  string
	Refresh      string
	ProfileARN   string
	APIRegion    string
	SSORegion    string
	Account      string
	ClientID     string
	ClientSecret string
	AuthType     string
	ExpiresUnix  int64
}

func ImportSnapshot(cred ImportedCredential) (AccountSnapshot, error) {
	snap := AccountSnapshot{
		AccessToken: cred.AccessToken,
		Refresh:     cred.Refresh,
		ProfileARN:  cred.ProfileARN,
		APIRegion:   cred.APIRegion,
		SSORegion:   cred.SSORegion,
		Account:     cred.Account,
		AuthType:    cred.AuthType,
	}
	if err := snap.Validate(); err != nil {
		return AccountSnapshot{}, err
	}
	if inferred := InferRegionFromProfileARN(snap.ProfileARN); inferred != "" {
		if snap.APIRegion == "" {
			snap.APIRegion = inferred
		} else if NormalizeRegion(snap.APIRegion) != inferred {
			return AccountSnapshot{}, fmt.Errorf("%w: imported apiRegion does not match profileArn", ErrSplitSnapshot)
		}
	}
	return snap, nil
}

func RevalidateSnapshot(current, latest AccountSnapshot) error {
	if err := current.Validate(); err != nil {
		return err
	}
	if err := latest.Validate(); err != nil {
		return err
	}
	if current.AccessToken != latest.AccessToken || current.ProfileARN != latest.ProfileARN || current.EffectiveRegion() != latest.EffectiveRegion() {
		return ErrSplitSnapshot
	}
	return nil
}
