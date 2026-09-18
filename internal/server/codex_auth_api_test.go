package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/codexauth"
)

func TestCodexAuthAutoSwitchPersistsThreshold(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0","autoSwitchThreshold":80}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/codex-auth/active", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("catalog token should not read management route: %d %s", blockedRR.Code, blockedRR.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/api/codex-auth/active", nil)
	get.Host = "127.0.0.1"
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, get)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	var before map[string]any
	if err := json.Unmarshal(getRR.Body.Bytes(), &before); err != nil {
		t.Fatal(err)
	}
	if before["autoSwitchThreshold"] != float64(80) {
		t.Fatalf("before %+v", before)
	}

	put := httptest.NewRequest(http.MethodPut, "/api/codex-auth/auto-switch", strings.NewReader(`{"threshold":0}`))
	put.Host = "127.0.0.1"
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"autoSwitchThreshold": 0`) && !strings.Contains(string(raw), `"autoSwitchThreshold":0`) {
		t.Fatalf("config not persisted: %s", raw)
	}
	get2 := httptest.NewRequest(http.MethodGet, "/api/codex-auth/active", nil)
	get2.Host = "127.0.0.1"
	get2RR := httptest.NewRecorder()
	h.ServeHTTP(get2RR, get2)
	var after map[string]any
	if err := json.Unmarshal(get2RR.Body.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if after["autoSwitchThreshold"] != float64(0) {
		t.Fatalf("after %+v", after)
	}
}

func TestCodexAuthAccountsListAliasPriorityAndCooldown(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0","codexAccounts":[{"id":"pool-1","email":"a@example.com"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	health := codexauth.NewHealthState()
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
		CodexHealth:    health,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	blocked := httptest.NewRequest(http.MethodGet, "/api/codex-auth/accounts", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("catalog token should not list accounts: %d %s", blockedRR.Code, blockedRR.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/api/codex-auth/accounts", nil)
	list.Host = "127.0.0.1"
	listRR := httptest.NewRecorder()
	h.ServeHTTP(listRR, list)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRR.Code, listRR.Body.String())
	}
	if strings.Contains(listRR.Body.String(), "accessToken") || strings.Contains(listRR.Body.String(), "refresh_token") {
		t.Fatalf("secret leaked: %s", listRR.Body.String())
	}
	if !strings.Contains(listRR.Body.String(), `"id":"pool-1"`) || !strings.Contains(listRR.Body.String(), `"id":"__main__"`) {
		t.Fatalf("list=%s", listRR.Body.String())
	}

	alias := httptest.NewRequest(http.MethodPut, "/api/codex-auth/accounts/alias", strings.NewReader(`{"id":"pool-1","alias":"desk"}`))
	alias.Host = "127.0.0.1"
	aliasRR := httptest.NewRecorder()
	h.ServeHTTP(aliasRR, alias)
	if aliasRR.Code != http.StatusOK {
		t.Fatalf("alias status=%d body=%s", aliasRR.Code, aliasRR.Body.String())
	}

	pri := httptest.NewRequest(http.MethodPut, "/api/codex-auth/accounts/priority", strings.NewReader(`{"id":"pool-1","priority":2}`))
	pri.Host = "127.0.0.1"
	priRR := httptest.NewRecorder()
	h.ServeHTTP(priRR, pri)
	if priRR.Code != http.StatusOK {
		t.Fatalf("priority status=%d body=%s", priRR.Code, priRR.Body.String())
	}

	use := httptest.NewRequest(http.MethodPut, "/api/codex-auth/active", strings.NewReader(`{"accountId":"pool-1"}`))
	use.Host = "127.0.0.1"
	useRR := httptest.NewRecorder()
	h.ServeHTTP(useRR, use)
	if useRR.Code != http.StatusOK {
		t.Fatalf("use status=%d body=%s", useRR.Code, useRR.Body.String())
	}

	cool := httptest.NewRequest(http.MethodPost, "/api/codex-auth/accounts/clear-cooldown", strings.NewReader(`{"id":"__main__"}`))
	cool.Host = "127.0.0.1"
	coolRR := httptest.NewRecorder()
	h.ServeHTTP(coolRR, cool)
	if coolRR.Code != http.StatusOK {
		t.Fatalf("cooldown status=%d body=%s", coolRR.Code, coolRR.Body.String())
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"alias": "desk"`) && !strings.Contains(text, `"alias":"desk"`) {
		t.Fatalf("alias not persisted: %s", text)
	}
	if !strings.Contains(text, `"pool-1": 2`) && !strings.Contains(text, `"pool-1":2`) {
		t.Fatalf("priority not persisted: %s", text)
	}
	if !strings.Contains(text, `"activeCodexAccountId": "pool-1"`) && !strings.Contains(text, `"activeCodexAccountId":"pool-1"`) {
		t.Fatalf("active not persisted: %s", text)
	}
	if strings.Contains(text, "accessToken") {
		t.Fatalf("secret written: %s", text)
	}
}

