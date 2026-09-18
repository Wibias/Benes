package codexauth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestProjectManagedAccountConfigPreservesRoutingMetadata(t *testing.T) {
	raw := []byte(`{
		"codexAccounts": [
			{"id":"acct-a","email":"a@example.com","alias":"Primary","plan":"plus","chatgptAccountId":"chat-a","logLabel":"alpha","isMain":false},
			{"id":"__main__","email":"legacy@example.com","isMain":false},
			{"id":"old-main-row","email":"old@example.com","isMain":true},
			{"id":"acct-b"}
		],
		"pausedCodexAccountIds": ["acct-b", "acct-b", "ghost"],
		"codexAccountPriorities": {"acct-a":12,"__main__":100,"acct-b":-7},
		"activeCodexAccountId": "acct-a",
		"activeCodexAccountPinned": "acct-a"
	}`)

	got, err := ProjectManagedAccountConfig(raw)
	if err != nil {
		t.Fatalf("ProjectManagedAccountConfig(): %v", err)
	}
	if len(got.Accounts) != 4 {
		t.Fatalf("accounts=%#v", got.Accounts)
	}
	if got.Accounts[0] != (ManagedAccount{
		ID: "acct-a", Email: "a@example.com", Alias: "Primary", Plan: "plus",
		ChatGPTAccountID: "chat-a", LogLabel: "alpha", IsMain: false,
	}) {
		t.Fatalf("account[0]=%#v", got.Accounts[0])
	}
	if got.Accounts[1].ID != MainAccountID || got.Accounts[1].IsMain {
		t.Fatalf("legacy main collision row not preserved: %#v", got.Accounts[1])
	}
	if !got.Accounts[2].IsMain || got.Accounts[2].ID != "old-main-row" {
		t.Fatalf("legacy main row=%#v", got.Accounts[2])
	}
	if got.Accounts[3].ID != "acct-b" || got.Accounts[3].Email != "" || got.Accounts[3].IsMain {
		t.Fatalf("sparse account=%#v", got.Accounts[3])
	}
	if len(got.PausedAccountIDs) != 2 || !got.PausedAccountIDs["acct-b"] || !got.PausedAccountIDs["ghost"] {
		t.Fatalf("paused=%#v", got.PausedAccountIDs)
	}
	if got.Priorities["acct-a"] != 12 || got.Priorities[MainAccountID] != 100 || got.Priorities["acct-b"] != -7 {
		t.Fatalf("priorities=%#v", got.Priorities)
	}
	if got.ActiveAccountID != "acct-a" || got.PinnedAccountID != "acct-a" {
		t.Fatalf("active=%q pinned=%q", got.ActiveAccountID, got.PinnedAccountID)
	}
}

func TestProjectManagedAccountConfigRejectsMalformedConsumedFields(t *testing.T) {
	for _, raw := range []string{
		`[]`,
		`{"codexAccounts":{}}`,
		`{"codexAccounts":[42]}`,
		`{"codexAccounts":[{"id":42}]}`,
		`{"codexAccounts":[{"id":"acct","alias":42}]}`,
		`{"pausedCodexAccountIds":"acct"}`,
		`{"pausedCodexAccountIds":["acct",42]}`,
		`{"codexAccountPriorities":[]}`,
		`{"codexAccountPriorities":{"acct":101}}`,
		`{"codexAccountPriorities":{"acct":1.5}}`,
		`{"activeCodexAccountId":42}`,
		`{"activeCodexAccountPinned":false}`,
	} {
		if _, err := ProjectManagedAccountConfig([]byte(raw)); err == nil {
			t.Fatalf("accepted malformed config %s", raw)
		}
	}
}

func TestManagedCredentialStoreNormalizesLegacyAndVersionedRecords(t *testing.T) {
	home := t.TempDir()
	writeManagedStore(t, home, []byte(`{
		"legacy": {
			"accessToken":"legacy-access",
			"refreshToken":"legacy-refresh",
			"expiresAt":1700001000000,
			"chatgptAccountId":"chat-legacy"
		},
		"current": {
			"generation":7,
			"credential": {
				"accessToken":"current-access",
				"refreshToken":"current-refresh",
				"expiresAt":1700002000000,
				"chatgptAccountId":"chat-current"
			},
			"refreshGrantFingerprint":"fp-1",
			"replacedAt":1700000000000
		},
		"deleted": {"generation":9,"deletedAt":1700003000000},
		"invalid": {"generation":"seven","credential":{}}
	}`), 0o600)

	store := mustManagedCredentialStore(t, home)
	got := store.Read()
	if got.Status != ManagedCredentialStoreOK {
		t.Fatalf("status=%q", got.Status)
	}
	if len(got.Records) != 3 {
		t.Fatalf("records=%#v", got.Records)
	}
	legacy := got.Records["legacy"]
	if legacy.Generation != 0 || legacy.Credential == nil || legacy.Credential.AccessToken != "legacy-access" || legacy.Credential.RefreshToken != "legacy-refresh" || legacy.Credential.ExpiresAtMS != 1700001000000 || legacy.Credential.ChatGPTAccountID != "chat-legacy" {
		t.Fatalf("legacy=%#v", legacy)
	}
	current := got.Records["current"]
	if current.Generation != 7 || current.Credential == nil || current.Credential.AccessToken != "current-access" || current.RefreshGrantFingerprint != "fp-1" || current.ReplacedAtMS == nil || *current.ReplacedAtMS != 1700000000000 {
		t.Fatalf("current=%#v", current)
	}
	deleted := got.Records["deleted"]
	if deleted.Generation != 9 || deleted.Credential != nil || deleted.DeletedAtMS == nil || *deleted.DeletedAtMS != 1700003000000 {
		t.Fatalf("deleted=%#v", deleted)
	}
	if _, exists := got.Records["invalid"]; exists {
		t.Fatalf("invalid record survived normalization: %#v", got.Records["invalid"])
	}
}

