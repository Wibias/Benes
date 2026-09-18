package codexauth

import (
	"strings"
	"testing"
)

func TestManagedAccountIDValidationMatchesPersistedContract(t *testing.T) {
	valid := []string{
		"a",
		"acct-1",
		"acct_1",
		"acct.1",
		strings.Repeat("a", 64),
	}
	for _, id := range valid {
		if !IsValidManagedAccountID(id) {
			t.Errorf("valid id rejected: %q", id)
		}
	}

	invalid := []string{
		"",
		MainAccountID,
		"__proto__",
		"__PrOtO__",
		"prototype",
		"Constructor",
		"has space",
		"has/slash",
		"ümlaut",
		strings.Repeat("a", 65),
	}
	for _, id := range invalid {
		if IsValidManagedAccountID(id) {
			t.Errorf("invalid id accepted: %q", id)
		}
	}
}

func TestStaticEligibleAccountIDsCombinesMainAndManagedWithoutRoutingState(t *testing.T) {
	config := ManagedAccountConfig{
		Accounts: []ManagedAccount{
			{ID: "acct-a"},
			{ID: "acct-paused"},
			{ID: "acct-deleted"},
			{ID: "acct-missing"},
			{ID: "bad/id"},
			{ID: "legacy-main", IsMain: true},
		},
		PausedAccountIDs: map[string]bool{"acct-paused": true},
	}
	credentials := ManagedCredentialSnapshot{
		Status: ManagedCredentialStoreOK,
		Records: map[string]ManagedCredentialRecord{
			"acct-a": {
				Generation: 3,
				Credential: &ManagedCredential{AccessToken: "a", RefreshToken: "ra", ExpiresAtMS: 1, ChatGPTAccountID: "chat-a"},
			},
			"acct-paused": {
				Credential: &ManagedCredential{AccessToken: "p", RefreshToken: "rp", ExpiresAtMS: 1, ChatGPTAccountID: "chat-p"},
			},
			"acct-deleted": {
				Generation:  5,
				Credential:  &ManagedCredential{AccessToken: "d", RefreshToken: "rd", ExpiresAtMS: 1, ChatGPTAccountID: "chat-d"},
				DeletedAtMS: int64Ptr(123),
			},
			"bad/id": {
				Credential: &ManagedCredential{AccessToken: "bad", RefreshToken: "rb", ExpiresAtMS: 1, ChatGPTAccountID: "chat-bad"},
			},
			"legacy-main": {
				Credential: &ManagedCredential{AccessToken: "legacy", RefreshToken: "rl", ExpiresAtMS: 1, ChatGPTAccountID: "chat-l"},
			},
		},
	}
	main := MainCredentialResult{
		Status: MainCredentialOK,
		Credential: MainCredential{AccessToken: "main", ChatGPTAccountID: "chat-main"},
	}

	got := StaticEligibleAccountIDs(config, credentials, main, StaticEligibilityOptions{IncludeMain: true})
	want := []string{MainAccountID, "acct-a"}
	assertStringsEqual(t, got, want)
}

func TestStaticEligibleAccountIDsDoesNotTreatManagedExpiryAsStaticIneligibility(t *testing.T) {
	config := ManagedAccountConfig{Accounts: []ManagedAccount{{ID: "acct-expired-token"}}}
	credentials := ManagedCredentialSnapshot{
		Status: ManagedCredentialStoreOK,
		Records: map[string]ManagedCredentialRecord{
			"acct-expired-token": {
				Credential: &ManagedCredential{
					AccessToken: "access", RefreshToken: "refresh", ExpiresAtMS: 1, ChatGPTAccountID: "chat",
				},
			},
		},
	}
	got := StaticEligibleAccountIDs(config, credentials, MainCredentialResult{}, StaticEligibilityOptions{})
	assertStringsEqual(t, got, []string{"acct-expired-token"})
}

func TestStaticEligibleAccountIDsMainFencesAndLegacyCollision(t *testing.T) {
	main := MainCredentialResult{Status: MainCredentialOK, Credential: MainCredential{AccessToken: "main"}}

	for _, tc := range []struct {
		name    string
		config  ManagedAccountConfig
		options StaticEligibilityOptions
	}{
		{
			name:   "main disabled by caller",
			config: ManagedAccountConfig{},
		},
		{
			name: "main paused",
			config: ManagedAccountConfig{PausedAccountIDs: map[string]bool{MainAccountID: true}},
			options: StaticEligibilityOptions{IncludeMain: true},
		},
		{
			name:   "main excluded",
			config: ManagedAccountConfig{},
			options: StaticEligibilityOptions{IncludeMain: true, ExcludeAccountID: MainAccountID},
		},
		{
			name: "legacy collision suppresses physical main",
			config: ManagedAccountConfig{Accounts: []ManagedAccount{{ID: MainAccountID, IsMain: false}}},
			options: StaticEligibilityOptions{IncludeMain: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := StaticEligibleAccountIDs(tc.config, ManagedCredentialSnapshot{Status: ManagedCredentialStoreMissing}, main, tc.options)
			if len(got) != 0 {
				t.Fatalf("eligible=%#v", got)
			}
		})
	}

	for _, status := range []MainCredentialStatus{MainCredentialMissing, MainCredentialInvalid, MainCredentialUnreadable, MainCredentialExpired} {
		t.Run(string(status), func(t *testing.T) {
			got := StaticEligibleAccountIDs(
				ManagedAccountConfig{},
				ManagedCredentialSnapshot{Status: ManagedCredentialStoreMissing},
				MainCredentialResult{Status: status},
				StaticEligibilityOptions{IncludeMain: true},
			)
			if len(got) != 0 {
				t.Fatalf("eligible=%#v", got)
			}
		})
	}
}

