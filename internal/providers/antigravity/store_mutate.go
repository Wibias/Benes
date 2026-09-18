package antigravity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Wibias/Benes/internal/store/atomicfile"
)

var authStoreMutationMu sync.Mutex

func SetActiveAccount(path, id string) error {
	return SetActiveAccountFor(path, AuthStoreProvider, id)
}

func SetActiveAccountFor(path, provider, id string) error {
	id = strings.TrimSpace(id)
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("auth store path is required")
	}
	if strings.TrimSpace(provider) == "" {
		return fmt.Errorf("provider is required")
	}
	if id == "" {
		return fmt.Errorf("account id is required")
	}
	return mutateAuthStore(path, provider, func(accounts []StoredAccount, active string) ([]StoredAccount, string, error) {
		found := false
		for _, account := range accounts {
			if strings.TrimSpace(account.ID) == id {
				found = true
				break
			}
		}
		if !found {
			return nil, "", fmt.Errorf("unknown account")
		}
		return accounts, id, nil
	})
}

func RemoveAccount(path, id string) error {
	return RemoveAccountFor(path, AuthStoreProvider, id)
}

func RemoveAccountFor(path, provider, id string) error {
	id = strings.TrimSpace(id)
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("auth store path is required")
	}
	if strings.TrimSpace(provider) == "" {
		return fmt.Errorf("provider is required")
	}
	if id == "" {
		return fmt.Errorf("account id is required")
	}
	return mutateAuthStore(path, provider, func(accounts []StoredAccount, active string) ([]StoredAccount, string, error) {
		kept := make([]StoredAccount, 0, len(accounts))
		found := false
		for _, account := range accounts {
			if strings.TrimSpace(account.ID) == id {
				found = true
				continue
			}
			kept = append(kept, account)
		}
		if !found {
			return nil, "", fmt.Errorf("unknown account")
		}
		nextActive := strings.TrimSpace(active)
		if nextActive == id {
			nextActive = ""
			if len(kept) > 0 {
				nextActive = strings.TrimSpace(kept[0].ID)
			}
		}
		return kept, nextActive, nil
	})
}

func ClearAccountsFor(path, provider string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("auth store path is required")
	}
	if strings.TrimSpace(provider) == "" {
		return fmt.Errorf("provider is required")
	}
	return mutateAuthStore(path, provider, func([]StoredAccount, string) ([]StoredAccount, string, error) {
		return nil, "", nil
	})
}

func AppendStoredAccount(path, provider string, account StoredAccount) error {
	return AppendStoredAccountChecked(path, provider, account, nil)
}

func AppendStoredAccountChecked(path, provider string, account StoredAccount, gate func() error) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("auth store path is required")
	}
	if strings.TrimSpace(provider) == "" {
		return fmt.Errorf("provider is required")
	}
	id := strings.TrimSpace(account.ID)
	if id == "" {
		id = strings.TrimSpace(account.ProjectID)
	}
	if id == "" {
		return fmt.Errorf("account id is required")
	}
	if strings.TrimSpace(account.Token) == "" {
		return fmt.Errorf("account token is required")
	}
	account.ID = id
	return mutateAuthStoreChecked(path, provider, func(accounts []StoredAccount, active string) ([]StoredAccount, string, error) {
		replaced := false
		for i, existing := range accounts {
			if strings.TrimSpace(existing.ID) == id {
				accounts[i] = account
				replaced = true
				break
			}
		}
		if !replaced {
			accounts = append(accounts, account)
		}
		return accounts, id, nil
	}, gate)
}

func mutateAuthStore(path, provider string, mut func([]StoredAccount, string) ([]StoredAccount, string, error)) error {
	return mutateAuthStoreChecked(path, provider, mut, nil)
}

