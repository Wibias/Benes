package codexauth

import "strings"

func UsageLogLabel(accountID string, accounts ManagedAccountConfig) string {
	id := strings.TrimSpace(accountID)
	if id == "" {
		return ""
	}
	if id == MainAccountID {
		return "main"
	}
	for _, account := range accounts.Accounts {
		if account.ID != id {
			continue
		}
		if account.IsMain {
			return "main"
		}
		return strings.TrimSpace(account.LogLabel)
	}
	return ""
}
