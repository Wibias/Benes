package codexauth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestNewMainCredentialSourceRejectsInvalidHome(t *testing.T) {
	for _, home := range []string{"", ".", "relative/codex"} {
		if _, err := NewMainCredentialSource(home); err == nil {
			t.Fatalf("NewMainCredentialSource(%q) accepted invalid home", home)
		}
	}
}

func TestMainCredentialSourceClassifiesMissingInvalidAndUnreadable(t *testing.T) {
	home := t.TempDir()
	source := mustMainCredentialSource(t, home)

	if got := source.Read(time.Unix(1_700_000_000, 0)); got.Status != MainCredentialMissing {
		t.Fatalf("missing status=%q", got.Status)
	}

	writeMainAuth(t, home, []byte(`{"tokens":`), 0o600)
	if got := source.Read(time.Unix(1_700_000_000, 0)); got.Status != MainCredentialInvalid {
		t.Fatalf("malformed status=%q", got.Status)
	}

	writeMainAuth(t, home, []byte(`{"tokens":{"account_id":"acct"}}`), 0o600)
	if got := source.Read(time.Unix(1_700_000_000, 0)); got.Status != MainCredentialInvalid {
		t.Fatalf("missing token status=%q", got.Status)
	}

	writeMainAuth(t, home, []byte(`{"tokens":{"access_token":42,"account_id":"acct"}}`), 0o600)
	if got := source.Read(time.Unix(1_700_000_000, 0)); got.Status != MainCredentialInvalid {
		t.Fatalf("non-string token status=%q", got.Status)
	}

	if err := os.Remove(filepath.Join(home, "auth.json")); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("permission denied")
	source.openFile = func(string) (*os.File, error) { return nil, sentinel }
	if got := source.Read(time.Unix(1_700_000_000, 0)); got.Status != MainCredentialUnreadable {
		t.Fatalf("unreadable status=%q", got.Status)
	}
}

func TestMainCredentialSourceReadsOpaqueAndJWTTokenLifetime(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		name      string
		token     string
		accountID *string
		want      MainCredentialStatus
	}{
		{name: "opaque token remains live", token: "opaque-access-token", accountID: stringPtr("acct_opaque"), want: MainCredentialOK},
		{name: "malformed jwt remains live", token: "a.not-base64.c", accountID: stringPtr("acct_malformed"), want: MainCredentialOK},
		{name: "jwt without exp remains live", token: jwtPayload(t, `{"sub":"user"}`), accountID: stringPtr("acct_noexp"), want: MainCredentialOK},
		{name: "jwt nonnumeric exp remains live", token: jwtPayload(t, `{"exp":"later"}`), accountID: stringPtr("acct_string_exp"), want: MainCredentialOK},
		{name: "future jwt remains live", token: jwtPayload(t, fmt.Sprintf(`{"exp":%d}`, now.Add(time.Minute).Unix())), accountID: stringPtr("acct_live"), want: MainCredentialOK},
		{name: "equal exp is expired", token: jwtPayload(t, fmt.Sprintf(`{"exp":%d}`, now.Unix())), accountID: stringPtr("acct_expired"), want: MainCredentialExpired},
		{name: "past jwt is expired", token: jwtPayload(t, fmt.Sprintf(`{"exp":%d}`, now.Add(-time.Second).Unix())), accountID: stringPtr("acct_expired"), want: MainCredentialExpired},
		{name: "missing account id stays usable", token: "opaque-access-token", accountID: nil, want: MainCredentialOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			account := ""
			accountField := ""
			if tc.accountID != nil {
				account = *tc.accountID
				accountField = fmt.Sprintf(`,"account_id":%q`, account)
			}
			writeMainAuth(t, home, []byte(fmt.Sprintf(`{"tokens":{"access_token":%q%s}}`, tc.token, accountField)), 0o600)
			source := mustMainCredentialSource(t, home)
			got := source.Read(now)
			if got.Status != tc.want {
				t.Fatalf("status=%q want=%q", got.Status, tc.want)
			}
			if tc.want == MainCredentialOK {
				if got.Credential.AccessToken != tc.token || got.Credential.ChatGPTAccountID != account {
					t.Fatalf("credential=%#v", got.Credential)
				}
			} else if got.Credential != (MainCredential{}) {
				t.Fatalf("non-ok result exposed credential=%#v", got.Credential)
			}
		})
	}
}

func TestMainCredentialSourceRejectsUnsafeOrOversizedAuthFile(t *testing.T) {
	home := t.TempDir()
	source := mustMainCredentialSource(t, home)

	writeMainAuth(t, home, make([]byte, maxMainAuthBytes+1), 0o600)
	if got := source.Read(time.Now()); got.Status != MainCredentialInvalid {
		t.Fatalf("oversized status=%q", got.Status)
	}

	if runtime.GOOS != "windows" {
		writeMainAuth(t, home, []byte(`{"tokens":{"access_token":"secret"}}`), 0o644)
		if got := source.Read(time.Now()); got.Status != MainCredentialInvalid {
			t.Fatalf("unsafe permissions status=%q", got.Status)
		}
	}
}

func TestMainCredentialSourceRejectsNonRegularAuthFile(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, "auth.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	source := mustMainCredentialSource(t, home)
	if got := source.Read(time.Now()); got.Status != MainCredentialInvalid {
		t.Fatalf("directory status=%q", got.Status)
	}
}

func TestMainCredentialSourceReadsFreshFileEachTime(t *testing.T) {
	home := t.TempDir()
	source := mustMainCredentialSource(t, home)
	writeMainAuth(t, home, []byte(`{"tokens":{"access_token":"first","account_id":"acct_first"}}`), 0o600)
	first := source.Read(time.Now())
	if first.Status != MainCredentialOK || first.Credential.AccessToken != "first" {
		t.Fatalf("first=%#v", first)
	}

	writeMainAuth(t, home, []byte(`{"tokens":{"access_token":"second","account_id":"acct_second"}}`), 0o600)
	second := source.Read(time.Now())
	if second.Status != MainCredentialOK || second.Credential.AccessToken != "second" || second.Credential.ChatGPTAccountID != "acct_second" {
		t.Fatalf("second=%#v", second)
	}
}

func TestMainCredentialResultDoesNotExposeTokenOutsideOK(t *testing.T) {
	home := t.TempDir()
	token := "super-secret-token-material"
	writeMainAuth(t, home, []byte(fmt.Sprintf(`{"tokens":{"access_token":%q}}`, token)), 0o600)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(home, "auth.json"), 0o644); err != nil {
			t.Fatal(err)
		}
		source := mustMainCredentialSource(t, home)
		got := source.Read(time.Now())
		if got.Status != MainCredentialInvalid || got.Credential.AccessToken != "" {
			t.Fatalf("result=%#v", got)
		}
	}
}

func mustMainCredentialSource(t *testing.T, home string) *MainCredentialSource {
	t.Helper()
	source, err := NewMainCredentialSource(home)
	if err != nil {
		t.Fatalf("NewMainCredentialSource(): %v", err)
	}
	return source
}

func writeMainAuth(t *testing.T, home string, body []byte, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(home, "auth.json")
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

func jwtPayload(t *testing.T, payload string) string {
	t.Helper()
	return "e30." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".signature"
}

func stringPtr(value string) *string { return &value }
