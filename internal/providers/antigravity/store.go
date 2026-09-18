package antigravity

import (
	"encoding/json"
	"os"
	"strings"
)

const AuthStoreProvider = "google-antigravity"

type StoredAccount struct {
	ID           string
	AccountID    string
	Token        string
	Refresh      string
	ProjectID    string
	Email        string
	Expires      int64
	NeedsReauth  bool
	ProfileARN   string
	APIRegion    string
	SSORegion    string
	AuthType     string
	ClientID     string
	ClientSecret string
}

func LoadAccountsFile(path string) ([]Account, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	accounts := AccountsFromAuthStore(raw)
	for i := range accounts {
		accounts[i].SourcePath = path
	}
	return accounts, nil
}

func AccountsFromAuthStore(raw []byte) []Account {
	stored := ParseAuthStore(raw)
	out := make([]Account, 0, len(stored))
	for _, account := range stored {
		if account.NeedsReauth || strings.TrimSpace(account.Token) == "" {
			continue
		}
		id := strings.TrimSpace(account.ID)
		if id == "" {
			id = strings.TrimSpace(account.ProjectID)
		}
		if id == "" {
			continue
		}
		out = append(out, Account{ID: id, Token: account.Token, ProjectID: strings.TrimSpace(account.ProjectID)})
	}
	return out
}

type PublicAccount struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId,omitempty"`
	Email       string `json:"email,omitempty"`
	NeedsReauth bool   `json:"needsReauth,omitempty"`
}

func LoadPublicAccounts(path string) ([]PublicAccount, string, error) {
	return LoadPublicAccountsFor(path, AuthStoreProvider)
}

func LoadPublicAccountsFor(path, provider string) ([]PublicAccount, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", err
	}
	stored := ParseAuthStoreFor(raw, provider)
	active := parseActiveAccountIDFor(raw, provider)
	out := make([]PublicAccount, 0, len(stored))
	for _, account := range stored {
		id := strings.TrimSpace(account.ID)
		if id == "" {
			id = strings.TrimSpace(account.ProjectID)
		}
		if id == "" {
			continue
		}
		out = append(out, PublicAccount{
			ID:          id,
			ProjectID:   strings.TrimSpace(account.ProjectID),
			Email:       strings.TrimSpace(account.Email),
			NeedsReauth: account.NeedsReauth || strings.TrimSpace(account.Token) == "",
		})
	}
	if active == "" && len(out) == 1 {
		active = out[0].ID
	}
	return out, active, nil
}

func parseActiveAccountID(raw []byte) string {
	return parseActiveAccountIDFor(raw, AuthStoreProvider)
}

func parseActiveAccountIDFor(raw []byte, provider string) string {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return ""
	}
	entry, ok := root[provider]
	if !ok {
		return ""
	}
	var set struct {
		ActiveAccountID string `json:"activeAccountId"`
	}
	if json.Unmarshal(entry, &set) != nil {
		return ""
	}
	return strings.TrimSpace(set.ActiveAccountID)
}

func ActiveStoredAccount(path, provider string) (StoredAccount, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return StoredAccount{}, false
	}
	stored := ParseAuthStoreFor(raw, provider)
	if len(stored) == 0 {
		return StoredAccount{}, false
	}
	active := parseActiveAccountIDFor(raw, provider)
	if active != "" {
		for _, account := range stored {
			id := strings.TrimSpace(account.ID)
			if id == "" {
				id = strings.TrimSpace(account.ProjectID)
			}
			if id == active {
				return account, strings.TrimSpace(account.Token) != "" && !account.NeedsReauth
			}
		}
		return StoredAccount{}, false
	}
	if len(stored) == 1 {
		account := stored[0]
		return account, strings.TrimSpace(account.Token) != "" && !account.NeedsReauth
	}
	return StoredAccount{}, false
}

func ParseAuthStore(raw []byte) []StoredAccount {
	return ParseAuthStoreFor(raw, AuthStoreProvider)
}

func ParseAuthStoreFor(raw []byte, provider string) []StoredAccount {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return nil
	}
	entry, ok := root[provider]
	if !ok {
		return nil
	}
	var set struct {
		ActiveAccountID string `json:"activeAccountId"`
		Accounts        []struct {
			ID          string `json:"id"`
			NeedsReauth bool   `json:"needsReauth"`
			Credential  struct {
				Access       string `json:"access"`
				Refresh      string `json:"refresh"`
				Expires      int64  `json:"expires"`
				ProjectID    string `json:"projectId"`
				Email        string `json:"email"`
				AccountID    string `json:"accountId"`
				ProfileARN   string `json:"profileArn"`
				APIRegion    string `json:"apiRegion"`
				SSORegion    string `json:"ssoRegion"`
				AuthType     string `json:"authType"`
				ClientID     string `json:"clientId"`
				ClientSecret string `json:"clientSecret"`
			} `json:"credential"`
		} `json:"accounts"`
	}
	if json.Unmarshal(entry, &set) == nil && len(set.Accounts) > 0 {
		out := make([]StoredAccount, 0, len(set.Accounts))
		for _, account := range set.Accounts {
			out = append(out, StoredAccount{
				ID:           account.ID,
				AccountID:    account.Credential.AccountID,
				Token:        account.Credential.Access,
				Refresh:      account.Credential.Refresh,
				ProjectID:    account.Credential.ProjectID,
				Email:        account.Credential.Email,
				Expires:      account.Credential.Expires,
				NeedsReauth:  account.NeedsReauth,
				ProfileARN:   account.Credential.ProfileARN,
				APIRegion:    account.Credential.APIRegion,
				SSORegion:    account.Credential.SSORegion,
				AuthType:     account.Credential.AuthType,
				ClientID:     account.Credential.ClientID,
				ClientSecret: account.Credential.ClientSecret,
			})
		}
		return out
	}
	var legacy struct {
		Access    string `json:"access"`
		Refresh   string `json:"refresh"`
		Expires   int64  `json:"expires"`
		ProjectID string `json:"projectId"`
	}
	if json.Unmarshal(entry, &legacy) == nil && strings.TrimSpace(legacy.Access) != "" {
		return []StoredAccount{{
			ID:        AuthStoreProvider,
			Token:     legacy.Access,
			Refresh:   legacy.Refresh,
			ProjectID: legacy.ProjectID,
			Expires:   legacy.Expires,
		}}
	}
	return nil
}
