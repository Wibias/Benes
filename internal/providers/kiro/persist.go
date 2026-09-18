package kiro

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/Wibias/Benes/internal/providers/antigravity"
)

const ProviderID = "kiro"

func Persist(path string, cred ImportedCredential) error {
	return antigravity.AppendStoredAccount(path, ProviderID, StoredFromImported(cred))
}

func AccountID(cred ImportedCredential) string {
	if profile := strings.TrimSpace(cred.ProfileARN); profile != "" {
		return profile
	}
	if account := strings.TrimSpace(cred.Account); account != "" {
		return account
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(cred.Refresh) + "\x00" + strings.TrimSpace(cred.AuthType)))
	return "kiro-" + hex.EncodeToString(sum[:8])
}

func StoredFromImported(cred ImportedCredential) antigravity.StoredAccount {
	return antigravity.StoredAccount{
		ID:           AccountID(cred),
		Token:        cred.AccessToken,
		Refresh:      cred.Refresh,
		Expires:      cred.ExpiresUnix,
		ProfileARN:   cred.ProfileARN,
		APIRegion:    cred.APIRegion,
		SSORegion:    cred.SSORegion,
		AuthType:     cred.AuthType,
		ClientID:     cred.ClientID,
		ClientSecret: cred.ClientSecret,
	}
}

func SnapshotFromStored(account antigravity.StoredAccount) (AccountSnapshot, error) {
	return ImportSnapshot(ImportedCredential{
		AccessToken: account.Token,
		Refresh:     account.Refresh,
		ProfileARN:  account.ProfileARN,
		APIRegion:   account.APIRegion,
		SSORegion:   account.SSORegion,
		AuthType:    account.AuthType,
		ExpiresUnix: account.Expires,
	})
}

func RefreshAccountFromStored(account antigravity.StoredAccount) RefreshAccount {
	source := "stored"
	if strings.TrimSpace(account.ClientID) != "" {
		source = "local-cli"
	}
	return RefreshAccount{
		ProfileARN:   account.ProfileARN,
		SSORegion:    account.SSORegion,
		APIRegion:    account.APIRegion,
		ClientID:     account.ClientID,
		ClientSecret: account.ClientSecret,
		Source:       source,
	}
}

func SeedProviderJSON() []byte {
	return []byte(`{"adapter":"kiro","baseUrl":"https://runtime.us-east-1.kiro.dev","authMode":"oauth"}`)
}
