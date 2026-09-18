package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wibias/Benes/internal/codexauth"
)

type quotaRT func(*http.Request) (*http.Response, error)

func (f quotaRT) Do(req *http.Request) (*http.Response, error) { return f(req) }

func TestCodexAuthListDTOAndPauseExhaustedUseStoredQuota(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0","codexAccounts":[{"id":"pool-1","email":"ada@example.com","plan":"plus"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := codexauth.NewManagedCredentialStore(home)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if err := store.Put(context.Background(), "pool-1", codexauth.ManagedCredential{
		AccessToken: "at", RefreshToken: "rt", ExpiresAtMS: now + 3600000, ChatGPTAccountID: "chat-1",
	}, &now); err != nil {
		t.Fatal(err)
	}
	quotas := codexauth.NewQuotaState()
	weekly := 100.0
	resetAt := float64(time.Now().Add(time.Hour).Unix())
	quotas.SetParsedForCredential("pool-1", 1, codexauth.QuotaReading{WeeklyPercent: &weekly, WeeklyResetAt: &resetAt}, 1, time.Now())

	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
		CodexAccounts:  &CodexAccountRuntime{Store: store, Quotas: quotas},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	list := httptest.NewRequest(http.MethodGet, "/api/codex-auth/accounts", nil)
	list.Host = "127.0.0.1"
	listRR := httptest.NewRecorder()
	h.ServeHTTP(listRR, list)
	if listRR.Code != 200 || !strings.Contains(listRR.Body.String(), `"hasCredential":true`) || !strings.Contains(listRR.Body.String(), `"weeklyPercent":100`) {
		t.Fatalf("list=%s", listRR.Body.String())
	}

	pause := httptest.NewRequest(http.MethodPut, "/api/codex-auth/accounts/pause-exhausted", nil)
	pause.Host = "127.0.0.1"
	pauseRR := httptest.NewRecorder()
	h.ServeHTTP(pauseRR, pause)
	if pauseRR.Code != 200 || !strings.Contains(pauseRR.Body.String(), `"pool-1"`) {
		t.Fatalf("pause=%s", pauseRR.Body.String())
	}
	cfg, _ := os.ReadFile(configPath)
	if !strings.Contains(string(cfg), "pool-1") || !strings.Contains(string(cfg), "pausedCodexAccountIds") {
		t.Fatalf("config=%s", cfg)
	}
}

func TestCodexAuthResetCreditsUsesCanonicalWHAM(t *testing.T) {
	prev := codexauth.ResetCreditHTTP
	t.Cleanup(func() { codexauth.ResetCreditHTTP = prev })
	codexauth.ResetCreditHTTP = quotaRT(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() != "chatgpt.com" || req.Header.Get("Authorization") != "Bearer at" {
			t.Fatalf("%s %s", req.URL, req.Header.Get("Authorization"))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"credits":[],"available_count":3}`)), Header: make(http.Header)}, nil
	})
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"at","account_id":"chat"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	main, err := codexauth.NewMainCredentialSource(codexHome)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
		CodexAccounts:  &CodexAccountRuntime{Main: main},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/codex-auth/reset-credits?accountId=__main__", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var dto map[string]any
	if json.Unmarshal(rr.Body.Bytes(), &dto) != nil || dto["available_count"] != 3.0 {
		t.Fatalf("dto=%s", rr.Body.String())
	}
}

const testRedeemRequestID = "550e8400-e29b-41d4-a716-446655440000"

func resetCreditConsumeHandler(t *testing.T) http.Handler {
	t.Helper()
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), []byte(`{"tokens":{"access_token":"at","account_id":"chat"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	main, err := codexauth.NewMainCredentialSource(codexHome)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
		CodexAccounts:  &CodexAccountRuntime{Main: main},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	return h
}

func TestCodexAuthResetCreditsConsumeReusesClientRedeemRequestID(t *testing.T) {
	prev := codexauth.ResetCreditHTTP
	t.Cleanup(func() { codexauth.ResetCreditHTTP = prev })
	var ids []string
	codexauth.ResetCreditHTTP = quotaRT(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/backend-api/wham/rate-limit-reset-credits/consume" {
			t.Fatalf("%s %s", req.Method, req.URL)
		}
		raw, _ := io.ReadAll(req.Body)
		var body map[string]string
		if json.Unmarshal(raw, &body) != nil {
			t.Fatalf("body=%s", raw)
		}
		ids = append(ids, body["redeem_request_id"])
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":"reset"}`)), Header: make(http.Header)}, nil
	})
	h := resetCreditConsumeHandler(t)
	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/api/codex-auth/reset-credits/consume", strings.NewReader(`{"accountId":"__main__","redeemRequestId":"`+testRedeemRequestID+`"}`))
		req.Host = "127.0.0.1"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"code":"reset"`) {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
	}
	if len(ids) != 2 || ids[0] != testRedeemRequestID || ids[1] != testRedeemRequestID {
		t.Fatalf("ids=%v", ids)
	}
}

func TestCodexAuthResetCreditsConsumeRejectsMissingRedeemRequestID(t *testing.T) {
	prev := codexauth.ResetCreditHTTP
	t.Cleanup(func() { codexauth.ResetCreditHTTP = prev })
	codexauth.ResetCreditHTTP = quotaRT(func(*http.Request) (*http.Response, error) {
		t.Fatal("missing redeemRequestId must not reach WHAM")
		return nil, nil
	})
	h := resetCreditConsumeHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/codex-auth/reset-credits/consume", strings.NewReader(`{"accountId":"__main__"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCodexAuthResetCreditsConsumeRejectsServerMintedHex(t *testing.T) {
	prev := codexauth.ResetCreditHTTP
	t.Cleanup(func() { codexauth.ResetCreditHTTP = prev })
	codexauth.ResetCreditHTTP = quotaRT(func(*http.Request) (*http.Response, error) {
		t.Fatal("16-hex mint must not reach WHAM")
		return nil, nil
	})
	h := resetCreditConsumeHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/codex-auth/reset-credits/consume", strings.NewReader(`{"accountId":"__main__","redeemRequestId":"0123456789abcdef"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
