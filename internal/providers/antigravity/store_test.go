package antigravity

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAccountsFromAuthStoreSkipReauthAndBindProject(t *testing.T) {
	accounts := AccountsFromAuthStore([]byte(`{
		"google-antigravity": {
			"activeAccountId": "a",
			"accounts": [
				{"id":"a","credential":{"access":"ta","refresh":"ra","expires":1,"projectId":"pa"}},
				{"id":"b","needsReauth":true,"credential":{"access":"tb","refresh":"rb","expires":1,"projectId":"pb"}},
				{"id":"c","credential":{"access":"tc","refresh":"rc","expires":1,"projectId":"pc"}}
			]
		}
	}`))
	if len(accounts) != 2 || accounts[0].ID != "a" || accounts[0].ProjectID != "pa" || accounts[1].ID != "c" || accounts[1].Token != "tc" {
		t.Fatalf("accounts=%#v", accounts)
	}
}

func TestAccountsFromLegacyAuthStore(t *testing.T) {
	accounts := AccountsFromAuthStore([]byte(`{"google-antigravity":{"access":"tok","refresh":"rt","expires":9,"projectId":"proj"}}`))
	if len(accounts) != 1 || accounts[0].Token != "tok" || accounts[0].ProjectID != "proj" {
		t.Fatalf("legacy=%#v", accounts)
	}
}

func TestLoadAccountsFileAbsentIsEmpty(t *testing.T) {
	got, err := LoadAccountsFile(filepath.Join(t.TempDir(), "missing-auth.json"))
	if err != nil || len(got) != 0 {
		t.Fatalf("absent=%#v err=%v", got, err)
	}
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"google-antigravity":{"access":"tok","refresh":"rt","expires":1,"projectId":"p"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = LoadAccountsFile(path)
	if err != nil || len(got) != 1 || got[0].ProjectID != "p" || got[0].SourcePath != path {
		t.Fatalf("file=%#v err=%v", got, err)
	}
}

func TestSetActiveAndRemoveAccountPreserveTokens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	raw := []byte(`{"google-antigravity":{"activeAccountId":"a","accounts":[{"id":"a","credential":{"access":"ta","refresh":"ra","expires":1,"projectId":"pa"}},{"id":"b","credential":{"access":"tb","refresh":"rb","expires":1,"projectId":"pb"}}]}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetActiveAccount(path, "b"); err != nil {
		t.Fatal(err)
	}
	_, active, err := LoadPublicAccounts(path)
	if err != nil || active != "b" {
		t.Fatalf("active=%q err=%v", active, err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "ta") || !strings.Contains(string(got), "tb") {
		t.Fatalf("tokens lost: %s", got)
	}
	if err := RemoveAccount(path, "a"); err != nil {
		t.Fatal(err)
	}
	accounts, active, err := LoadPublicAccounts(path)
	if err != nil || active != "b" || len(accounts) != 1 || accounts[0].ID != "b" {
		t.Fatalf("after remove accounts=%#v active=%q err=%v", accounts, active, err)
	}
	if err := RemoveAccount(path, "missing"); err == nil {
		t.Fatal("expected unknown account")
	}
}

func TestAppendStoredAccountPreservesOtherProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(`{"google-antigravity":{"activeAccountId":"g","accounts":[{"id":"g","credential":{"access":"ga","refresh":"gr","expires":1}}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AppendStoredAccount(path, "cursor", StoredAccount{ID: "c", Token: "ca", Refresh: "cr", Email: "c@x.com", Expires: 1}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "google-antigravity") || !strings.Contains(string(got), `"cursor"`) || !strings.Contains(string(got), "ga") {
		t.Fatalf("%s", got)
	}
}

func TestAppendStoredAccountCheckedHonorsGateBeforeWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	blocked := errors.New("blocked")
	err := AppendStoredAccountChecked(path, "cursor", StoredAccount{ID: "c", Token: "ca"}, func() error {
		return blocked
	})
	if !errors.Is(err, blocked) {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("store written after gate failure")
	}
}

func TestAuthStoreMutationsSerializeReadModifyWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := AppendStoredAccount(path, AuthStoreProvider, StoredAccount{
		ID: "a", Token: "initial", Refresh: "refresh-a", ProjectID: "project-initial", Expires: 1,
	}); err != nil {
		t.Fatal(err)
	}

	firstInside := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- AppendStoredAccountChecked(path, AuthStoreProvider, StoredAccount{
			ID: "a", Token: "refresh-result", Refresh: "refresh-a", ProjectID: "project-refresh", Expires: 2,
		}, func() error {
			close(firstInside)
			<-releaseFirst
			return nil
		})
	}()
	<-firstInside

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- AppendStoredAccount(path, AuthStoreProvider, StoredAccount{
			ID: "a", Token: "external-newer", Refresh: "external-refresh", ProjectID: "project-external", Expires: 3,
		})
	}()

	select {
	case err := <-secondDone:
		t.Fatalf("second mutation completed inside first read-modify-write window: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}

	stored, err := loadStoredAccount(path, "a")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Token != "external-newer" || stored.Refresh != "external-refresh" || stored.ProjectID != "project-external" || stored.Expires != 3 {
		t.Fatalf("newer mutation was overwritten: %#v", stored)
	}
}
