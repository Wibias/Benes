package kiro

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers/antigravity"
)

func TestPersistAppendsDistinctKiroAccountsAndKeepsRoutingPaired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	first := ImportedCredential{
		AccessToken: "tok-a", Refresh: "rt-a",
		ProfileARN: "arn:aws:codewhisperer:us-east-1:123456789012:profile/a",
		APIRegion:  "us-east-1", AuthType: AuthKiroDesktop,
	}
	second := ImportedCredential{
		AccessToken: "tok-b", Refresh: "rt-b",
		ProfileARN: "arn:aws:codewhisperer:eu-west-1:123456789012:profile/b",
		APIRegion:  "eu-west-1", AuthType: AuthKiroDesktop,
		ClientID: "client-b", ClientSecret: "secret-b",
	}
	if err := Persist(path, first); err != nil {
		t.Fatal(err)
	}
	if err := Persist(path, second); err != nil {
		t.Fatal(err)
	}
	accounts, active, err := antigravity.LoadPublicAccountsFor(path, ProviderID)
	if err != nil || len(accounts) != 2 || active != AccountID(second) {
		t.Fatalf("accounts=%#v active=%q err=%v", accounts, active, err)
	}
	stored, ok := antigravity.ActiveStoredAccount(path, ProviderID)
	if !ok {
		t.Fatal("active")
	}
	snap, err := SnapshotFromStored(stored)
	if err != nil || snap.AccessToken != "tok-b" || snap.ProfileARN != second.ProfileARN || snap.EffectiveRegion() != "eu-west-1" {
		t.Fatalf("active snap=%#v err=%v", snap, err)
	}
	if err := antigravity.SetActiveAccountFor(path, ProviderID, AccountID(first)); err != nil {
		t.Fatal(err)
	}
	stored, ok = antigravity.ActiveStoredAccount(path, ProviderID)
	if !ok {
		t.Fatal("switched")
	}
	snap, err = SnapshotFromStored(stored)
	if err != nil || snap.AccessToken != "tok-a" || snap.ProfileARN != first.ProfileARN || snap.EffectiveRegion() != "us-east-1" {
		t.Fatalf("switched snap=%#v err=%v", snap, err)
	}
}

func TestInactiveKiroAccountRefreshUsesOwnSnapshotNotForeignCLI(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "auth.json")
	stored := ImportedCredential{
		AccessToken: "tok-stored", Refresh: "rt-stored",
		ProfileARN: "arn:aws:codewhisperer:eu-west-1:123456789012:profile/stored",
		APIRegion:  "eu-west-1", SSORegion: "eu-west-1",
		ClientID: "stored-client", ClientSecret: "stored-secret",
		AuthType: AuthAWSSsoOIDC,
	}
	if err := Persist(path, stored); err != nil {
		t.Fatal(err)
	}
	account, ok := antigravity.ActiveStoredAccount(path, ProviderID)
	if !ok {
		t.Fatal("stored")
	}
	writeJSON(t, filepath.Join(root, "other.json"), map[string]any{
		"accessToken": "aoa-other", "refreshToken": "rt-other",
		"region": "ap-southeast-1", "clientId": "other-client", "clientSecret": "other-secret",
	})
	var gotURL string
	var gotBody map[string]any
	meta := RefreshAccountFromStored(account)
	_, err := RefreshToken(context.Background(), RefreshInput{
		Refresh: account.Refresh,
		Stored:  &meta,
		Host:    Host{Platform: runtime.GOOS, Home: root, Env: map[string]string{"KIRO_CREDS_FILE": filepath.Join(root, "other.json")}},
		HTTPClient: &http.Client{Transport: refreshRoundTrip(func(req *http.Request) (*http.Response, error) {
			gotURL = req.URL.String()
			_ = json.NewDecoder(req.Body).Decode(&gotBody)
			return jsonResponse(200, map[string]any{"accessToken": "aoa-new", "expiresIn": 60}), nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "oidc.eu-west-1.amazonaws.com") {
		t.Fatalf("url=%s", gotURL)
	}
	if gotBody["clientId"] != "stored-client" || gotBody["clientSecret"] != "stored-secret" {
		t.Fatalf("body=%#v", gotBody)
	}
}