func mutateAuthStoreChecked(path, provider string, mut func([]StoredAccount, string) ([]StoredAccount, string, error), gate func() error) error {
	authStoreMutationMu.Lock()
	defer authStoreMutationMu.Unlock()

	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		raw = []byte("{}")
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		root = map[string]json.RawMessage{}
	}
	accounts := ParseAuthStoreFor(raw, provider)
	active := parseActiveAccountIDFor(raw, provider)
	nextAccounts, nextActive, err := mut(accounts, active)
	if err != nil {
		return err
	}
	if gate != nil {
		if err := gate(); err != nil {
			return err
		}
	}
	if len(nextAccounts) == 0 {
		delete(root, provider)
	} else {
		rows := make([]map[string]any, 0, len(nextAccounts))
		for _, account := range nextAccounts {
			cred := map[string]any{
				"access":  account.Token,
				"refresh": account.Refresh,
				"expires": account.Expires,
			}
			if strings.TrimSpace(account.ProjectID) != "" {
				cred["projectId"] = account.ProjectID
			}
			if strings.TrimSpace(account.Email) != "" {
				cred["email"] = account.Email
			}
			if strings.TrimSpace(account.AccountID) != "" {
				cred["accountId"] = account.AccountID
			}
			if strings.TrimSpace(account.ProfileARN) != "" {
				cred["profileArn"] = account.ProfileARN
			}
			if strings.TrimSpace(account.APIRegion) != "" {
				cred["apiRegion"] = account.APIRegion
			}
			if strings.TrimSpace(account.SSORegion) != "" {
				cred["ssoRegion"] = account.SSORegion
			}
			if strings.TrimSpace(account.AuthType) != "" {
				cred["authType"] = account.AuthType
			}
			if strings.TrimSpace(account.ClientID) != "" {
				cred["clientId"] = account.ClientID
			}
			if strings.TrimSpace(account.ClientSecret) != "" {
				cred["clientSecret"] = account.ClientSecret
			}
			rows = append(rows, map[string]any{
				"id":          account.ID,
				"needsReauth": account.NeedsReauth,
				"credential":  cred,
			})
		}
		if nextActive == "" {
			nextActive = strings.TrimSpace(nextAccounts[0].ID)
		}
		entry, err := json.Marshal(map[string]any{
			"activeAccountId": nextActive,
			"accounts":        rows,
		})
		if err != nil {
			return err
		}
		root[provider] = entry
	}
	out, err := json.Marshal(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if len(root) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return atomicfile.Write(path, out, atomicfile.Options{Mode: 0o600})
}

func UpsertByIdentity(path, provider string, cred ImportCredential) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("auth store path is required")
	}
	if strings.TrimSpace(provider) == "" {
		return "", fmt.Errorf("provider is required")
	}
	if strings.TrimSpace(cred.AccountID) == "" && strings.TrimSpace(cred.Email) == "" {
		return "", fmt.Errorf("verified identity is required")
	}
	if strings.TrimSpace(cred.Token) == "" {
		return "", fmt.Errorf("account token is required")
	}
	disposition := "inserted"
	err := mutateAuthStore(path, provider, func(accounts []StoredAccount, active string) ([]StoredAccount, string, error) {
		next := StoredAccount{
			AccountID: strings.TrimSpace(cred.AccountID),
			Token:     cred.Token,
			Refresh:   cred.Refresh,
			ProjectID: cred.ProjectID,
			Email:     strings.ToLower(strings.TrimSpace(cred.Email)),
			Expires:   cred.Expires,
		}
		for i, existing := range accounts {
			if matchIdentity(existing, next) {
				next.ID = existing.ID
				accounts[i] = next
				disposition = "updated"
				if strings.TrimSpace(active) == "" {
					active = next.ID
				}
				return accounts, active, nil
			}
		}
		next.ID = newStoredAccountID(next, accounts)
		accounts = append(accounts, next)
		if strings.TrimSpace(active) == "" {
			active = next.ID
		}
		return accounts, active, nil
	})
	return disposition, err
}

func matchIdentity(existing, next StoredAccount) bool {
	if next.AccountID != "" && existing.AccountID == next.AccountID {
		return true
	}
	if next.AccountID != "" && existing.AccountID == "" && existing.Email != "" && strings.EqualFold(existing.Email, next.Email) {
		return true
	}
	if next.AccountID == "" && existing.AccountID == "" && existing.Email != "" && strings.EqualFold(existing.Email, next.Email) {
		return true
	}
	return false
}

func newStoredAccountID(cred StoredAccount, accounts []StoredAccount) string {
	identity := cred.AccountID
	if identity == "" {
		identity = cred.Email
	}
	if identity == "" {
		identity = cred.Refresh
	}
	sum := sha256.Sum256([]byte(identity))
	base := hex.EncodeToString(sum[:])[:32]
	occupied := map[string]bool{}
	for _, account := range accounts {
		occupied[account.ID] = true
	}
	if !occupied[base] {
		return base
	}
	for suffix := 1; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d", base, suffix)
		if !occupied[candidate] {
			return candidate
		}
	}
}
