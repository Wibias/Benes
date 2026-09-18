package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/authpublic"
	"github.com/Wibias/Benes/internal/oauth/loginown"
	"github.com/Wibias/Benes/internal/providers/antigravity"
)

func TestOAuthLoginRejectsInProgressDuplicate(t *testing.T) {
	loginown.ResetForTest()
	t.Cleanup(loginown.ResetForTest)
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	first := httptest.NewRequest(http.MethodPost, "/api/oauth/login", strings.NewReader(`{"provider":"google-antigravity"}`))
	first.Host = "127.0.0.1"
	firstRR := httptest.NewRecorder()
	h.ServeHTTP(firstRR, first)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", firstRR.Code, firstRR.Body.String())
	}
	second := httptest.NewRequest(http.MethodPost, "/api/oauth/login", strings.NewReader(`{"provider":"google-antigravity"}`))
	second.Host = "127.0.0.1"
	secondRR := httptest.NewRecorder()
	h.ServeHTTP(secondRR, second)
	if secondRR.Code != http.StatusConflict {
		t.Fatalf("second status=%d body=%s", secondRR.Code, secondRR.Body.String())
	}
	if !strings.Contains(secondRR.Body.String(), "A login for google-antigravity is already in progress") {
		t.Fatalf("busy body=%s", secondRR.Body.String())
	}
}

func TestPersistOwnedAccountRejectsSupersededFlow(t *testing.T) {
	loginown.ResetForTest()
	t.Cleanup(loginown.ResetForTest)
	token, err := loginown.TryClaim("cursor")
	if err != nil {
		t.Fatal(err)
	}
	loginown.Cancel("cursor")
	path := filepath.Join(t.TempDir(), "auth.json")
	rr := httptest.NewRecorder()
	if persistOwnedAccount(rr, "cursor", path, antigravity.StoredAccount{ID: "a", Token: "tok"}, token) {
		t.Fatal("superseded persist succeeded")
	}
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), authpublic.SupersededMessage) {
		t.Fatalf("body=%s", rr.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("auth store written after superseded persist")
	}
}