func TestManagedCredentialStoreClassifiesMissingInvalidAndUnreadable(t *testing.T) {
	home := t.TempDir()
	store := mustManagedCredentialStore(t, home)
	missing := store.Read()
	if missing.Status != ManagedCredentialStoreMissing || len(missing.Records) != 0 {
		t.Fatalf("missing=%#v", missing)
	}

	writeManagedStore(t, home, []byte(`{"broken":`), 0o600)
	invalid := store.Read()
	if invalid.Status != ManagedCredentialStoreInvalid || len(invalid.Records) != 0 {
		t.Fatalf("invalid=%#v", invalid)
	}

	if err := os.Remove(filepath.Join(home, "codex-accounts.json")); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("permission denied")
	store.openFile = func(string) (*os.File, error) { return nil, sentinel }
	unreadable := store.Read()
	if unreadable.Status != ManagedCredentialStoreUnreadable || len(unreadable.Records) != 0 {
		t.Fatalf("unreadable=%#v", unreadable)
	}
}

func TestManagedCredentialStoreRejectsUnsafeOversizedAndNonRegularFiles(t *testing.T) {
	home := t.TempDir()
	store := mustManagedCredentialStore(t, home)

	writeManagedStore(t, home, make([]byte, maxManagedStoreBytes+1), 0o600)
	if got := store.Read(); got.Status != ManagedCredentialStoreInvalid {
		t.Fatalf("oversized status=%q", got.Status)
	}

	if runtime.GOOS != "windows" {
		writeManagedStore(t, home, []byte(`{}`), 0o644)
		if got := store.Read(); got.Status != ManagedCredentialStoreInvalid {
			t.Fatalf("unsafe permissions status=%q", got.Status)
		}
	}

	path := filepath.Join(home, "codex-accounts.json")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := store.Read(); got.Status != ManagedCredentialStoreInvalid {
		t.Fatalf("directory status=%q", got.Status)
	}
}

func TestManagedCredentialStoreReadsFreshSnapshotEachTime(t *testing.T) {
	home := t.TempDir()
	store := mustManagedCredentialStore(t, home)
	writeManagedStore(t, home, []byte(`{"acct":{"accessToken":"first","refreshToken":"r1","expiresAt":1,"chatgptAccountId":"c1"}}`), 0o600)
	first := store.Read()
	if first.Status != ManagedCredentialStoreOK || first.Records["acct"].Credential == nil || first.Records["acct"].Credential.AccessToken != "first" {
		t.Fatalf("first=%#v", first)
	}
	writeManagedStore(t, home, []byte(`{"acct":{"generation":4,"credential":{"accessToken":"second","refreshToken":"r2","expiresAt":2,"chatgptAccountId":"c2"}}}`), 0o600)
	second := store.Read()
	if second.Status != ManagedCredentialStoreOK || second.Records["acct"].Generation != 4 || second.Records["acct"].Credential == nil || second.Records["acct"].Credential.AccessToken != "second" {
		t.Fatalf("second=%#v", second)
	}
}

func TestNewManagedCredentialStoreRequiresAbsoluteHome(t *testing.T) {
	for _, home := range []string{"", ".", "relative/benes"} {
		if _, err := NewManagedCredentialStore(home); err == nil {
			t.Fatalf("NewManagedCredentialStore(%q) accepted invalid home", home)
		}
	}
}

func mustManagedCredentialStore(t *testing.T, home string) *ManagedCredentialStore {
	t.Helper()
	store, err := NewManagedCredentialStore(home)
	if err != nil {
		t.Fatalf("NewManagedCredentialStore(): %v", err)
	}
	return store
}

func writeManagedStore(t *testing.T, home string, body []byte, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(home, "codex-accounts.json")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
