package codexauth

import (
	"regexp"
	"sort"
	"strings"
)

var managedAccountIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

var reservedManagedAccountIDs = map[string]struct{}{
	"__proto__":   {},
	"prototype":   {},
	"constructor": {},
}

type StaticEligibilityOptions struct {
	IncludeMain      bool
	ExcludeAccountID string
}

func IsValidManagedAccountID(id string) bool {
	if id == MainAccountID || !managedAccountIDPattern.MatchString(id) {
		return false
	}
	_, reserved := reservedManagedAccountIDs[strings.ToLower(id)]
	return !reserved
}

func IsSelectableManagedAccount(account ManagedAccount) bool {
	return !account.IsMain && IsValidManagedAccountID(account.ID)
}

func StaticEligibleAccountIDs(
	config ManagedAccountConfig,
	credentials ManagedCredentialSnapshot,
	main MainCredentialResult,
	options StaticEligibilityOptions,
) []string {
	ids := make([]string, 0, len(config.Accounts)+1)

	if options.IncludeMain && main.Selectable() && mainAllowed(config, options.ExcludeAccountID) {
		ids = append(ids, MainAccountID)
	}

	if credentials.Status != ManagedCredentialStoreOK {
		return ids
	}
	for _, account := range config.Accounts {
		if !IsSelectableManagedAccount(account) {
			continue
		}
		if account.ID == options.ExcludeAccountID || config.PausedAccountIDs[account.ID] {
			continue
		}
		record, exists := credentials.Records[account.ID]
		if !exists || record.Credential == nil || record.DeletedAtMS != nil {
			continue
		}
		ids = append(ids, account.ID)
	}
	return ids
}

func mainAllowed(config ManagedAccountConfig, excluded string) bool {
	if excluded == MainAccountID || config.PausedAccountIDs[MainAccountID] {
		return false
	}
	for _, account := range config.Accounts {
		if !account.IsMain && account.ID == MainAccountID {
			return false
		}
	}
	return true
}

func AccountPriority(config ManagedAccountConfig, accountID string) int {
	return config.Priorities[accountID]
}

func SelectPriorityTier(
	ids []string,
	priorityOf func(string) int,
	hasHeadroom func(string) bool,
	pinnedID string,
) []string {
	if len(ids) <= 1 {
		return append([]string(nil), ids...)
	}

	priorities := make([]int, len(ids))
	allSame := true
	for index, id := range ids {
		priorities[index] = priorityOf(id)
		if index > 0 && priorities[index] != priorities[0] {
			allSame = false
		}
	}
	if allSame {
		return append([]string(nil), ids...)
	}

	ceilingSet := false
	ceiling := 0
	if pinnedID != "" && hasHeadroom(pinnedID) {
		for index, id := range ids {
			if id == pinnedID {
				ceilingSet = true
				ceiling = priorities[index]
				break
			}
		}
	}

	unique := make(map[int]struct{}, len(priorities))
	for _, priority := range priorities {
		unique[priority] = struct{}{}
	}
	tiers := make([]int, 0, len(unique))
	for priority := range unique {
		tiers = append(tiers, priority)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(tiers)))

	for _, tier := range tiers {
		if ceilingSet && tier > ceiling {
			continue
		}
		members := make([]string, 0, len(ids))
		hasUsableMember := false
		for index, id := range ids {
			if priorities[index] != tier {
				continue
			}
			members = append(members, id)
			if hasHeadroom(id) {
				hasUsableMember = true
			}
		}
		if hasUsableMember {
			return members
		}
	}

	return append([]string(nil), ids...)
}
