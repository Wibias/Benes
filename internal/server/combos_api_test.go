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
)

func TestCombosAPIIsLoopbackAndListsTargets(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		Combos: []Combo{{
			ID: "fast",
			Targets: []ComboTarget{
				{ProviderID: "openai-apikey", Model: "gpt-5.5", Protocol: "openai-chat"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	blocked := httptest.NewRequest(http.MethodGet, "/api/combos", nil)
	blocked.Header.Set("Authorization", "Bearer local-secret")
	blockedRR := httptest.NewRecorder()
	h.ServeHTTP(blockedRR, blocked)
	if blockedRR.Code != http.StatusNotFound {
		t.Fatalf("data-plane status=%d body=%s", blockedRR.Code, blockedRR.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/combos", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-") || strings.Contains(rr.Body.String(), `"apiKey"`) {
		t.Fatalf("secret leaked: %s", rr.Body.String())
	}
	var got struct {
		Combos []struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Targets []struct {
				Provider string `json:"provider"`
				Model    string `json:"model"`
			} `json:"targets"`
		} `json:"combos"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Combos) != 1 || got.Combos[0].ID != "fast" || got.Combos[0].Model != "combo/fast" {
		t.Fatalf("combos=%v", got.Combos)
	}
	if len(got.Combos[0].Targets) != 1 || got.Combos[0].Targets[0].Provider != "openai-apikey" {
		t.Fatalf("targets=%v", got.Combos[0].Targets)
	}
}

func TestCombosAPIEmptyWithoutCombos(t *testing.T) {
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/combos", nil)
	req.Host = "127.0.0.1"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"combos"`) {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCombosAPIPutAndDeletePersistFailover(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
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
	inner := h.(*handler)
	put := httptest.NewRequest(http.MethodPut, "/api/combos", strings.NewReader(`{"id":"fast","combo":{"targets":[{"provider":"openai-apikey","model":"gpt-5.4"}],"strategy":"failover"}}`))
	put.Host = "127.0.0.1"
	put.Header.Set("Content-Type", "application/json")
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK || !strings.Contains(putRR.Body.String(), `"success":true`) {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	spec, ok := inner.comboByID("fast")
	if !ok || len(spec.Targets) != 1 || spec.Targets[0].Model != "gpt-5.4" {
		t.Fatalf("runtime combo=%v ok=%v", spec, ok)
	}
	roundRobin := httptest.NewRequest(http.MethodPut, "/api/combos", strings.NewReader(`{"id":"rr","combo":{"targets":[{"provider":"openai-apikey","model":"gpt-5.4","weight":3}],"strategy":"round-robin","stickyLimit":4}}`))
	roundRobin.Host = "127.0.0.1"
	roundRobin.Header.Set("Content-Type", "application/json")
	rrRR := httptest.NewRecorder()
	h.ServeHTTP(rrRR, roundRobin)
	if rrRR.Code != http.StatusBadRequest {
		t.Fatalf("new round-robin status=%d body=%s", rrRR.Code, rrRR.Body.String())
	}
	convertRR := httptest.NewRequest(http.MethodPut, "/api/combos", strings.NewReader(`{"id":"fast","combo":{"targets":[{"provider":"openai-apikey","model":"gpt-5.4"}],"strategy":"round-robin"}}`))
	convertRR.Host = "127.0.0.1"
	convertRR.Header.Set("Content-Type", "application/json")
	convRR := httptest.NewRecorder()
	h.ServeHTTP(convRR, convertRR)
	if convRR.Code != http.StatusBadRequest {
		t.Fatalf("failover->round-robin convert status=%d body=%s", convRR.Code, convRR.Body.String())
	}
	unknown := httptest.NewRequest(http.MethodPut, "/api/combos", strings.NewReader(`{"id":"weird","combo":{"targets":[{"provider":"openai-apikey","model":"gpt-5.4"}],"strategy":"weighted"}}`))
	unknown.Host = "127.0.0.1"
	unknown.Header.Set("Content-Type", "application/json")
	unkRR := httptest.NewRecorder()
	h.ServeHTTP(unkRR, unknown)
	if unkRR.Code != http.StatusBadRequest {
		t.Fatalf("unknown strategy status=%d body=%s", unkRR.Code, unkRR.Body.String())
	}
	del := httptest.NewRequest(http.MethodDelete, "/api/combos?id=fast", nil)
	del.Host = "127.0.0.1"
	delRR := httptest.NewRecorder()
	h.ServeHTTP(delRR, del)
	if delRR.Code != http.StatusOK || !strings.Contains(delRR.Body.String(), `"success":true`) {
		t.Fatalf("delete status=%d body=%s", delRR.Code, delRR.Body.String())
	}
	if _, ok := inner.comboByID("fast"); ok {
		t.Fatal("combo still in runtime after delete")
	}
}

func TestCombosAPIPutAddsComboToModelsCatalog(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"providers":{"openai-apikey":{"adapter":"openai-chat"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers:      map[string]Provider{"openai-apikey": providerFunc(nil)},
		ConfigPath:     configPath,
		CatalogModels: []catalog.Model{{
			ID:           "openai-apikey/gpt-5.4",
			Context:      catalog.ContextWindow{Tokens: 128000},
			Availability: catalog.Availability{Selectable: true},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	put := httptest.NewRequest(http.MethodPut, "/api/combos", strings.NewReader(`{"id":"fast","combo":{"targets":[{"provider":"openai-apikey","model":"gpt-5.4"}],"strategy":"failover"}}`))
	put.Host = "127.0.0.1"
	put.Header.Set("Content-Type", "application/json")
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	models := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	models.Header.Set("Authorization", "Bearer local-secret")
	modelsRR := httptest.NewRecorder()
	h.ServeHTTP(modelsRR, models)
	if modelsRR.Code != http.StatusOK || !strings.Contains(modelsRR.Body.String(), `"id":"combo/fast"`) {
		t.Fatalf("models status=%d body=%s", modelsRR.Code, modelsRR.Body.String())
	}
}


func TestCombosAPIPreserveLegacyRoundRobinFields(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	seed := `{"providers":{"openai-apikey":{"adapter":"openai-chat"},"anthropic":{"adapter":"anthropic-messages"}},"combos":{"legacy_rr":{"strategy":"round-robin","stickyLimit":4,"targets":[{"provider":"openai-apikey","model":"gpt-4o","weight":3},{"provider":"anthropic","model":"claude-opus-4-6","weight":1}]}}}`
	if err := os.WriteFile(configPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Options{
		DataPlaneToken: "local-secret",
		Providers: map[string]Provider{
			"openai-apikey": providerFunc(nil),
			"anthropic":     providerFunc(nil),
		},
		ConfigPath: configPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	h = attachHandlerClose(t, h)
	get := httptest.NewRequest(http.MethodGet, "/api/combos", nil)
	get.Host = "127.0.0.1"
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, get)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	if !strings.Contains(getRR.Body.String(), `"strategy":"round-robin"`) {
		t.Fatalf("GET missing round-robin: %s", getRR.Body.String())
	}
	if !strings.Contains(getRR.Body.String(), `"weight":3`) {
		t.Fatalf("GET missing weight: %s", getRR.Body.String())
	}
	if !strings.Contains(getRR.Body.String(), `"stickyLimit":4`) {
		t.Fatalf("GET missing stickyLimit: %s", getRR.Body.String())
	}
	// Unrelated edit: change alias only while carrying legacy strategy/weights/sticky.
	put := httptest.NewRequest(http.MethodPut, "/api/combos", strings.NewReader(`{"id":"legacy_rr","combo":{"targets":[{"provider":"openai-apikey","model":"gpt-4o","weight":3},{"provider":"anthropic","model":"claude-opus-4-6","weight":1}],"strategy":"round-robin","stickyLimit":4,"alias":"kept-rr"}}`))
	put.Host = "127.0.0.1"
	put.Header.Set("Content-Type", "application/json")
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, needle := range []string{`"strategy": "round-robin"`, `"stickyLimit": 4`, `"weight": 3`, `"alias": "kept-rr"`} {
		compact := strings.ReplaceAll(needle, " ", "")
		if !strings.Contains(body, needle) && !strings.Contains(body, compact) {
			t.Fatalf("missing %s in config after preserve save: %s", needle, body)
		}
	}
	get2 := httptest.NewRequest(http.MethodGet, "/api/combos", nil)
	get2.Host = "127.0.0.1"
	get2RR := httptest.NewRecorder()
	h.ServeHTTP(get2RR, get2)
	if !strings.Contains(get2RR.Body.String(), `"strategy":"round-robin"`) || !strings.Contains(get2RR.Body.String(), `"weight":3`) {
		t.Fatalf("GET after save lost legacy fields: %s", get2RR.Body.String())
	}
}

func TestCombosAPIPreserveStickyWithoutInventingWeights(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	seed := `{"providers":{"openai-apikey":{"adapter":"openai-chat"}},"combos":{"sticky_failover":{"strategy":"failover","stickyLimit":7,"targets":[{"provider":"openai-apikey","model":"gpt-4o"}]}}}`
	if err := os.WriteFile(configPath, []byte(seed), 0o600); err != nil {
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
	put := httptest.NewRequest(http.MethodPut, "/api/combos", strings.NewReader(`{"id":"sticky_failover","combo":{"targets":[{"provider":"openai-apikey","model":"gpt-4o"}],"strategy":"failover","stickyLimit":7,"alias":"sticky-kept"}}`))
	put.Host = "127.0.0.1"
	put.Header.Set("Content-Type", "application/json")
	putRR := httptest.NewRecorder()
	h.ServeHTTP(putRR, put)
	if putRR.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRR.Code, putRR.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, `"stickyLimit": 7`) && !strings.Contains(body, `"stickyLimit":7`) {
		t.Fatalf("stickyLimit dropped: %s", body)
	}
	absent := httptest.NewRequest(http.MethodPut, "/api/combos", strings.NewReader(`{"id":"no_sticky","combo":{"targets":[{"provider":"openai-apikey","model":"gpt-4o"}],"strategy":"failover"}}`))
	absent.Host = "127.0.0.1"
	absent.Header.Set("Content-Type", "application/json")
	absRR := httptest.NewRecorder()
	h.ServeHTTP(absRR, absent)
	if absRR.Code != http.StatusOK {
		t.Fatalf("absent sticky put status=%d body=%s", absRR.Code, absRR.Body.String())
	}
	raw2, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw2), `"no_sticky"`) && strings.Contains(string(raw2), `"stickyLimit"`) {
		// Ensure the new combo did not invent stickyLimit:1
		start := strings.Index(string(raw2), `"no_sticky"`)
		chunk := string(raw2)[start:]
		end := strings.Index(chunk, "},")
		if end > 0 {
			chunk = chunk[:end]
		}
		if strings.Contains(chunk, "stickyLimit") {
			t.Fatalf("invented stickyLimit on absent combo: %s", chunk)
		}
	}
}


func TestCombosAPIRejectRoundRobinUnlessAlreadyStored(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.json")
	seed := `{"providers":{"openai-apikey":{"adapter":"openai-chat"}},"combos":{"legacy_rr":{"strategy":"round-robin","stickyLimit":2,"targets":[{"provider":"openai-apikey","model":"gpt-4o","weight":2}]},"plain":{"strategy":"failover","targets":[{"provider":"openai-apikey","model":"gpt-4o"}]}}}`
	if err := os.WriteFile(configPath, []byte(seed), 0o600); err != nil {
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
	// Existing RR may be re-saved / renamed.
	rename := httptest.NewRequest(http.MethodPut, "/api/combos", strings.NewReader(`{"id":"legacy_rr_renamed","renameFrom":"legacy_rr","combo":{"targets":[{"provider":"openai-apikey","model":"gpt-4o","weight":2}],"strategy":"round-robin","stickyLimit":2,"alias":"renamed"}}`))
	rename.Host = "127.0.0.1"
	rename.Header.Set("Content-Type", "application/json")
	renRR := httptest.NewRecorder()
	h.ServeHTTP(renRR, rename)
	if renRR.Code != http.StatusOK {
		t.Fatalf("rename preserve status=%d body=%s", renRR.Code, renRR.Body.String())
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, `"legacy_rr"`) {
		t.Fatalf("old id still present after rename: %s", body)
	}
	if !strings.Contains(body, `"legacy_rr_renamed"`) {
		t.Fatalf("renamed id missing: %s", body)
	}
	if !strings.Contains(body, `"round-robin"`) {
		t.Fatalf("strategy lost on rename: %s", body)
	}
	// Existing failover must not convert to RR.
	convert := httptest.NewRequest(http.MethodPut, "/api/combos", strings.NewReader(`{"id":"plain","combo":{"targets":[{"provider":"openai-apikey","model":"gpt-4o"}],"strategy":"round-robin"}}`))
	convert.Host = "127.0.0.1"
	convert.Header.Set("Content-Type", "application/json")
	convRR := httptest.NewRecorder()
	h.ServeHTTP(convRR, convert)
	if convRR.Code != http.StatusBadRequest {
		t.Fatalf("convert status=%d body=%s", convRR.Code, convRR.Body.String())
	}
	// Unknown strategy introduce rejected; preserve-only if already stored.
	intro := httptest.NewRequest(http.MethodPut, "/api/combos", strings.NewReader(`{"id":"plain","combo":{"targets":[{"provider":"openai-apikey","model":"gpt-4o"}],"strategy":"weighted"}}`))
	intro.Host = "127.0.0.1"
	intro.Header.Set("Content-Type", "application/json")
	introRR := httptest.NewRecorder()
	h.ServeHTTP(introRR, intro)
	if introRR.Code != http.StatusBadRequest {
		t.Fatalf("introduce unknown status=%d body=%s", introRR.Code, introRR.Body.String())
	}
}
