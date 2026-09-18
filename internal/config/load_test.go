package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfigFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDiskConfigPreservesProviderJSONAndExtractsDataPlaneKeys(t *testing.T) {
	path := writeConfigFixture(t, `{
		"unknownTopLevel":{"keep":true},
		"providers":{
			"openrouter":{"adapter":"openai-chat","baseUrl":"https://openrouter.ai/api/v1","apiKey":"provider-secret","future":{"x":1}},
			"native":{"adapter":"openai-responses","baseUrl":"https://api.openai.com/v1","other":42}
		},
		"apiKeys":[
			{"id":"a","name":"A","key":"client-secret-a","createdAt":"2026-01-01"},
			{"id":"broken","name":"Broken","createdAt":"2026-01-01"},
			{"id":"b","name":"B","key":"client-secret-b","createdAt":"2026-01-02"}
		]
	}`)

	cfg, err := LoadDiskConfig(path, 1<<20)
	if err != nil {
		t.Fatalf("LoadDiskConfig(): %v", err)
	}
	if cfg.Source != DiskConfigSourceFile {
		t.Fatalf("source=%q", cfg.Source)
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("providers=%#v", cfg.Providers)
	}
	openrouter := string(cfg.Providers["openrouter"])
	if !strings.Contains(openrouter, `"future":{"x":1}`) || !strings.Contains(openrouter, `"apiKey":"provider-secret"`) {
		t.Fatalf("provider raw JSON lost fields: %s", openrouter)
	}
	if len(cfg.DataPlaneTokens) != 2 || cfg.DataPlaneTokens[0] != "client-secret-a" || cfg.DataPlaneTokens[1] != "client-secret-b" {
		t.Fatalf("tokens=%#v", cfg.DataPlaneTokens)
	}
	if !strings.Contains(string(cfg.Raw), `"unknownTopLevel":{"keep":true}`) {
		t.Fatalf("raw config lost unknown field: %s", cfg.Raw)
	}
}

func TestLoadDiskConfigAcceptsUTF8BOM(t *testing.T) {
	path := writeConfigFixture(t, "\ufeff{\"providers\":{},\"apiKeys\":[]}")
	if _, err := LoadDiskConfig(path, 1<<20); err != nil {
		t.Fatalf("LoadDiskConfig(): %v", err)
	}
}

func TestLoadDiskConfigIgnoresMalformedAPIKeyRowsLikeCurrentLoader(t *testing.T) {
	path := writeConfigFixture(t, `{
		"providers":{},
		"apiKeys":[null,1,"x",{}, {"key":""}, {"key":"   "}, {"key":"valid"}]
	}`)
	cfg, err := LoadDiskConfig(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.DataPlaneTokens) != 1 || cfg.DataPlaneTokens[0] != "valid" {
		t.Fatalf("tokens=%#v", cfg.DataPlaneTokens)
	}
}

