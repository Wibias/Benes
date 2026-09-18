package commandcode

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/providers/antigravity"
)

type rt func(*http.Request) (*http.Response, error)

func (f rt) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestCommandCodeJSONCallbackPersists(t *testing.T) {
	login, err := NewPendingLogin()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(login.AuthURL(), "commandcode.ai") {
		t.Fatalf("url=%s", login.AuthURL())
	}
	home := t.TempDir()
	file := PendingFile{Path: filepath.Join(home, PendingFileName)}
	if err := file.Save(login); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"apiKey": "cc-secret", "userId": "u1", "userName": "ada", "keyName": "cli", "state": login.State})
	done, err := file.Complete(context.Background(), string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if done.Account.ID != "u1" || done.Account.Token != "cc-secret" {
		t.Fatalf("%+v", done.Account)
	}
	auth := filepath.Join(home, "auth.json")
	if err := Persist(auth, done.Account); err != nil {
		t.Fatal(err)
	}
	accounts, active, err := antigravity.LoadPublicAccountsFor(auth, ProviderID)
	if err != nil || active != "u1" || len(accounts) != 1 {
		t.Fatalf("%#v %s %v", accounts, active, err)
	}
	stored, _ := os.ReadFile(auth)
	if !strings.Contains(string(stored), `"command-code"`) {
		t.Fatalf("%s", stored)
	}
}

func TestCommandCodePastedKeyRequiresWhoami(t *testing.T) {
	prev := HTTPClient
	t.Cleanup(func() { HTTPClient = prev })
	HTTPClient = rt(func(req *http.Request) (*http.Response, error) {
		body, _ := json.Marshal(map[string]any{"user": map[string]string{"id": "u2"}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})
	login, _ := NewPendingLogin()
	file := PendingFile{Path: filepath.Join(t.TempDir(), PendingFileName)}
	_ = file.Save(login)
	done, err := file.Complete(context.Background(), "cc-pasted")
	if err != nil {
		t.Fatal(err)
	}
	if done.Account.ID != "u2" {
		t.Fatalf("%+v", done.Account)
	}
}
