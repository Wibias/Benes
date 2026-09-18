package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wibias/Benes/internal/catalog"
	"github.com/Wibias/Benes/internal/harnessboard"
	"github.com/Wibias/Benes/internal/harnesspolicy"
)

func TestHarnessesAPIRequiresLoopback(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/harnesses", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d", blockedRR.Code)
	}
}

func TestHarnessesAPIProbeAndSettingsPersist(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	if err := os.MkdirAll(filepath.Join(xdg, "opencode", "logs"), 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"hostname":"127.0.0.1","port":18080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	harnessboard.UserHome = func() (string, error) { return home, nil }
	harnessboard.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	harnessboard.ListProcesses = func() ([]harnessboard.Process, error) {
		return []harnessboard.Process{{Name: "opencode"}}, nil
	}
	t.Cleanup(harnessboard.ResetHooks)

	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	get := httptest.NewRequest(http.MethodGet, "/api/harnesses", nil)
	get.Host = "127.0.0.1"
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, get)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get=%d %s", getRR.Code, getRR.Body.String())
	}
	if strings.Contains(getRR.Body.String(), `"token":"valid"`) {
		t.Fatalf("claimed valid token: %s", getRR.Body.String())
	}
	var payload struct {
		Clients []struct {
			ClientID   string  `json:"clientId"`
			DetectPath *string `json:"detectPath"`
			LogPath    *string `json:"logPath"`
			Token      string  `json:"token"`
		} `json:"clients"`
	}
	if err := json.Unmarshal(getRR.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range payload.Clients {
		if row.ClientID != "opencode" {
			continue
		}
		found = true
		if row.DetectPath == nil || !strings.Contains(*row.DetectPath, "opencode") {
			t.Fatalf("detect=%v", row.DetectPath)
		}
		if row.LogPath == nil || !strings.HasSuffix(*row.LogPath, "logs") {
			t.Fatalf("log=%v", row.LogPath)
		}
		if row.Token != "none" {
			t.Fatalf("token=%s", row.Token)
		}
	}
	if !found {
		t.Fatal("missing opencode")
	}

	put := httptest.NewRequest(http.MethodPut, "/api/harnesses/settings", strings.NewReader(`{"clientId":"opencode","autoDetect":false,"autoApply":true,"retainSnapshot":true,"allowRestart":false}`))
	put.Host = "127.0.0.1"
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put=%d %s", putRR.Code, putRR.Body.String())
	}
	stored, err := harnessboard.LoadSettings(filepath.Dir(configPath))
	if err != nil {
		t.Fatal(err)
	}
	if stored["opencode"].AutoDetect {
		t.Fatalf("settings not persisted: %#v", stored["opencode"])
	}
}

func TestHarnessesSettingsSidecarPatchSemantics(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"hostname":"127.0.0.1","port":18080}`), 0o600); err != nil {
		t.Fatal(err)
	}

	webDisabled := harnesspolicy.ActivationDisabled
	visionEnabled := harnesspolicy.ActivationEnabled
	seed := harnessboard.DefaultSettings()
	seed.Sidecars = &harnesspolicy.Overrides{WebSearch: &webDisabled, Vision: &visionEnabled}
	if _, err := harnessboard.PutSettings(home, "opencode", seed); err != nil {
		t.Fatal(err)
	}

	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	put := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/harnesses/settings", strings.NewReader(body))
		req.Host = "127.0.0.1"
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}
	load := func() harnessboard.Settings {
		t.Helper()
		all, err := harnessboard.LoadSettings(home)
		if err != nil {
			t.Fatal(err)
		}
		return all["opencode"]
	}

	if rr := put(`{"clientId":"opencode"}`); rr.Code != http.StatusOK {
		t.Fatalf("omitted sidecars status=%d body=%s", rr.Code, rr.Body.String())
	}
	row := load()
	if row.Sidecars == nil || row.Sidecars.WebSearch == nil || *row.Sidecars.WebSearch != harnesspolicy.ActivationDisabled || row.Sidecars.Vision == nil || *row.Sidecars.Vision != harnesspolicy.ActivationEnabled {
		t.Fatalf("omitted sidecars changed state: %#v", row.Sidecars)
	}

	if rr := put(`{"clientId":"opencode","sidecars":{"webSearch":"enabled"}}`); rr.Code != http.StatusOK {
		t.Fatalf("set webSearch status=%d body=%s", rr.Code, rr.Body.String())
	}
	row = load()
	if row.Sidecars == nil || row.Sidecars.WebSearch == nil || *row.Sidecars.WebSearch != harnesspolicy.ActivationEnabled || row.Sidecars.Vision == nil || *row.Sidecars.Vision != harnesspolicy.ActivationEnabled {
		t.Fatalf("set webSearch changed wrong fields: %#v", row.Sidecars)
	}

	if rr := put(`{"clientId":"opencode","sidecars":{"vision":null}}`); rr.Code != http.StatusOK {
		t.Fatalf("clear vision status=%d body=%s", rr.Code, rr.Body.String())
	}
	row = load()
	if row.Sidecars == nil || row.Sidecars.WebSearch == nil || *row.Sidecars.WebSearch != harnesspolicy.ActivationEnabled || row.Sidecars.Vision != nil {
		t.Fatalf("clear vision did not restore inheritance: %#v", row.Sidecars)
	}

	before, err := os.ReadFile(harnessboard.SettingsPath(home))
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		`{"clientId":"opencode","sidecars":{"webSearch":"inherit"}}`,
		`{"clientId":"opencode","sidecars":{"webSearch":true}}`,
	} {
		rr := put(invalid)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), `"code":"invalid_body"`) {
			t.Fatalf("invalid patch status=%d body=%s", rr.Code, rr.Body.String())
		}
		after, err := os.ReadFile(harnessboard.SettingsPath(home))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(before) {
			t.Fatalf("invalid patch mutated settings\nbefore=%s\nafter=%s", before, after)
		}
	}
}

func TestHarnessesRevealOpensAllowlistedLog(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	logs := filepath.Join(xdg, "opencode", "logs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"hostname":"127.0.0.1","port":18080}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	harnessboard.UserHome = func() (string, error) { return home, nil }
	harnessboard.LookPath = func(string) (string, error) { return "", os.ErrNotExist }
	harnessboard.ListProcesses = func() ([]harnessboard.Process, error) { return nil, nil }
	var opened string
	harnessboard.OpenPath = func(path string, isDir bool) error {
		opened = path
		if !isDir {
			t.Fatalf("expected directory")
		}
		return nil
	}
	t.Cleanup(harnessboard.ResetHooks)

	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
		CatalogModels:  []catalog.Model{{ID: "openai-apikey/gpt-5.5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)

	req := httptest.NewRequest(http.MethodPost, "/api/harnesses/reveal", strings.NewReader(`{"clientId":"opencode","target":"log"}`))
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d %s", rr.Code, rr.Body.String())
	}
	if opened != logs {
		t.Fatalf("opened=%q want=%q", opened, logs)
	}
}