func TestLoadDiskConfigExtractsOutboundProxyPolicyFields(t *testing.T) {
	path := writeConfigFixture(t, `{
		"providers":{},
		"proxy":"http://proxy.example:3128",
		"noProxy":["internal.test","10.0.0.0/8"]
	}`)
	cfg, err := LoadDiskConfig(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Proxy != "http://proxy.example:3128" {
		t.Fatalf("proxy=%q", cfg.Proxy)
	}
	if cfg.NoProxy != "internal.test,10.0.0.0/8" {
		t.Fatalf("noProxy=%q", cfg.NoProxy)
	}
}

func TestLoadDiskConfigExtractsProxyDirectFallback(t *testing.T) {
	path := writeConfigFixture(t, `{
		"providers":{},
		"proxy":"http://proxy.example:3128",
		"proxyDirectFallback":true
	}`)
	cfg, err := LoadDiskConfig(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ProxyDirectFallback {
		t.Fatal("proxyDirectFallback not loaded")
	}
}

func TestLoadDiskConfigExtractsWebSearchMaxSearchesPerTurn(t *testing.T) {
	path := writeConfigFixture(t, `{
		"providers":{},
		"webSearchSidecar":{"maxSearchesPerTurn":5}
	}`)
	cfg, err := LoadDiskConfig(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebSearchMaxSearches != 5 {
		t.Fatalf("maxSearches=%d", cfg.WebSearchMaxSearches)
	}
}

func TestLoadDiskConfigRejectsInvalidWebSearchMaxSearchesPerTurn(t *testing.T) {
	path := writeConfigFixture(t, `{
		"providers":{},
		"webSearchSidecar":{"maxSearchesPerTurn":"three"}
	}`)
	if _, err := LoadDiskConfig(path, 1<<20); err == nil {
		t.Fatal("invalid maxSearchesPerTurn was accepted")
	}
}

func TestLoadDiskConfigTreatsNonArrayAPIKeysAsAbsent(t *testing.T) {
	path := writeConfigFixture(t, `{"providers":{},"apiKeys":{"key":"not-a-row"}}`)
	cfg, err := LoadDiskConfig(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataPlaneTokens != nil {
		t.Fatalf("tokens=%#v", cfg.DataPlaneTokens)
	}
}

func TestLoadDiskConfigRejectsMalformedRootAndProvidersShape(t *testing.T) {
	for _, content := range []string{
		`[]`,
		`null`,
		`{"providers":[]}`,
		`{"providers":{"x":null}}`,
		`{"providers":{"x":1}}`,
	} {
		path := writeConfigFixture(t, content)
		if _, err := LoadDiskConfig(path, 1<<20); err == nil {
			t.Fatalf("accepted %s", content)
		}
	}
}

func TestLoadDiskConfigAllowsMissingProvidersForReadOnlyInspection(t *testing.T) {
	path := writeConfigFixture(t, `{"apiKeys":[{"key":"k"}]}`)
	cfg, err := LoadDiskConfig(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers == nil || len(cfg.Providers) != 0 {
		t.Fatalf("providers=%#v", cfg.Providers)
	}
}

func TestLoadDiskConfigIsBounded(t *testing.T) {
	path := writeConfigFixture(t, `{"providers":{},"padding":"`+strings.Repeat("x", 1024)+`"}`)
	_, err := LoadDiskConfig(path, 128)
	if !errors.Is(err, ErrConfigTooLarge) {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadDiskConfigRecoversOnlyMissingPathAsDefaultState(t *testing.T) {
	if _, err := LoadDiskConfig("", 1024); err == nil {
		t.Fatal("blank path accepted")
	}

	cfg, err := LoadDiskConfig(filepath.Join(t.TempDir(), "missing.json"), 1024)
	if err != nil {
		t.Fatalf("missing config returned error: %v", err)
	}
	if cfg.Source != DiskConfigSourceDefault {
		t.Fatalf("source=%q", cfg.Source)
	}
	if cfg.Providers == nil || len(cfg.Providers) != 1 || cfg.Providers["openai"] == nil {
		t.Fatalf("providers=%#v", cfg.Providers)
	}
	if cfg.DataPlaneTokens != nil || cfg.CORSAllowOrigins != nil {
		t.Fatalf("default credentials/origins tokens=%#v origins=%#v", cfg.DataPlaneTokens, cfg.CORSAllowOrigins)
	}
	if string(cfg.Raw) != defaultDiskConfigJSON {
		t.Fatalf("raw=%q", cfg.Raw)
	}
	listener, err := ProjectListener(cfg)
	if err != nil {
		t.Fatalf("ProjectListener(default): %v", err)
	}
	if listener.Hostname != DefaultListenerHostname || listener.Port != DefaultListenerPort {
		t.Fatalf("listener=%#v", listener)
	}

	present := writeConfigFixture(t, `{}`)
	presentCfg, err := LoadDiskConfig(present, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if presentCfg.Source != DiskConfigSourceFile {
		t.Fatalf("present source=%q", presentCfg.Source)
	}
	if len(presentCfg.Providers) != 0 || string(presentCfg.Raw) != "{}" {
		t.Fatalf("present providers=%#v raw=%q", presentCfg.Providers, presentCfg.Raw)
	}

	broken := writeConfigFixture(t, `{`)
	if _, err := LoadDiskConfig(broken, 1024); err == nil {
		t.Fatal("malformed existing config degraded to defaults")
	}
}