func TestStaticEligibleAccountIDsFailClosedOnManagedStoreFailureAndApplyExclusionBeforeTiering(t *testing.T) {
	config := ManagedAccountConfig{
		Accounts: []ManagedAccount{{ID: "high"}, {ID: "low"}},
		Priorities: map[string]int{"high": 10, "low": 0},
	}
	records := map[string]ManagedCredentialRecord{
		"high": {Credential: &ManagedCredential{AccessToken: "a", RefreshToken: "r", ChatGPTAccountID: "c"}},
		"low":  {Credential: &ManagedCredential{AccessToken: "b", RefreshToken: "r", ChatGPTAccountID: "c"}},
	}
	for _, status := range []ManagedCredentialStoreStatus{ManagedCredentialStoreInvalid, ManagedCredentialStoreUnreadable} {
		got := StaticEligibleAccountIDs(config, ManagedCredentialSnapshot{Status: status, Records: records}, MainCredentialResult{}, StaticEligibilityOptions{})
		if len(got) != 0 {
			t.Fatalf("status=%q eligible=%#v", status, got)
		}
	}

	eligible := StaticEligibleAccountIDs(
		config,
		ManagedCredentialSnapshot{Status: ManagedCredentialStoreOK, Records: records},
		MainCredentialResult{},
		StaticEligibilityOptions{ExcludeAccountID: "high"},
	)
	assertStringsEqual(t, eligible, []string{"low"})
	tier := SelectPriorityTier(eligible, func(id string) int { return AccountPriority(config, id) }, func(string) bool { return true }, "")
	assertStringsEqual(t, tier, []string{"low"})
}

func TestSelectPriorityTierMatchesBunOrderingContract(t *testing.T) {
	priority := map[string]int{"main": 0, "a": 10, "b": 10, "c": 0, "d": -10}
	priorityOf := func(id string) int { return priority[id] }

	for _, tc := range []struct {
		name       string
		ids        []string
		headroom   map[string]bool
		pinned     string
		want       []string
	}{
		{
			name:     "same priority preserves input order",
			ids:      []string{"main", "c"},
			headroom: map[string]bool{"main": true, "c": true},
			want:     []string{"main", "c"},
		},
		{
			name:     "highest healthy tier wins and preserves member order",
			ids:      []string{"main", "b", "a", "c", "d"},
			headroom: map[string]bool{"a": true, "b": true, "main": true, "c": true, "d": true},
			want:     []string{"b", "a"},
		},
		{
			name:     "drained highest tier descends",
			ids:      []string{"main", "a", "b", "c", "d"},
			headroom: map[string]bool{"a": false, "b": false, "main": true, "c": false, "d": true},
			want:     []string{"main", "c"},
		},
		{
			name:     "all drained preserves original list",
			ids:      []string{"main", "a", "d"},
			headroom: map[string]bool{},
			want:     []string{"main", "a", "d"},
		},
		{
			name:     "live pin lowers priority ceiling",
			ids:      []string{"a", "c", "d"},
			headroom: map[string]bool{"a": true, "c": true, "d": true},
			pinned:   "c",
			want:     []string{"c"},
		},
		{
			name:     "drained pin does not lower ceiling",
			ids:      []string{"a", "c", "d"},
			headroom: map[string]bool{"a": true, "c": false, "d": true},
			pinned:   "c",
			want:     []string{"a"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headroom := func(id string) bool { return tc.headroom[id] }
			got := SelectPriorityTier(tc.ids, priorityOf, headroom, tc.pinned)
			assertStringsEqual(t, got, tc.want)
		})
	}
}

func TestAccountPriorityDefaultsToZero(t *testing.T) {
	config := ManagedAccountConfig{Priorities: map[string]int{"known": 42}}
	if got := AccountPriority(config, "known"); got != 42 {
		t.Fatalf("known priority=%d", got)
	}
	if got := AccountPriority(config, "unknown"); got != 0 {
		t.Fatalf("unknown priority=%d", got)
	}
}

func assertStringsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got=%#v want=%#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%#v want=%#v", got, want)
		}
	}
}

func int64Ptr(value int64) *int64 { return &value }
