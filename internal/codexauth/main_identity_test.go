package codexauth

import (
	"fmt"
	"testing"
	"time"
)

func TestMainCredentialSourceDerivesPhysicalAccountIdentity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		name      string
		idToken   string
		access    string
		accountID string
		want      string
	}{
		{
			name:      "id token direct claim wins",
			idToken:   jwtPayload(t, `{"chatgpt_account_id":"id-direct"}`),
			access:    jwtPayload(t, `{"chatgpt_account_id":"access-direct"}`),
			accountID: "field-account",
			want:      "id-direct",
		},
		{
			name:      "access token direct claim",
			access:    jwtPayload(t, `{"chatgpt_account_id":"access-direct"}`),
			accountID: "field-account",
			want:      "access-direct",
		},
		{
			name:      "namespaced auth claim",
			access:    jwtPayload(t, `{"https://api.openai.com/auth":{"chatgpt_account_id":"namespace-account"}}`),
			accountID: "field-account",
			want:      "namespace-account",
		},
		{
			name:      "first organization fallback",
			access:    jwtPayload(t, `{"organizations":[{"id":"org-first"},{"id":"org-second"}]}`),
			accountID: "field-account",
			want:      "org-first",
		},
		{
			name:      "auth field final fallback",
			access:    "opaque-access-token",
			accountID: "field-account",
			want:      "field-account",
		},
		{
			name:      "missing identity stays empty",
			access:    "opaque-access-token",
			accountID: "",
			want:      "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			idField := ""
			if tc.idToken != "" {
				idField = fmt.Sprintf(`,"id_token":%q`, tc.idToken)
			}
			accountField := ""
			if tc.accountID != "" {
				accountField = fmt.Sprintf(`,"account_id":%q`, tc.accountID)
			}
			writeMainAuth(t, home, []byte(fmt.Sprintf(`{"tokens":{"access_token":%q%s%s}}`, tc.access, idField, accountField)), 0o600)
			got := mustMainCredentialSource(t, home).Read(now)
			if got.Status != MainCredentialOK || got.Identity != tc.want {
				t.Fatalf("status=%q identity=%q want=%q", got.Status, got.Identity, tc.want)
			}
		})
	}
}

func TestMainCredentialSourceIdentityLivenessRereadsPhysicalAuth(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	home := t.TempDir()
	source := mustMainCredentialSource(t, home)
	writeMainAuth(t, home, []byte(`{"tokens":{"access_token":"first-token","account_id":"same-account"}}`), 0o600)
	first := source.Read(now)
	if first.Status != MainCredentialOK || first.Identity != "same-account" {
		t.Fatalf("first=%#v", first)
	}

	writeMainAuth(t, home, []byte(`{"tokens":{"access_token":"rotated-token","account_id":"same-account"}}`), 0o600)
	if !source.IsIdentityLive(first.Identity, now) {
		t.Fatal("same physical account with a rotated token must remain live")
	}

	writeMainAuth(t, home, []byte(`{"tokens":{"access_token":"other-token","account_id":"other-account"}}`), 0o600)
	if source.IsIdentityLive(first.Identity, now) {
		t.Fatal("different physical account must fence stale work")
	}

	writeMainAuth(t, home, []byte(`{"tokens":`), 0o600)
	if source.IsIdentityLive("other-account", now) {
		t.Fatal("unreadable or invalid current auth must fail the identity fence")
	}
}

func TestMainCredentialSourceIdentityUsesJWTBeforeMutableAccountField(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	home := t.TempDir()
	source := mustMainCredentialSource(t, home)
	token := jwtPayload(t, `{"chatgpt_account_id":"jwt-account"}`)
	writeMainAuth(t, home, []byte(fmt.Sprintf(`{"tokens":{"access_token":%q,"account_id":"old-field"}}`, token)), 0o600)
	first := source.Read(now)
	if first.Identity != "jwt-account" {
		t.Fatalf("identity=%q", first.Identity)
	}

	writeMainAuth(t, home, []byte(fmt.Sprintf(`{"tokens":{"access_token":%q,"account_id":"new-field"}}`, token)), 0o600)
	if !source.IsIdentityLive(first.Identity, now) {
		t.Fatal("account_id field drift must not replace a live JWT account identity")
	}
}

func TestMainCredentialSourceReadsEmailFromIDToken(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	home := t.TempDir()
	idToken := jwtPayload(t, `{"email":"Ada@Example.com","chatgpt_account_id":"id-direct"}`)
	access := jwtPayload(t, `{"chatgpt_account_id":"access-direct"}`)
	writeMainAuth(t, home, []byte(fmt.Sprintf(
		`{"tokens":{"access_token":%q,"id_token":%q,"account_id":"field-account"}}`,
		access, idToken,
	)), 0o600)
	got := mustMainCredentialSource(t, home).Read(now)
	if got.Status != MainCredentialOK {
		t.Fatalf("status=%q", got.Status)
	}
	if got.Email != "ada@example.com" {
		t.Fatalf("email=%q", got.Email)
	}
}
