package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/oauth/loginown"
	"github.com/Wibias/Benes/internal/providers/antigravity"
)

func TestOAuthAccountsAPIOmitsSecrets(t *testing.T) {
	home := t.TempDir()
	authPath := filepath.Join(home, "auth.json")
	if err := antigravity.AppendAccount(authPath, antigravity.Account{ID: "proj-1", Token: "ya29-secret", ProjectID: "proj-1"}, "refresh-secret"); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		AuthStorePath:  authPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/oauth/accounts?provider=google-antigravity", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/accounts?provider=google-antigravity", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "ya29-secret") || strings.Contains(rr.Body.String(), "refresh-secret") {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var got struct {
		ActiveAccountID string `json:"activeAccountId"`
		Accounts        []struct {
			ID     string `json:"id"`
			Active bool   `json:"active"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ActiveAccountID != "proj-1" || len(got.Accounts) != 1 || !got.Accounts[0].Active {
		t.Fatalf("%+v", got)
	}
	status := httptest.NewRequest(http.MethodGet, "/api/oauth/status?provider=google-antigravity", nil)
	status.Host = "127.0.0.1"
	statusRR := httptest.NewRecorder()
	h.ServeHTTP(statusRR, status)
	if statusRR.Code != http.StatusOK || !strings.Contains(statusRR.Body.String(), `"loggedIn":true`) {
		t.Fatalf("status=%d body=%s", statusRR.Code, statusRR.Body.String())
	}
}

func TestOAuthStatusHidesStoredLoginWhileAuthorizeIsPending(t *testing.T) {
	loginown.ResetForTest()
	t.Cleanup(loginown.ResetForTest)
	home := t.TempDir()
	authPath := filepath.Join(home, "auth.json")
	if err := antigravity.AppendAccount(authPath, antigravity.Account{ID: "proj-1", Token: "ya29-secret", ProjectID: "proj-1"}, "refresh-secret"); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		AuthStorePath:  authPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	if _, err := loginown.TryClaim("google-antigravity"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/status?provider=google-antigravity", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got struct {
		LoggedIn bool `json:"loggedIn"`
		Pending  bool `json:"pending"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Pending || got.LoggedIn {
		t.Fatalf("want pending leftover login hidden, got %+v body=%s", got, rr.Body.String())
	}
}

func TestOAuthAccountsUnknownProvider(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/oauth/accounts?provider=chatgpt", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestOAuthAccountsPutActiveAndDelete(t *testing.T) {
	home := t.TempDir()
	authPath := filepath.Join(home, "auth.json")
	raw := []byte(`{"google-antigravity":{"activeAccountId":"a","accounts":[{"id":"a","credential":{"access":"ta-secret","refresh":"ra-secret","expires":1,"projectId":"pa"}},{"id":"b","credential":{"access":"tb-secret","refresh":"rb-secret","expires":1,"projectId":"pb"}}]}}`)
	if err := os.WriteFile(authPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		AuthStorePath:  authPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	put := httptest.NewRequest(http.MethodPut, "/api/oauth/accounts/active", strings.NewReader(`{"provider":"google-antigravity","id":"b"}`))
	put.Host = "127.0.0.1"
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	if strings.Contains(putRR.Body.String(), "secret") {
		t.Fatalf("secret leaked: %s", putRR.Body.String())
	}
	get := httptest.NewRequest(http.MethodGet, "/api/oauth/accounts?provider=google-antigravity", nil)
	get.Host = "127.0.0.1"
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, get)
	if !strings.Contains(getRR.Body.String(), `"activeAccountId":"b"`) {
		t.Fatalf("get=%s", getRR.Body.String())
	}
	del := httptest.NewRequest(http.MethodDelete, "/api/oauth/accounts?provider=google-antigravity&id=a", nil)
	del.Host = "127.0.0.1"
	delRR := httptest.NewRecorder()
	h.ServeHTTP(delRR, del)
	if delRR.Code != http.StatusOK {
		t.Fatalf("del status=%d body=%s", delRR.Code, delRR.Body.String())
	}
	get2 := httptest.NewRequest(http.MethodGet, "/api/oauth/accounts?provider=google-antigravity", nil)
	get2.Host = "127.0.0.1"
	get2RR := httptest.NewRecorder()
	h.ServeHTTP(get2RR, get2)
	if strings.Contains(get2RR.Body.String(), `"id":"a"`) || !strings.Contains(get2RR.Body.String(), `"id":"b"`) {
		t.Fatalf("after delete=%s", get2RR.Body.String())
	}
	if strings.Contains(get2RR.Body.String(), "secret") {
		t.Fatalf("secret leaked: %s", get2RR.Body.String())
	}
	other := httptest.NewRequest(http.MethodPut, "/api/oauth/accounts/active", strings.NewReader(`{"provider":"not-a-provider","id":"x"}`))
	other.Host = "127.0.0.1"
	otherRR := httptest.NewRecorder()
	h.ServeHTTP(otherRR, other)
	if otherRR.Code != http.StatusBadRequest {
		t.Fatalf("other status=%d body=%s", otherRR.Code, otherRR.Body.String())
	}
}

func TestOAuthAccountsCursorUseAndRemove(t *testing.T) {
	home := t.TempDir()
	authPath := filepath.Join(home, "auth.json")
	raw := []byte(`{"cursor":{"activeAccountId":"a","accounts":[{"id":"a","credential":{"access":"ta-secret","refresh":"ra-secret","expires":1,"email":"a@x.com"}},{"id":"b","credential":{"access":"tb-secret","refresh":"rb-secret","expires":1,"email":"b@x.com"}}]}}`)
	if err := os.WriteFile(authPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		AuthStorePath:  authPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	put := httptest.NewRequest(http.MethodPut, "/api/oauth/accounts/active", strings.NewReader(`{"provider":"cursor","id":"b"}`))
	put.Host = "127.0.0.1"
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK || strings.Contains(putRR.Body.String(), "secret") {
		t.Fatalf("put=%d %s", putRR.Code, putRR.Body.String())
	}
	get := httptest.NewRequest(http.MethodGet, "/api/oauth/accounts?provider=cursor", nil)
	get.Host = "127.0.0.1"
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, get)
	if !strings.Contains(getRR.Body.String(), `"activeAccountId":"b"`) || strings.Contains(getRR.Body.String(), "secret") {
		t.Fatalf("get=%s", getRR.Body.String())
	}
	del := httptest.NewRequest(http.MethodDelete, "/api/oauth/accounts?provider=cursor&id=a", nil)
	del.Host = "127.0.0.1"
	delRR := httptest.NewRecorder()
	h.ServeHTTP(delRR, del)
	if delRR.Code != http.StatusOK {
		t.Fatalf("del=%d %s", delRR.Code, delRR.Body.String())
	}
}