func TestCodexAuthPauseStrategyAndFailoverPersist(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"listen":"127.0.0.1:0","codexAccounts":[{"id":"pool-1","email":"a@example.com"}],"activeCodexAccountId":"pool-1","activeCodexAccountPinned":"pool-1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai": providerFunc(nil)},
		ConfigPath:     configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	blocked := httptest.NewRequest(http.MethodPut, "/api/codex-auth/accounts/pause", strings.NewReader(`{"id":"pool-1","paused":true}`))
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("catalog token should not pause: %d %s", blockedRR.Code, blockedRR.Body.String())
	}

	pause := httptest.NewRequest(http.MethodPut, "/api/codex-auth/accounts/pause", strings.NewReader(`{"id":"pool-1","paused":true}`))
	pause.Host = "127.0.0.1"
	pauseRR := httptest.NewRecorder()
	h.ServeHTTP(pauseRR, pause)
	if pauseRR.Code != http.StatusOK {
		t.Fatalf("pause status=%d body=%s", pauseRR.Code, pauseRR.Body.String())
	}

	strategy := httptest.NewRequest(http.MethodPut, "/api/codex-auth/pool-strategy", strings.NewReader(`{"strategy":"round-robin","stickyLimit":3}`))
	strategy.Host = "127.0.0.1"
	strategyRR := httptest.NewRecorder()
	h.ServeHTTP(strategyRR, strategy)
	if strategyRR.Code != http.StatusOK {
		t.Fatalf("strategy status=%d body=%s", strategyRR.Code, strategyRR.Body.String())
	}

	fail := httptest.NewRequest(http.MethodPut, "/api/codex-auth/failover", strings.NewReader(`{"threshold":5}`))
	fail.Host = "127.0.0.1"
	failRR := httptest.NewRecorder()
	h.ServeHTTP(failRR, fail)
	if failRR.Code != http.StatusOK {
		t.Fatalf("failover status=%d body=%s", failRR.Code, failRR.Body.String())
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "pool-1") || !strings.Contains(text, "pausedCodexAccountIds") {
		t.Fatalf("pause not persisted: %s", text)
	}
	if strings.Contains(text, `"activeCodexAccountId": "pool-1"`) || strings.Contains(text, `"activeCodexAccountId":"pool-1"`) {
		t.Fatalf("paused active pin not released: %s", text)
	}
	if !strings.Contains(text, "round-robin") || (!strings.Contains(text, `"accountPoolStickyLimit": 3`) && !strings.Contains(text, `"accountPoolStickyLimit":3`)) {
		t.Fatalf("strategy not persisted: %s", text)
	}
	if !strings.Contains(text, `"upstreamFailoverThreshold": 5`) && !strings.Contains(text, `"upstreamFailoverThreshold":5`) {
		t.Fatalf("failover not persisted: %s", text)
	}
	if strings.Contains(text, "accessToken") {
		t.Fatalf("secret written: %s", text)
	}
}
